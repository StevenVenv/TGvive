package model

import "time"

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"type:varchar(255);default:''" json:"-"`
	AuthVersion  uint      `gorm:"not null;default:1" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
