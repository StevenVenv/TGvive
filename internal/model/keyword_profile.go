package model

import (
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ReplaceRule struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type KeywordRule struct {
	Content string `json:"content"`
	IsRegex bool   `json:"is_regex"`
}

// KeywordProfile stores keyword based filtering & replacement rules.
// It is designed to be reusable across tasks (content control strategy).
type KeywordProfile struct {
	gorm.Model
	UserID uint `gorm:"index;not null" json:"user_id"` // 归属用户

	Name string `gorm:"type:varchar(64);not null" json:"name"` // 策略名称

	// Remark: 备注说明（可选）
	Remark string `gorm:"type:varchar(255);default:''" json:"remark"`

	// BlockWords: 屏蔽词（命中则跳过）
	BlockWords datatypes.JSON `gorm:"type:json" json:"block_words"`
	// AllowWords: 白名单（若非空则必须命中）
	AllowWords datatypes.JSON `gorm:"type:json" json:"allow_words"`
	// ReplaceRules: 文本替换规则（from -> to）
	ReplaceRules datatypes.JSON `gorm:"type:json" json:"replace_rules"`
}
