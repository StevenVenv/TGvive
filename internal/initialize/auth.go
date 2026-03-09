package initialize

import (
	"errors"
	"fmt"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	defaultAdminUsername = "admin"
	defaultAdminPassword = "123456"
)

func EnsureAdminUser() error {
	if global.DB == nil {
		return errors.New("db not initialized")
	}

	var u model.User
	queryErr := global.DB.Where("username = ?", defaultAdminUsername).First(&u).Error
	if queryErr != nil && !errors.Is(queryErr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("query admin user failed: %w", queryErr)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password failed: %w", err)
	}

	if queryErr == nil {
		if strings.TrimSpace(u.PasswordHash) == "" {
			if err := global.DB.Model(&model.User{}).Where("id = ?", u.ID).Update("password_hash", string(hash)).Error; err != nil {
				return fmt.Errorf("update default admin password failed: %w", err)
			}
			if global.Logger != nil {
				global.Logger.Warn("default admin password initialized", zap.String("username", defaultAdminUsername), zap.Uint("user_id", u.ID))
			}
		}
		return nil
	}

	var total int64
	if err := global.DB.Model(&model.User{}).Count(&total).Error; err != nil {
		return fmt.Errorf("count users failed: %w", err)
	}
	if total > 0 {
		return nil
	}

	u = model.User{
		Username:     defaultAdminUsername,
		PasswordHash: string(hash),
	}
	if err := global.DB.Create(&u).Error; err != nil {
		return fmt.Errorf("create default admin user failed: %w", err)
	}
	if global.Logger != nil {
		global.Logger.Warn(
			"default admin user created",
			zap.String("username", defaultAdminUsername),
			zap.Uint("user_id", u.ID),
		)
	}
	return nil
}
