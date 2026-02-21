package localdb

import (
	"time"

	"gorm.io/datatypes"
)

type LocalMapping struct {
	ID uint `gorm:"primaryKey"`

	SourceRootID int `gorm:"not null;uniqueIndex"`
	TargetRootID int `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// LocalMsgMapping stores (source msg id -> target msg id) mapping for a task.
// It is used to rebuild reply relationships (keep_reply) and other cross-message references.
type LocalMsgMapping struct {
	ID uint `gorm:"primaryKey"`

	SourceMsgID int `gorm:"not null;uniqueIndex"`
	TargetMsgID int `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type LocalComment struct {
	ID uint `gorm:"primaryKey"`

	// SourcePostID is the source linked-chat discussion root msg id.
	// Composite index optimizes consumer scan:
	//   WHERE source_post_id=? AND is_forwarded=? ORDER BY comment_msg_id ASC LIMIT N
	SourcePostID int `gorm:"not null;index:idx_comment_pending,priority:1"`

	CommentMsgID int `gorm:"not null;uniqueIndex;index:idx_comment_pending,priority:3"`

	GroupedID int64 `gorm:"not null;default:0;index"`

	LightPayload datatypes.JSON `gorm:"type:json"`

	IsForwarded bool `gorm:"not null;default:false;index:idx_comment_pending,priority:2"`

	CreatedAt time.Time
	UpdatedAt time.Time
}
