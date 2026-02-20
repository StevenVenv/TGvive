package initialize

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func InitDB() error {
	c := global.Config.MySQL
	if strings.TrimSpace(c.Host) == "" || c.Port <= 0 || strings.TrimSpace(c.User) == "" || strings.TrimSpace(c.DBName) == "" {
		return errors.New("mysql config is incomplete (host/port/user/dbname required)")
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", c.User, c.Password, c.Host, c.Port, c.DBName, c.Config)

	logMode := logger.Warn
	if global.Config.Server.Mode == "debug" {
		logMode = logger.Info
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}

	global.DB = db

	if err := global.DB.AutoMigrate(&model.User{}, &model.Task{}, &model.Strategy{}, &model.KeywordProfile{}, &model.MessageMapping{}); err != nil {
		return fmt.Errorf("auto migrate failed: %w", err)
	}

	sqlDB, err := global.DB.DB()
	if err == nil && sqlDB != nil {
		tuneMySQLPool(sqlDB)
	}
	return nil
}

func tuneMySQLPool(db *sql.DB) {
	if db == nil {
		return
	}

	// Conservative defaults; can be overridden by upstream if needed.
	db.SetMaxOpenConns(30)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(2 * time.Hour)
	db.SetConnMaxIdleTime(15 * time.Minute)
}
