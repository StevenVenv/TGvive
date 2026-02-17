package service

import (
	"my-go-server/internal/global"
	"my-go-server/internal/model"
)

func CreateKeywordProfile(p *model.KeywordProfile) error {
	return global.DB.Create(p).Error
}

func GetKeywordProfileList(userID uint) ([]model.KeywordProfile, error) {
	var list []model.KeywordProfile
	err := global.DB.Where("user_id = ?", userID).Order("id desc").Find(&list).Error
	return list, err
}

func GetKeywordProfileByID(userID uint, profileID uint) (model.KeywordProfile, error) {
	var p model.KeywordProfile
	err := global.DB.Where("id = ? AND user_id = ?", profileID, userID).First(&p).Error
	return p, err
}

func UpdateKeywordProfile(userID uint, profileID uint, payload *model.KeywordProfile) (model.KeywordProfile, error) {
	p, err := GetKeywordProfileByID(userID, profileID)
	if err != nil {
		return p, err
	}

	p.Name = payload.Name
	p.Remark = payload.Remark
	p.BlockWords = payload.BlockWords
	p.AllowWords = payload.AllowWords
	p.ReplaceRules = payload.ReplaceRules
	p.UseRegex = payload.UseRegex

	if err := global.DB.Save(&p).Error; err != nil {
		return p, err
	}
	return p, nil
}

func DeleteKeywordProfile(userID uint, profileID uint) error {
	return global.DB.Where("id = ? AND user_id = ?", profileID, userID).Delete(&model.KeywordProfile{}).Error
}
