package engine

import (
	"errors"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

type taskProgressSnapshot struct {
	TotalMsg     int
	ProcessedCnt int
	SuccessCnt   int
	FailCnt      int
	RootCnt      int
	ReplyCnt     int
}

func persistTaskProgressSnapshot(taskID uint, snap taskProgressSnapshot) error {
	if taskID == 0 {
		return errors.New("task id is required")
	}
	if global.DB == nil {
		return nil
	}

	if snap.TotalMsg < 0 {
		snap.TotalMsg = 0
	}
	if snap.ProcessedCnt < 0 {
		snap.ProcessedCnt = 0
	}
	if snap.SuccessCnt < 0 {
		snap.SuccessCnt = 0
	}
	if snap.FailCnt < 0 {
		snap.FailCnt = 0
	}
	if snap.RootCnt < 0 {
		snap.RootCnt = 0
	}
	if snap.ReplyCnt < 0 {
		snap.ReplyCnt = 0
	}

	return global.DB.Model(&model.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"progress_total_msg":     snap.TotalMsg,
			"progress_processed_cnt": snap.ProcessedCnt,
			"progress_success_cnt":   snap.SuccessCnt,
			"progress_fail_cnt":      snap.FailCnt,
			"progress_root_cnt":      snap.RootCnt,
			"progress_reply_cnt":     snap.ReplyCnt,
		}).Error
}
