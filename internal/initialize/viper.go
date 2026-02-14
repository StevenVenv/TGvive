package initialize

import (
	"fmt"

	"my-go-server/internal/global"

	"github.com/spf13/viper"
)

func InitConfig() {
	v := viper.New()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./configs")
	v.AddConfigPath(".")

	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("mysql.host", "127.0.0.1")
	v.SetDefault("mysql.port", 3306)
	v.SetDefault("mysql.user", "root")
	v.SetDefault("mysql.password", "")
	v.SetDefault("mysql.dbname", "")
	v.SetDefault("mysql.config", "charset=utf8mb4&parseTime=True&loc=Local")
	v.SetDefault("jwt.secret", "change_me")
	v.SetDefault("telegram.api_id", 0)
	v.SetDefault("telegram.api_hash", "")
	v.SetDefault("telegram.session_path", "./sessions/")

	if err := v.ReadInConfig(); err != nil {
		panic(fmt.Errorf("read config failed: %w", err))
	}

	var cfg global.AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		panic(fmt.Errorf("unmarshal config failed: %w", err))
	}

	global.Viper = v
	global.Config = cfg
}
