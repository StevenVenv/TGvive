package initialize

import (
	"fmt"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func InitDB() {
	c := global.Config.MySQL
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
		panic("database connection failed: " + err.Error())
	}

	global.DB = db

	if err := global.DB.AutoMigrate(&model.User{}, &model.Task{}, &model.Strategy{}, &model.KeywordProfile{}, &model.MessageMapping{}); err != nil {
		panic("auto migrate failed: " + err.Error())
	}
}
