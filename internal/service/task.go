package service

import (
	"errors"

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
