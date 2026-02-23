package service

import (
	"errors"
	"strings"

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
	updates := map[string]any{
		"status": status,
	}
	// Clear last error on new start to avoid stale error messages in UI.
	if status == model.TaskStatusRunning {
		updates["last_error"] = ""
	}
	if err := global.DB.Model(&task).Updates(updates).Error; err != nil {
		return task, err
	}
	task.Status = status
	if status == model.TaskStatusRunning {
		task.LastError = ""
	}
	return task, nil
}

func ApplyTaskAction(userID uint, taskID uint, action string) (model.Task, error) {
	var status int
	switch action {
	case "start":
		status = model.TaskStatusRunning
	case "restart":
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

	src := strings.TrimSpace(payload.SourceURL)
	dst := strings.TrimSpace(payload.TargetURL)
	exec := strings.TrimSpace(payload.ExecuteBy)
	remark := strings.TrimSpace(payload.Remark)
	pubType := strings.TrimSpace(payload.PublishType)
	pubSession := strings.TrimSpace(payload.PublishSessionKey)
	pubBotID := strings.TrimSpace(payload.PublishBotID)

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
	task.Remark = remark
	switch pubType {
	case "":
		task.PublishType = ""
		task.PublishSessionKey = ""
		task.PublishBotID = ""
	case "account":
		task.PublishType = "account"
		task.PublishSessionKey = pubSession
		task.PublishBotID = ""
	case "bot":
		task.PublishType = "bot"
		task.PublishSessionKey = ""
		task.PublishBotID = pubBotID
	default:
		// Ignore invalid publish_type to avoid breaking existing tasks.
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
	task.RandomFilename = payload.ChangeMD5 && payload.RandomFilename
	task.EnableMediaEdit = payload.EnableMediaEdit

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
	return global.DB.Delete(&task).Error
}
