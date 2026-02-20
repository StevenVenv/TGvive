package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	TaskStatusStopped = 0
	TaskStatusRunning = 1
	TaskStatusPaused  = 2
	TaskStatusError   = 3
)

const (
	// HistoryOrderOldToNew clones messages in ascending ID order.
	HistoryOrderOldToNew = 1
	// HistoryOrderNewToOld clones messages in descending ID order.
	HistoryOrderNewToOld = 2
)

type Task struct {
	gorm.Model
	UserID    uint   `gorm:"index;not null" json:"user_id"`                // 归属用户
	SourceURL string `gorm:"type:varchar(255);not null" json:"source_url"` // 对方频道/群组
	TargetURL string `gorm:"type:varchar(255);not null" json:"target_url"` // 自己频道/群组

	// ExecuteBy 指定执行账号（session key，对应 sessions/session_{key}.json）。
	ExecuteBy string `gorm:"type:varchar(64);default:'';index" json:"session_key"`

	// StrategyID 关联策略模板（Strategy）。
	StrategyID uint `gorm:"index;default:0" json:"strategy_id"`

	// KeywordProfileID 关联关键词/过滤策略（KeywordProfile），允许为空（0 表示未配置）。
	KeywordProfileID uint `gorm:"index;default:0" json:"keyword_profile_id,omitempty"`

	// SourceChannelID 用于实时监听时的路由（SourceURL 解析后的数值 ID）
	SourceChannelID int64 `gorm:"type:bigint;default:0;index" json:"source_channel_id"`

	// 克隆模式: 1-转发, 2-发送, 3-下载上传
	CloneMode int `gorm:"type:tinyint;not null" json:"clone_mode"`

	// 内容类型 (DB 存 "text,image,video"，API 传 JSON 数组)
	ContentTypes CSVStringSlice `gorm:"type:varchar(100)" json:"content_types"`

	// File extension (suffix) filters (only apply to "file" content type).
	// DB stores CSV like ".zip,.apk", API uses JSON string array.
	// Empty means allow all file suffixes.
	BlockFileExts CSVStringSlice `gorm:"type:varchar(255)" json:"block_file_exts"`
	AllowFileExts CSVStringSlice `gorm:"type:varchar(255)" json:"allow_file_exts"`

	// 消息范围: 1-全部, 2-最近N条, 3-时间范围, 4-ID范围
	ScopeType  int    `gorm:"type:tinyint;not null" json:"scope_type"`
	ScopeValue string `gorm:"type:varchar(255)" json:"scope_value"` // 存储具体的N条或时间范围

	// 开关配置 (布尔值)
	KeepReply    bool `gorm:"default:false" json:"keep_reply"`    // 保留回复关系
	Realtime     bool `gorm:"default:false" json:"realtime"`      // 实时监控
	CloneComment bool `gorm:"default:false" json:"clone_comment"` // 克隆评论
	GpuAccel     bool `gorm:"default:false" json:"gpu_accel"`     // GPU 加速
	ChangeMD5    bool `gorm:"default:false" json:"change_md5"`    // 下载上传时修改文件 MD5
	// EnableMediaEdit controls whether to apply media processors (image/video watermark, cover extraction, etc.).
	// It is only effective in CloneMode=3 (Upload). For other modes it will be force-disabled at runtime.
	EnableMediaEdit bool `gorm:"default:false" json:"enable_media_edit"`

	// Anti-detection delays (random interval): sleep after each successfully processed message.
	DelayMinMs int `gorm:"type:int;default:0" json:"delay_min_ms"`
	DelayMaxMs int `gorm:"type:int;default:0" json:"delay_max_ms"`

	// Quota & scheduler
	DailyLimit int    `gorm:"type:int;default:0" json:"daily_limit"` // 0 = unlimited
	TodayCount int    `gorm:"type:int;default:0" json:"today_count"`
	TodayDate  string `gorm:"type:varchar(10);default:''" json:"today_date"` // YYYY-MM-DD (server local)
	RunWindow  string `gorm:"type:varchar(32);default:''" json:"run_window"` // e.g. "09:00-18:00", empty = always

	// 运行状态: 0-停止, 1-运行中, 2-暂停, 3-异常
	Status int `gorm:"type:tinyint;default:0" json:"status"`

	// LastError stores last fatal error message for debugging (persisted to DB).
	LastError string `gorm:"type:varchar(255);default:''" json:"last_error,omitempty"`

	// CurrentSlotCount/Key persist scheduler slot quota across restarts.
	CurrentSlotCount int    `gorm:"type:int;default:0" json:"current_slot_count"`
	CurrentSlotKey   string `gorm:"type:varchar(32);default:''" json:"current_slot_key"`

	// NextRunTime is the next wake-up time calculated by scheduler (optional).
	NextRunTime *time.Time `gorm:"type:datetime" json:"next_run_time,omitempty"`

	// 历史克隆进度（断点续传）
	HistoryCursor int `gorm:"type:int;default:0" json:"history_cursor"` // 最近一次成功转发的消息 ID
	// HistoryMaxID tracks the max message id ever successfully processed (used for 追更 in new->old mode).
	HistoryMaxID int `gorm:"type:int;default:0" json:"history_max_id"`
	// 历史克隆方向：1-从旧到新，2-从新到旧
	HistoryOrder int `gorm:"type:tinyint;default:1" json:"history_order"`
}
