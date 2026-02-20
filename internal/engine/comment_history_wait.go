package engine

import (
	"context"
	"fmt"
	"time"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"go.uber.org/zap"
)

func (m *TaskManager) waitUntilCommentsDrained(ctx context.Context, task model.Task, runID uint64, cfg *commentPipelineConfig, sourceRootID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return nil
	}
	if sourceRootID <= 0 {
		return nil
	}

	db := cfg.LocalDB

	logEvery := 10 * time.Second
	lastLog := time.Time{}
	logged := false

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		var pendingCount int64
		if err := db.Model(&localdb.LocalComment{}).
			Where("source_post_id = ? AND is_forwarded = ?", sourceRootID, false).
			Count(&pendingCount).Error; err != nil {
			if global.Logger != nil {
				global.Logger.Warn(
					"count pending comments failed",
					zap.Uint("task_id", task.ID),
					zap.Int("source_root_id", sourceRootID),
					zap.Error(err),
				)
			}
			// Best-effort: don't block history on localdb query issues.
			return nil
		}

		if pendingCount <= 0 {
			if logged {
				line := fmt.Sprintf("评论已处理完成: root=%d", sourceRootID)
				if runID != 0 {
					m.record(task.ID, runID, 0, 0, 0, 0, line)
				} else {
					global.BroadcastLog(line)
				}
			}
			return nil
		}

		now := time.Now()
		if !logged || lastLog.IsZero() || now.Sub(lastLog) >= logEvery {
			line := fmt.Sprintf("等待评论处理完成: root=%d pending=%d", sourceRootID, pendingCount)
			if runID != 0 {
				m.record(task.ID, runID, 0, 0, 0, 0, line)
			} else {
				global.BroadcastLog(line)
			}
			logged = true
			lastLog = now
		}

		sleepRandom(ctx, 500*time.Millisecond, 1*time.Second)
	}
}
