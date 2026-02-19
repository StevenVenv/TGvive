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

	MsgID         int `gorm:"not null;uniqueIndex"`
	ReplyToRootID int `gorm:"not null;index"`

	Status   string `gorm:"type:varchar(16);not null;index"` // pending|success|failed
	Attempts int    `gorm:"not null;default:0"`

	LastError     string    `gorm:"type:varchar(255);default:''"`
	NextAttemptAt time.Time `gorm:"index"`

	LightPayload datatypes.JSON `gorm:"type:json"`

	CreatedAt time.Time
	UpdatedAt time.Time
}
