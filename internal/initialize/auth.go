package initialize

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func EnsureAdminUser() error {
	if global.DB == nil {
		return errors.New("db not initialized")
	}

	username := strings.TrimSpace(global.Config.Auth.AdminUsername)
	if username == "" {
		username = "admin"
	}
	password := strings.TrimSpace(global.Config.Auth.AdminPassword)
	mode := strings.ToLower(strings.TrimSpace(global.Config.Server.Mode))

	var u model.User
	err := global.DB.Where("username = ?", username).First(&u).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("query admin user failed: %w", err)
	}

	needInit := errors.Is(err, gorm.ErrRecordNotFound) || strings.TrimSpace(u.PasswordHash) == ""

	if password == "" {
		if !needInit {
			return nil
		}
		if mode != "debug" {
			return errors.New("auth.admin_password is required for first-time setup (or set it via env: TGVIVE_AUTH_ADMIN_PASSWORD)")
		}
		pw, perr := generateRandomPassword(18)
		if perr != nil {
			return fmt.Errorf("generate admin password failed: %w", perr)
		}
		password = pw

		// Show password explicitly only in debug mode to avoid accidental leakage.
		if global.Logger != nil {
			global.Logger.Warn("generated admin password (debug only)",
				zap.String("username", username),
				zap.String("password", password),
			)
		} else {
			_, _ = fmt.Fprintln(os.Stderr, "generated admin password (debug only):", username, password)
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password failed: %w", err)
	}

	// If password is explicitly configured, treat it as source of truth and apply on each boot.
	forceUpdate := strings.TrimSpace(global.Config.Auth.AdminPassword) != ""

	if errors.Is(err, gorm.ErrRecordNotFound) {
		u = model.User{
			Username:     username,
			PasswordHash: string(hash),
		}
		if err := global.DB.Create(&u).Error; err != nil {
			return fmt.Errorf("create admin user failed: %w", err)
		}
		if global.Logger != nil {
			global.Logger.Info("admin user created", zap.String("username", username), zap.Uint("user_id", u.ID))
		}
		return nil
	}

	if forceUpdate || strings.TrimSpace(u.PasswordHash) == "" {
		if err := global.DB.Model(&u).Update("password_hash", string(hash)).Error; err != nil {
			return fmt.Errorf("update admin password failed: %w", err)
		}
		if global.Logger != nil {
			global.Logger.Info("admin password updated", zap.String("username", username), zap.Uint("user_id", u.ID))
		}
	}

	return nil
}

func generateRandomPassword(n int) (string, error) {
	if n < 12 {
		n = 12
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := 0; i < len(b); i++ {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}
