package model

import "gorm.io/gorm"

// MessageMapping stores the mapping between discussion-root messages
// so comments can "find their home" across channels.
//
// Note: SourceMsgID/TargetMsgID are discussion group root message IDs (not channel post IDs).
type MessageMapping struct {
	gorm.Model

	SourceChannelID int64 `gorm:"type:bigint;not null;index:idx_mapping_src,priority:1;index:uk_mapping_src_target,unique,priority:1" json:"source_channel_id"`
	SourceMsgID     int   `gorm:"type:int;not null;index:idx_mapping_src,priority:2;index:uk_mapping_src_target,unique,priority:2" json:"source_msg_id"`

	TargetChannelID int64 `gorm:"type:bigint;not null;index:uk_mapping_src_target,unique,priority:3" json:"target_channel_id"`
	TargetMsgID     int   `gorm:"type:int;not null" json:"target_msg_id"`
}
