package localdb

import (
	"time"

	"gorm.io/datatypes"
)

type LocalMapping struct {
	ID uint `gorm:"primaryKey"`

	SourceRootID int `gorm:"not null;uniqueIndex"`
	TargetRootID int `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type LocalComment struct {
	ID uint `gorm:"primaryKey"`

	SourcePostID int `gorm:"not null;index"` // source linked-chat discussion root msg id
	CommentMsgID int `gorm:"not null;uniqueIndex"`

	LightPayload datatypes.JSON `gorm:"type:json"`

	IsForwarded bool `gorm:"not null;default:false;index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}
