package initialize

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"my-go-server/internal/global"

	"github.com/spf13/viper"
)

func InitConfig() error {
	v := viper.New()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./configs")
	v.AddConfigPath(".")

	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "release")
	v.SetDefault("server.allow_anonymous_debug", false)
	v.SetDefault("server.shutdown_timeout_sec", 10)

	v.SetDefault("server.cors.allow_origins", []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://localhost:4173",
		"http://127.0.0.1:4173",
	})
	v.SetDefault("server.cors.allow_methods", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
	v.SetDefault("server.cors.allow_headers", []string{"Content-Type", "Authorization"})
	v.SetDefault("server.cors.expose_headers", []string{})
	v.SetDefault("server.cors.allow_credentials", true)
	v.SetDefault("server.cors.max_age_sec", 600)

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

	v.SetEnvPrefix("TGVIVE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Prefer a local, ignored config file for secrets:
	// - configs/config.local.yaml (recommended)
	// - config.local.yaml
	//
	// Or override explicitly via env: TGVIVE_CONFIG_FILE=/path/to/config.yaml
	if p := strings.TrimSpace(os.Getenv("TGVIVE_CONFIG_FILE")); p != "" {
		v.SetConfigFile(p)
	} else {
		if _, err := os.Stat("./configs/config.local.yaml"); err == nil {
			v.SetConfigFile("./configs/config.local.yaml")
		} else if _, err := os.Stat("./config.local.yaml"); err == nil {
			v.SetConfigFile("./config.local.yaml")
		}
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("read config failed: %w", err)
		}
	}

	var cfg global.AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("unmarshal config failed: %w", err)
	}

	// Basic hardening: forbid shipping with default JWT secret outside debug.
	mode := strings.ToLower(strings.TrimSpace(cfg.Server.Mode))
	secret := strings.TrimSpace(cfg.JWT.Secret)
	if mode != "debug" && (secret == "" || secret == "change_me") {
		return fmt.Errorf("invalid jwt.secret in %q mode: must be set to a strong random value", cfg.Server.Mode)
	}

	global.Viper = v
	global.Config = cfg
	return nil
}
