package model

import (
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Strategy stores reusable task configuration templates.
// It is intentionally kept close to Task's config fields so Tasks can reference StrategyID later.
type Strategy struct {
	gorm.Model
	UserID uint `gorm:"index;not null" json:"user_id"` // 归属用户

	// Basic info
	Name   string `gorm:"type:varchar(64);not null" json:"name"`                // 策略名称
	Remark string `gorm:"type:varchar(255);default:''" json:"remark,omitempty"` // 备注

	// 克隆模式: 1-转发, 2-发送, 3-下载上传
	CloneMode int `gorm:"type:tinyint;not null" json:"clone_mode"`

	// AllowedTypes controls message content type filtering.
	// DB stores CSV like "text,image,video" while API uses JSON string array.
	// Empty means allow all types.
	AllowedTypes CSVStringSlice `gorm:"type:varchar(100)" json:"allowed_types"`

	// ContentTypes is kept for backward compatibility (legacy field).
	ContentTypes CSVStringSlice `gorm:"type:varchar(100)" json:"content_types"`

	// File extension (suffix) filters (only apply to "file" content type).
	// DB stores CSV like ".zip,.apk", API uses JSON string array.
	// Empty means allow all file suffixes.
	BlockFileExts CSVStringSlice `gorm:"type:varchar(255)" json:"block_file_exts"`
	AllowFileExts CSVStringSlice `gorm:"type:varchar(255)" json:"allow_file_exts"`

	// 消息范围: 1-全部, 2-最近N条, 3-时间范围, 4-ID范围
	ScopeType  int    `gorm:"type:tinyint;not null" json:"scope_type"`
	ScopeValue string `gorm:"type:varchar(255)" json:"scope_value"`

	// 历史克隆方向：1-从旧到新，2-从新到旧
	HistoryOrder int `gorm:"type:tinyint;default:1" json:"history_order"`

	// PollInterval controls fallback polling in realtime mode (seconds).
	// 0 = disabled (push only), >0 = poll newest message every N seconds.
	PollInterval int `gorm:"type:int;default:0" json:"poll_interval"`

	// EnableRealtime controls push mode (UpdateDispatcher).
	EnableRealtime bool `gorm:"default:false" json:"enable_realtime"`

	// ScheduleRules stores slot based limits, e.g. [{start:"10:00",end:"11:00",limit:2}, ...]
	ScheduleRules datatypes.JSON `gorm:"type:json" json:"schedule_rules"`

	// CommentRule stores comment mirroring rules.
	CommentRule datatypes.JSON `gorm:"type:json" json:"comment_rule"`

	// 开关配置 (布尔值)
	KeepReply bool `gorm:"default:false" json:"keep_reply"`
	// Realtime is kept for backward compatibility (legacy field).
	Realtime     bool `gorm:"default:false" json:"realtime"`
	CloneComment bool `gorm:"default:false" json:"clone_comment"`
	GpuAccel     bool `gorm:"default:false" json:"gpu_accel"`
	ChangeMD5    bool `gorm:"default:false" json:"change_md5"`
	// EnableMediaEdit controls whether to apply media processors (image/video watermark, cover extraction, etc.).
	// It is only effective in CloneMode=3 (Upload). For other modes it will be force-disabled at runtime.
	EnableMediaEdit bool `gorm:"default:false" json:"enable_media_edit"`

	// Anti-detection delays (random interval): sleep after each successfully processed message.
	DelayMinMs int `gorm:"type:int;default:0" json:"delay_min_ms"`
	DelayMaxMs int `gorm:"type:int;default:0" json:"delay_max_ms"`

	// Quota & scheduler
	DailyLimit int    `gorm:"type:int;default:0" json:"daily_limit"` // 0 = unlimited
	RunWindow  string `gorm:"type:varchar(32);default:''" json:"run_window"`
}
