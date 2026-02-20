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

const strictCommentDelay = 500 * time.Millisecond

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

	for _, item := range batch {
		if err := ctx.Err(); err != nil {
			return
		}
		if item.CommentMsgID <= 0 || item.SourcePostID != sourceRootID {
			continue
		}
		if item.IsForwarded {
			continue
		}

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
				continue
			}
		}

		sendErr := m.sendLightPayloadAsComment(ctx, api, cfg, payload, targetRootID)
		if sendErr != nil {
			// Upload fallback: refetch original message and send with fallback.
			if fullMsg, rerr := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, item.CommentMsgID); rerr == nil && fullMsg != nil {
				sendErr = m.sendCommentWithFallback(ctx, api, cfg.SourceLinkedPeer, cfg.TargetLinkedPeer, fullMsg, targetRootID, task.ID)
			}
		}

		if sendErr != nil {
			// FloodWait: skip remaining comments for this root.
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
			continue
		}

		_ = db.Model(&localdb.LocalComment{}).
			Where("comment_msg_id = ? AND source_post_id = ?", item.CommentMsgID, sourceRootID).
			Update("is_forwarded", true).Error

		sleepWithContext(ctx, strictCommentDelay)
	}
}

