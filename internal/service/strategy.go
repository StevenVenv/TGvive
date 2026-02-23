package service

import (
	"context"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

func CreateStrategy(ctx context.Context, strategy *model.Strategy) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return global.DB.WithContext(ctx).Create(strategy).Error
}

func GetStrategyList(ctx context.Context, userID uint) ([]model.Strategy, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var list []model.Strategy
	err := global.DB.WithContext(ctx).Where("user_id = ?", userID).Order("id desc").Find(&list).Error
	return list, err
}

func GetStrategyByID(ctx context.Context, userID uint, strategyID uint) (model.Strategy, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var s model.Strategy
	err := global.DB.WithContext(ctx).Where("id = ? AND user_id = ?", strategyID, userID).First(&s).Error
	return s, err
}

func UpdateStrategy(ctx context.Context, userID uint, strategyID uint, payload *model.Strategy) (model.Strategy, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := GetStrategyByID(ctx, userID, strategyID)
	if err != nil {
		return s, err
	}

	s.Name = payload.Name
	s.Remark = payload.Remark

	s.CloneMode = payload.CloneMode
	allowed := payload.AllowedTypes
	if len(allowed.Strings()) == 0 {
		allowed = payload.ContentTypes
	}
	s.AllowedTypes = allowed
	s.ContentTypes = allowed
	s.BlockFileExts = payload.BlockFileExts
	s.AllowFileExts = payload.AllowFileExts
	s.ScopeType = payload.ScopeType
	s.ScopeValue = payload.ScopeValue
	s.HistoryOrder = payload.HistoryOrder
	s.PollInterval = payload.PollInterval
	s.EnableRealtime = payload.EnableRealtime
	s.ScheduleRules = payload.ScheduleRules
	s.CommentRule = payload.CommentRule
	s.WatermarkRule = payload.WatermarkRule

	s.KeepReply = payload.KeepReply
	s.Realtime = payload.Realtime
	s.CloneComment = payload.CloneComment
	s.GpuAccel = payload.GpuAccel
	s.ChangeMD5 = payload.ChangeMD5
	s.RandomFilename = payload.ChangeMD5 && payload.RandomFilename
	s.EnableMediaEdit = payload.EnableMediaEdit

	s.DelayMinMs = payload.DelayMinMs
	s.DelayMaxMs = payload.DelayMaxMs
	s.DailyLimit = payload.DailyLimit
	s.RunWindow = payload.RunWindow

	if err := global.DB.WithContext(ctx).Save(&s).Error; err != nil {
		return s, err
	}
	return s, nil
}

func DeleteStrategy(ctx context.Context, userID uint, strategyID uint) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return global.DB.WithContext(ctx).Where("id = ? AND user_id = ?", strategyID, userID).Delete(&model.Strategy{}).Error
}
