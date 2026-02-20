package engine

import (
	"context"
	"encoding/json"
	"time"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

const (
	commentSendDelayMin = 300 * time.Millisecond
	commentSendDelayMax = 1 * time.Second
)

// sendPendingCommentsForRoot sends all unforwarded comments for a given source discussion root,
// in ascending msg id order, and marks them as forwarded in the task localdb.
// Best-effort: errors are logged and the caller continues its main workflow.
// FloodWait will stop processing remaining comments for this root (skip).
func (m *TaskManager) sendPendingCommentsForRoot(
	ctx context.Context,
	api *tg.Client,
	task model.Task,
	cfg *commentPipelineConfig,
	sourceRootID int,
	targetRootID int,
) {
	if err := ctx.Err(); err != nil {
		return
	}
	if m == nil || api == nil || cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return
	}
	if cfg.SourceLinkedPeer == nil || cfg.TargetLinkedPeer == nil {
		return
	}
	if sourceRootID <= 0 || targetRootID <= 0 {
		return
	}

	cfg.sendMu.Lock()
	defer cfg.sendMu.Unlock()

	db := cfg.LocalDB

	var batch []localdb.LocalComment
	if err := db.
		Where("source_post_id = ? AND is_forwarded = ?", sourceRootID, false).
		Order("comment_msg_id ASC").
		Find(&batch).Error; err != nil {
		if global.Logger != nil {
			global.Logger.Warn(
				"scan pending comments failed",
				zap.Uint("task_id", task.ID),
				zap.Int("source_root_id", sourceRootID),
				zap.Error(err),
			)
		}
		return
	}

	sendOne := func(item localdb.LocalComment, payload localdb.LightPayload) error {
		sendErr := m.sendLightPayloadAsComment(ctx, api, cfg, payload, targetRootID)
		if sendErr != nil {
			// Upload fallback: refetch original message and send with fallback.
			if fullMsg, rerr := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, item.CommentMsgID); rerr == nil && fullMsg != nil {
				sendErr = m.sendCommentWithFallback(ctx, api, cfg.SourceLinkedPeer, cfg.TargetLinkedPeer, fullMsg, targetRootID, task.ID)
			}
		}
		return sendErr
	}

	markForwarded := func(ids []int) {
		if len(ids) == 0 {
			return
		}
		_ = db.Model(&localdb.LocalComment{}).
			Where("source_post_id = ? AND comment_msg_id IN ?", sourceRootID, ids).
			Update("is_forwarded", true).Error
	}

	for i := 0; i < len(batch); {
		if err := ctx.Err(); err != nil {
			return
		}

		item := batch[i]
		if item.CommentMsgID <= 0 || item.SourcePostID != sourceRootID || item.IsForwarded {
			i++
			continue
		}

		// Grouped media (album) support.
		if item.GroupedID != 0 {
			gid := item.GroupedID
			j := i
			group := make([]localdb.LocalComment, 0, 8)
			for j < len(batch) {
				it := batch[j]
				if it.CommentMsgID <= 0 || it.SourcePostID != sourceRootID || it.IsForwarded || it.GroupedID != gid {
					break
				}
				group = append(group, it)
				j++
			}

			// Try album send-by-reference if we have >=2 media items with decodable media_bytes.
			if len(group) >= 2 {
				payloads := make([]localdb.LightPayload, 0, len(group))
				ids := make([]int, 0, len(group))
				okAlbum := true
				for _, it := range group {
					ids = append(ids, it.CommentMsgID)
					var p localdb.LightPayload
					if len(it.LightPayload) > 0 {
						if err := json.Unmarshal(it.LightPayload, &p); err != nil {
							okAlbum = false
							break
						}
					}
					if len(p.MediaBytes) == 0 {
						okAlbum = false
						break
					}
					payloads = append(payloads, p)
				}

				if okAlbum {
					if err := m.sendLightPayloadAlbumAsComment(ctx, api, cfg, payloads, targetRootID); err == nil {
						markForwarded(ids)
						sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
						i = j
						continue
					} else if d, ok := tgerr.AsFloodWait(err); ok {
						if global.Logger != nil {
							global.Logger.Warn(
								"comment floodwait, skipping remaining comments for trunk",
								zap.Uint("task_id", task.ID),
								zap.Int("source_root_id", sourceRootID),
								zap.Duration("wait", d),
								zap.Error(err),
							)
						}
						return
					}
				}
			}

			// Fallback: send each item individually (still in order).
			for _, it := range group {
				if err := ctx.Err(); err != nil {
					return
				}

				var payload localdb.LightPayload
				if len(it.LightPayload) > 0 {
					if err := json.Unmarshal(it.LightPayload, &payload); err != nil {
						if global.Logger != nil {
							global.Logger.Warn(
								"decode light payload failed",
								zap.Uint("task_id", task.ID),
								zap.Int("msg_id", it.CommentMsgID),
								zap.Error(err),
							)
						}
						continue
					}
				}

				sendErr := sendOne(it, payload)
				if sendErr != nil {
					if d, ok := tgerr.AsFloodWait(sendErr); ok {
						if global.Logger != nil {
							global.Logger.Warn(
								"comment floodwait, skipping remaining comments for trunk",
								zap.Uint("task_id", task.ID),
								zap.Int("source_root_id", sourceRootID),
								zap.Duration("wait", d),
								zap.Error(sendErr),
							)
						}
						return
					}
					if global.Logger != nil {
						global.Logger.Warn(
							"send comment failed",
							zap.Uint("task_id", task.ID),
							zap.Int("msg_id", it.CommentMsgID),
							zap.Int64("grouped_id", gid),
							zap.Error(sendErr),
						)
					}
					continue
				}

				markForwarded([]int{it.CommentMsgID})
				sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
			}

			i = j
			continue
		}

		// Single message.
		var payload localdb.LightPayload
		if len(item.LightPayload) > 0 {
			if err := json.Unmarshal(item.LightPayload, &payload); err != nil {
				if global.Logger != nil {
					global.Logger.Warn(
						"decode light payload failed",
						zap.Uint("task_id", task.ID),
						zap.Int("msg_id", item.CommentMsgID),
						zap.Error(err),
					)
				}
				i++
				continue
			}
		}

		sendErr := sendOne(item, payload)
		if sendErr != nil {
			if d, ok := tgerr.AsFloodWait(sendErr); ok {
				if global.Logger != nil {
					global.Logger.Warn(
						"comment floodwait, skipping remaining comments for trunk",
						zap.Uint("task_id", task.ID),
						zap.Int("source_root_id", sourceRootID),
						zap.Duration("wait", d),
						zap.Error(sendErr),
					)
				}
				return
			}
			if global.Logger != nil {
				global.Logger.Warn(
					"send comment failed",
					zap.Uint("task_id", task.ID),
					zap.Int("msg_id", item.CommentMsgID),
					zap.Error(sendErr),
				)
			}
			i++
			continue
		}

		markForwarded([]int{item.CommentMsgID})
		sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
		i++
	}
}
