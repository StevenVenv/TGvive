package initialize

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"my-go-server/internal/global"
	"my-go-server/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestEnsureAdminUserCreatesDefaultAdminWhenNoUsers(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate user: %v", err)
	}

	oldDB := global.DB
	oldConfig := global.Config
	oldLogger := global.Logger
	t.Cleanup(func() {
		global.DB = oldDB
		global.Config = oldConfig
		global.Logger = oldLogger
	})

	global.DB = db
	global.Logger = nil
	global.Config.Server.Mode = "release"

	if err := EnsureAdminUser(); err != nil {
		t.Fatalf("EnsureAdminUser returned error: %v", err)
	}

	var user model.User
	if err := db.Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("query admin user: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("expected created admin user to have a primary key")
	}
	if user.PasswordHash == "" {
		t.Fatal("expected created admin user to have a password hash")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(defaultAdminPassword)); err != nil {
		t.Fatalf("expected default password hash to match: %v", err)
	}
}

func TestEnsureAdminUserDoesNotCreateDuplicateWhenUserExists(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate user: %v", err)
	}

	oldDB := global.DB
	oldConfig := global.Config
	oldLogger := global.Logger
	t.Cleanup(func() {
		global.DB = oldDB
		global.Config = oldConfig
		global.Logger = oldLogger
	})

	global.DB = db
	global.Logger = nil

	existing := model.User{Username: "renamed-admin", PasswordHash: "hash"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	if err := EnsureAdminUser(); err != nil {
		t.Fatalf("EnsureAdminUser returned error: %v", err)
	}

	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one user after init, got %d", count)
	}
}
