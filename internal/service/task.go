package service

import (
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
