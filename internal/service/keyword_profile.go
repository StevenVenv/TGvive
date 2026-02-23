package service

import (
	"context"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

func CreateKeywordProfile(ctx context.Context, p *model.KeywordProfile) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return global.DB.WithContext(ctx).Create(p).Error
}

func GetKeywordProfileList(ctx context.Context, userID uint) ([]model.KeywordProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var list []model.KeywordProfile
	err := global.DB.WithContext(ctx).Where("user_id = ?", userID).Order("id desc").Find(&list).Error
	return list, err
}

func GetKeywordProfileByID(ctx context.Context, userID uint, profileID uint) (model.KeywordProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var p model.KeywordProfile
	err := global.DB.WithContext(ctx).Where("id = ? AND user_id = ?", profileID, userID).First(&p).Error
	return p, err
}

func UpdateKeywordProfile(ctx context.Context, userID uint, profileID uint, payload *model.KeywordProfile) (model.KeywordProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p, err := GetKeywordProfileByID(ctx, userID, profileID)
	if err != nil {
		return p, err
	}

	p.Name = payload.Name
	p.Remark = payload.Remark
	p.BlockWords = payload.BlockWords
	p.AllowWords = payload.AllowWords
	p.ReplaceRules = payload.ReplaceRules

	if err := global.DB.WithContext(ctx).Save(&p).Error; err != nil {
		return p, err
	}
	return p, nil
}

func DeleteKeywordProfile(ctx context.Context, userID uint, profileID uint) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return global.DB.WithContext(ctx).Where("id = ? AND user_id = ?", profileID, userID).Delete(&model.KeywordProfile{}).Error
}
