package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

const (
	consumerBatchLimit = 50
	consumerMinTick    = 2 * time.Second
	consumerMaxTick    = 3 * time.Second
)

func (m *TaskManager) startCommentConsumer(ctx context.Context, api *tg.Client, task model.Task, cfg *commentPipelineConfig) {
	if m == nil || api == nil || cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return
	}
	if cfg.SourceLinkedPeer == nil || cfg.TargetLinkedPeer == nil {
		return
	}

	go func() {
		// Stagger start a bit to avoid synchronized scans across tasks.
		sleepRandom(ctx, 0, 2*time.Second)

		var pauseUntil time.Time

		for {
			if err := ctx.Err(); err != nil {
				return
			}

			now := time.Now()
			if !pauseUntil.IsZero() && now.Before(pauseUntil) {
				sleepWithContext(ctx, minDuration(pauseUntil.Sub(now), consumerMaxTick))
				continue
			}

			nextPause := m.consumeLocalCommentsOnce(ctx, api, task, cfg)
			if !nextPause.IsZero() {
				pauseUntil = nextPause
			}

			sleepRandom(ctx, consumerMinTick, consumerMaxTick)
		}
	}()
}

func truncateError(err error, max int) string {
	if err == nil || max <= 0 {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func nextAttemptDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return 3 * time.Second
	}
	if attempts > 10 {
		attempts = 10
	}
	// Exponential backoff: 3s, 6s, 12s, ... capped at 10 minutes.
	d := 3 * time.Second * time.Duration(1<<(attempts-1))
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	// Add jitter.
	return randomDuration(d/2, d)
}

// consumeLocalCommentsOnce scans pending comments and sends a batch.
// It returns pauseUntil when a FloodWait happens (caller should skip current cycle).
func (m *TaskManager) consumeLocalCommentsOnce(ctx context.Context, api *tg.Client, task model.Task, cfg *commentPipelineConfig) time.Time {
	if err := ctx.Err(); err != nil {
		return time.Time{}
	}
	if m == nil || api == nil || cfg == nil || cfg.LocalDB == nil {
		return time.Time{}
	}

	db := cfg.LocalDB
	now := time.Now()

	var batch []localdb.LocalComment
	if err := db.
		Where("status = ? AND next_attempt_at <= ?", "pending", now).
		Order("reply_to_root_id ASC").
		Order("msg_id ASC").
		Limit(consumerBatchLimit).
		Find(&batch).Error; err != nil {
		if global.Logger != nil {
			global.Logger.Warn("consumer scan pending failed", zap.Uint("task_id", task.ID), zap.Error(err))
		}
		return time.Time{}
	}

	for _, item := range batch {
		if err := ctx.Err(); err != nil {
			return time.Time{}
		}
		if item.MsgID <= 0 || item.ReplyToRootID <= 0 {
			continue
		}

		var mapping localdb.LocalMapping
		if err := db.Where("source_root_id = ?", item.ReplyToRootID).First(&mapping).Error; err != nil {
			// Mapping missing: retry with backoff, eventually mark failed to avoid blocking history forever.
			attempts := item.Attempts + 1
			updates := map[string]any{
				"attempts":   attempts,
				"last_error": "mapping missing",
			}
			if attempts >= 10 {
				updates["status"] = "failed"
				updates["next_attempt_at"] = now.Add(24 * time.Hour)
			} else {
				updates["status"] = "pending"
				updates["next_attempt_at"] = now.Add(nextAttemptDelay(attempts))
			}
			_ = db.Model(&localdb.LocalComment{}).Where("id = ?", item.ID).Updates(updates).Error
			continue
		}
		if mapping.TargetRootID <= 0 {
			continue
		}

		var payload localdb.LightPayload
		if len(item.LightPayload) > 0 {
			if err := json.Unmarshal(item.LightPayload, &payload); err != nil {
				_ = db.Model(&localdb.LocalComment{}).Where("id = ?", item.ID).
					Updates(map[string]any{
						"status":          "failed",
						"attempts":        item.Attempts + 1,
						"last_error":      truncateError(err, 255),
						"next_attempt_at": now.Add(10 * time.Minute),
					}).Error
				continue
			}
		}

		sendErr := m.sendLightPayloadAsComment(ctx, api, cfg, payload, mapping.TargetRootID)
		if sendErr == nil {
			_ = db.Model(&localdb.LocalComment{}).Where("id = ?", item.ID).
				Updates(map[string]any{
					"status":          "success",
					"last_error":      "",
					"next_attempt_at": time.Time{},
				}).Error
			continue
		}

		// FloodWait: pause and skip current cycle.
		if d, ok := tgerr.AsFloodWait(sendErr); ok {
			if global.Logger != nil {
				global.Logger.Warn(
					"consumer floodwait, skipping current cycle",
					zap.Uint("task_id", task.ID),
					zap.Int("msg_id", item.MsgID),
					zap.Duration("wait", d),
					zap.Error(sendErr),
				)
			}
			return now.Add(d)
		}

		// Fallback: refetch original message and try upload fallback.
		fullMsg, rerr := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, item.MsgID)
		if rerr == nil && fullMsg != nil {
			if ferr := m.sendCommentWithFallback(ctx, api, cfg.SourceLinkedPeer, cfg.TargetLinkedPeer, fullMsg, mapping.TargetRootID, task.ID); ferr == nil {
				_ = db.Model(&localdb.LocalComment{}).Where("id = ?", item.ID).
					Updates(map[string]any{
						"status":          "success",
						"last_error":      "",
						"next_attempt_at": time.Time{},
					}).Error
				continue
			} else {
				sendErr = ferr
			}
		}

		attempts := item.Attempts + 1
		updates := map[string]any{
			"attempts":   attempts,
			"last_error": truncateError(sendErr, 255),
		}
		if attempts >= 10 {
			updates["status"] = "failed"
			updates["next_attempt_at"] = now.Add(24 * time.Hour)
		} else {
			updates["status"] = "pending"
			updates["next_attempt_at"] = now.Add(nextAttemptDelay(attempts))
		}

		_ = db.Model(&localdb.LocalComment{}).Where("id = ?", item.ID).Updates(updates).Error

		if global.Logger != nil {
			global.Logger.Warn(
				"send comment failed",
				zap.Uint("task_id", task.ID),
				zap.Int("msg_id", item.MsgID),
				zap.Int("attempts", attempts),
				zap.Error(sendErr),
			)
		}
	}

	return time.Time{}
}

func (m *TaskManager) sendLightPayloadAsComment(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, payload localdb.LightPayload, replyToRootID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if cfg == nil || cfg.TargetLinkedPeer == nil {
		return errors.New("target peer is nil")
	}
	if replyToRootID <= 0 {
		return errors.New("reply_to_root_id is required")
	}

	replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToRootID}

	// If media reference exists, try sendMedia first.
	if len(payload.MediaBytes) > 0 {
		im, err := tg.DecodeInputMedia(&bin.Buffer{Buf: payload.MediaBytes})
		if err == nil && im != nil {
			rid, err := randomID()
			if err != nil {
				return err
			}
			_, err = api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
				Peer:     cfg.TargetLinkedPeer,
				ReplyTo:  replyTo,
				Media:    im,
				Message:  payload.Text,
				RandomID: rid,
			})
			return err
		}
	}

	// Text-only.
	if strings.TrimSpace(payload.Text) == "" {
		return nil
	}
	rid, err := randomID()
	if err != nil {
		return err
	}
	_, err = api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     cfg.TargetLinkedPeer,
		ReplyTo:  replyTo,
		Message:  payload.Text,
		RandomID: rid,
	})
	return err
}
