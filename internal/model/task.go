package model

import "gorm.io/gorm"

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

	// 克隆模式: 1-转发, 2-发送, 3-下载上传
	CloneMode int `gorm:"type:tinyint;not null" json:"clone_mode"`

	// 内容类型 (DB 存 "text,image,video"，API 传 JSON 数组)
	ContentTypes CSVStringSlice `gorm:"type:varchar(100)" json:"content_types"`

	// 消息范围: 1-全部, 2-最近N条, 3-时间范围, 4-ID范围
	ScopeType  int    `gorm:"type:tinyint;not null" json:"scope_type"`
	ScopeValue string `gorm:"type:varchar(255)" json:"scope_value"` // 存储具体的N条或时间范围

	// 开关配置 (布尔值)
	KeepReply    bool `gorm:"default:false" json:"keep_reply"`    // 保留回复关系
	Realtime     bool `gorm:"default:false" json:"realtime"`      // 实时监控
	CloneComment bool `gorm:"default:false" json:"clone_comment"` // 克隆评论
	GpuAccel     bool `gorm:"default:false" json:"gpu_accel"`     // GPU 加速

	// 运行状态: 0-停止, 1-运行中, 2-暂停, 3-异常
	Status int `gorm:"type:tinyint;default:0" json:"status"`

	// 历史克隆进度（断点续传）
	HistoryCursor int `gorm:"type:int;default:0" json:"history_cursor"` // 最近一次成功搬运的消息 ID
	// 历史克隆方向：1-从旧到新，2-从新到旧
	HistoryOrder int `gorm:"type:tinyint;default:1" json:"history_order"`
}
