package engine

import (
	"context"
	"errors"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

// ResetInterruptedTasksOnStartup clears task statuses that cannot survive a process restart.
func ResetInterruptedTasksOnStartup(ctx context.Context) (int64, error) {
	if global.DB == nil {
		return 0, errors.New("database is nil")
	}
	res := global.DB.WithContext(ctx).
		Model(&model.Task{}).
		Where("status IN ?", []int{model.TaskStatusRunning, model.TaskStatusPaused}).
		Updates(map[string]any{
			"status":     model.TaskStatusStopped,
			"last_error": "服务重启，未完成任务已停止，请重新开始",
		})
	return res.RowsAffected, res.Error
}
