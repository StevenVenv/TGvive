package service

import (
	"errors"
	"strings"

	"my-go-server/internal/engine"
	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

func CreateTask(task *model.Task) error {
	return global.DB.Create(task).Error
}

func GetTaskList(userID uint) ([]model.Task, error) {
	var list []model.Task
	err := global.DB.Where("user_id = ?", userID).Order("id desc").Find(&list).Error
	return list, err
}

func GetTaskByID(userID uint, taskID uint) (model.Task, error) {
	var task model.Task
	err := global.DB.Where("id = ? AND user_id = ?", taskID, userID).First(&task).Error
	return task, err
}

func UpdateTaskStatus(userID uint, taskID uint, status int) (model.Task, error) {
	var task model.Task
	if err := global.DB.Where("id = ? AND user_id = ?", taskID, userID).First(&task).Error; err != nil {
		return task, err
	}
	if err := global.DB.Model(&task).Update("status", status).Error; err != nil {
		return task, err
	}
	task.Status = status
	return task, nil
}

func ApplyTaskAction(userID uint, taskID uint, action string) (model.Task, error) {
	var status int
	switch action {
	case "start":
		status = model.TaskStatusRunning
	case "stop":
		status = model.TaskStatusStopped
	case "pause":
		status = model.TaskStatusPaused
	default:
		return model.Task{}, errors.New("invalid action")
	}

	task, err := UpdateTaskStatus(userID, taskID, status)
	if err != nil {
		return task, err
	}

	switch status {
	case model.TaskStatusRunning:
		engine.Manager.StartTask(task)
	case model.TaskStatusPaused:
		engine.Manager.PauseTask(task.ID)
	case model.TaskStatusStopped:
		engine.Manager.StopTask(task.ID)
	}

	return task, nil
}

func UpdateTask(userID uint, taskID uint, payload *model.Task) (model.Task, error) {
	if payload == nil {
		return model.Task{}, errors.New("payload is nil")
	}

	task, err := GetTaskByID(userID, taskID)
	if err != nil {
		return task, err
	}

	// Ensure the worker is stopped before applying changes.
	engine.Manager.StopTask(task.ID)

	src := strings.TrimSpace(payload.SourceURL)
	dst := strings.TrimSpace(payload.TargetURL)
	exec := strings.TrimSpace(payload.ExecuteBy)

	if src != "" && strings.TrimSpace(task.SourceURL) != src {
		task.SourceURL = src
		task.SourceChannelID = 0
	}
	if dst != "" {
		task.TargetURL = dst
	}
	if exec != "" {
		task.ExecuteBy = exec
	}

	if payload.StrategyID != 0 {
		task.StrategyID = payload.StrategyID
	}
	task.KeywordProfileID = payload.KeywordProfileID

	// Strategy-derived configs.
	if payload.CloneMode != 0 {
		task.CloneMode = payload.CloneMode
	}
	task.ContentTypes = payload.ContentTypes
	task.BlockFileExts = payload.BlockFileExts
	task.AllowFileExts = payload.AllowFileExts

	if payload.ScopeType != 0 {
		task.ScopeType = payload.ScopeType
	}
	task.ScopeValue = strings.TrimSpace(payload.ScopeValue)

	task.KeepReply = payload.KeepReply
	task.Realtime = payload.Realtime
	task.CloneComment = payload.CloneComment
	task.GpuAccel = payload.GpuAccel
	task.ChangeMD5 = payload.ChangeMD5

	task.DelayMinMs = payload.DelayMinMs
	task.DelayMaxMs = payload.DelayMaxMs

	task.DailyLimit = payload.DailyLimit
	task.RunWindow = strings.TrimSpace(payload.RunWindow)

	if payload.HistoryOrder != 0 {
		task.HistoryOrder = payload.HistoryOrder
	}

	// Always reset to stopped after edit.
	task.Status = model.TaskStatusStopped

	if err := global.DB.Save(&task).Error; err != nil {
		return task, err
	}
	return task, nil
}

func DeleteTask(userID uint, taskID uint) error {
	task, err := GetTaskByID(userID, taskID)
	if err != nil {
		return err
	}

	engine.Manager.StopTask(task.ID)
	return global.DB.Delete(&task).Error
}
