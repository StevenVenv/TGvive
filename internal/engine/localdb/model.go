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

// RootMapping stores discussion-root mapping for comment mirroring.
// SourceRootID is the source linked-chat root msg id, and TargetRootID is the target linked-chat root msg id.
type RootMapping struct {
	SourceRootID int `gorm:"primaryKey;autoIncrement:false"`
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

const (
	CommentStatusPending = "pending"
	CommentStatusSuccess = "success"
	CommentStatusFailed  = "failed"
)

// CommentQueue stores pending/sent comment mirroring jobs for a task.
type CommentQueue struct {
	MsgID int `gorm:"primaryKey;autoIncrement:false"`

	// ReplyToRootID is the source linked-chat discussion root msg id.
	// Composite index optimizes consumer scan:
	//   WHERE reply_to_root_id=? AND status=? ORDER BY msg_id ASC LIMIT N
	ReplyToRootID int `gorm:"not null;index:idx_comment_pending,priority:1"`

	GroupedID int64 `gorm:"not null;default:0;index"`

	Status string `gorm:"type:varchar(16);not null;default:'pending';index:idx_comment_pending,priority:2"`

	// TargetMsgID is the mirrored message id in target linked-chat (set when status=success).
	TargetMsgID int `gorm:"not null;default:0;index"`

	Payload datatypes.JSON `gorm:"type:json"`

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
