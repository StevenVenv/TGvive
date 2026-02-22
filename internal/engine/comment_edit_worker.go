package engine

import (
	"context"
	"encoding/json"
	"strings"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"
	"my-go-server/pkg/workerpool"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

var commentEditPool = workerpool.New(4, 512).WithName("comment-edit")

// submitCommentEdit schedules an async messages.editMessage to keep mirrored comments in sync.
// It re-reads payload from localdb at execution time to avoid stale/out-of-order edits.
func (m *TaskManager) submitCommentEdit(ctx context.Context, api *tg.Client, taskID uint, cfg *commentPipelineConfig, sourceMsgID int, targetMsgID int) {
	if m == nil || api == nil {
		return
	}
	// Bot published comments cannot be edited via MTProto session.
	if pub := m.getPublisher(taskID); pub != nil && pub.isBot() {
		return
	}
	if cfg == nil || cfg.TargetLinkedPeer == nil || cfg.LocalDB == nil {
		return
	}
	if sourceMsgID <= 0 || targetMsgID <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := commentEditPool.TrySubmit(ctx, func(jobCtx context.Context) error {
		var rec localdb.CommentQueue
		if err := cfg.LocalDB.
			Select("status", "target_msg_id", "payload").
			First(&rec, "msg_id = ?", sourceMsgID).Error; err != nil {
			return err
		}
		if rec.Status != localdb.CommentStatusSuccess || rec.TargetMsgID <= 0 {
			return nil
		}

		targetID := rec.TargetMsgID

		var p localdb.LightPayload
		if len(rec.Payload) > 0 {
			if err := json.Unmarshal(rec.Payload, &p); err != nil {
				return err
			}
		}

		text := p.Text
		if strings.TrimSpace(text) == "" {
			return nil
		}
		if len(p.MediaBytes) > 0 || (p.MediaType != "" && !strings.EqualFold(p.MediaType, "text")) {
			if out, truncated := sanitizeMediaCaptionText(text); truncated {
				text = out
			}
		}

		editReq := &tg.MessagesEditMessageRequest{
			Peer:    cfg.TargetLinkedPeer,
			ID:      targetID,
			Message: text,
		}

		err := processWithRetry(jobCtx, func() error {
			_, err := api.MessagesEditMessage(jobCtx, editReq)
			if err != nil && tgerr.Is(err, "MESSAGE_NOT_MODIFIED") {
				return nil
			}
			return err
		})
		if err != nil && global.Logger != nil {
			global.Logger.Warn(
				"edit mirrored comment failed",
				zap.Uint("task_id", taskID),
				zap.Int("source_msg_id", sourceMsgID),
				zap.Int("target_msg_id", targetID),
				zap.Error(err),
			)
		}
		return nil
	})
	if err != nil && global.Logger != nil {
		global.Logger.Warn(
			"submit comment edit job failed",
			zap.Uint("task_id", taskID),
			zap.Int("source_msg_id", sourceMsgID),
			zap.Int("target_msg_id", targetMsgID),
			zap.Error(err),
		)
	}
}
