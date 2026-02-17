package service

import (
	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

func CreateStrategy(strategy *model.Strategy) error {
	return global.DB.Create(strategy).Error
}

func GetStrategyList(userID uint) ([]model.Strategy, error) {
	var list []model.Strategy
	err := global.DB.Where("user_id = ?", userID).Order("id desc").Find(&list).Error
	return list, err
}

func GetStrategyByID(userID uint, strategyID uint) (model.Strategy, error) {
	var s model.Strategy
	err := global.DB.Where("id = ? AND user_id = ?", strategyID, userID).First(&s).Error
	return s, err
}

func UpdateStrategy(userID uint, strategyID uint, payload *model.Strategy) (model.Strategy, error) {
	s, err := GetStrategyByID(userID, strategyID)
	if err != nil {
		return s, err
	}

	s.Name = payload.Name
	s.Remark = payload.Remark

	s.CloneMode = payload.CloneMode
	s.ContentTypes = payload.ContentTypes
	s.ScopeType = payload.ScopeType
	s.ScopeValue = payload.ScopeValue
	s.HistoryOrder = payload.HistoryOrder
	s.PollInterval = payload.PollInterval
	s.EnableRealtime = payload.EnableRealtime
	s.ScheduleRules = payload.ScheduleRules

	s.KeepReply = payload.KeepReply
	s.Realtime = payload.Realtime
	s.CloneComment = payload.CloneComment
	s.GpuAccel = payload.GpuAccel
	s.ChangeMD5 = payload.ChangeMD5

	s.DelayMinMs = payload.DelayMinMs
	s.DelayMaxMs = payload.DelayMaxMs
	s.DailyLimit = payload.DailyLimit
	s.RunWindow = payload.RunWindow

	if err := global.DB.Save(&s).Error; err != nil {
		return s, err
	}
	return s, nil
}

func DeleteStrategy(userID uint, strategyID uint) error {
	return global.DB.Where("id = ? AND user_id = ?", strategyID, userID).Delete(&model.Strategy{}).Error
}
