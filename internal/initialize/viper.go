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
	v.SetDefault("telegram.session_key", "")

	// Processor defaults (all disabled by default).
	v.SetDefault("processor.text.enabled", false)
	v.SetDefault("processor.text.trim_space", true)
	v.SetDefault("processor.text.collapse_blank_lines", true)
	v.SetDefault("processor.text.remove_lines_containing", []string{})

	v.SetDefault("processor.image.enabled", false)
	v.SetDefault("processor.image.max_width", 0)
	v.SetDefault("processor.image.max_height", 0)
	v.SetDefault("processor.image.output_quality", 85)
	v.SetDefault("processor.image.watermark.enabled", false)
	v.SetDefault("processor.image.watermark.type", "text")
	v.SetDefault("processor.image.watermark.text", "")
	v.SetDefault("processor.image.watermark.image_path", "")
	v.SetDefault("processor.image.watermark.font_path", "")
	v.SetDefault("processor.image.watermark.font_size", 24.0)
	v.SetDefault("processor.image.watermark.opacity", 0.25)
	v.SetDefault("processor.image.watermark.position", "bottom_right")
	v.SetDefault("processor.image.watermark.margin", 16)
	v.SetDefault("processor.image.watermark.scale", 0.2)

	v.SetDefault("processor.video.enabled", false)
	v.SetDefault("processor.video.ffmpeg_path", "ffmpeg")
	v.SetDefault("processor.video.extract_cover", true)
	v.SetDefault("processor.video.cover_timestamp_sec", 0.0)
	v.SetDefault("processor.video.cover_max_width", 720)
	v.SetDefault("processor.video.cover_max_height", 0)
	v.SetDefault("processor.video.cover_quality", 85)

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
