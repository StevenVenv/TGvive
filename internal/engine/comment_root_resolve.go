package engine

import (
	"context"

	"my-go-server/internal/engine/localdb"

	"github.com/gotd/td/tg"
	"gorm.io/gorm"
)

func hasRootMapping(db *gorm.DB, sourceRootID int) bool {
	if db == nil || sourceRootID <= 0 {
		return false
	}
	var rm localdb.RootMapping
	if err := db.
		Select("source_root_id").
		Where("source_root_id = ?", sourceRootID).
		Limit(1).
		Find(&rm).Error; err != nil {
		return false
	}
	return rm.SourceRootID > 0
}

func resolveSourceRootIDForComment(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, msg *tg.Message) int {
	if msg == nil || msg.ID <= 0 || msg.ReplyTo == nil {
		return 0
	}
	if cfg == nil {
		return 0
	}

	hdr, ok := msg.ReplyTo.(*tg.MessageReplyHeader)
	if !ok || hdr == nil {
		return 0
	}

	// Extract candidate from reply header.
	rootID := extractCommentRootMsgID(msg)
	if rootID <= 0 {
		return 0
	}

	// If reply_to_peer_id points to the source channel, ReplyToMsgID is a channel msg id.
	if peer, ok := hdr.GetReplyToPeerID(); ok {
		if ch, ok := peer.(*tg.PeerChannel); ok && ch != nil {
			if ch.ChannelID == cfg.SourceChannelID && cfg.SourcePeer != nil && api != nil && cfg.SourceLinkedChatID != 0 {
				if channelMsgID, ok := hdr.GetReplyToMsgID(); ok && channelMsgID > 0 {
					if discRootID, err := getDiscussionRootIDWithRetry(ctx, api, cfg.SourcePeer, channelMsgID, cfg.SourceLinkedChatID); err == nil && discRootID > 0 {
						return discRootID
					}
				}
			}
		}
	}

	// If reply_to_top_id exists, it is already the thread root in the linked discussion.
	if _, ok := hdr.GetReplyToTopID(); ok {
		return rootID
	}

	// Nested replies or missing top id: resolve using the comment message itself.
	if api != nil && cfg.SourceLinkedPeer != nil && cfg.SourceLinkedChatID != 0 {
		if discRootID, err := getDiscussionRootIDWithRetry(ctx, api, cfg.SourceLinkedPeer, msg.ID, cfg.SourceLinkedChatID); err == nil && discRootID > 0 {
			return discRootID
		}
	}

	// Fallback: best-effort walk up the chain if RootMapping already exists.
	if api != nil {
		if mapped := resolveMappedSourceRootID(ctx, api, cfg, rootID); mapped > 0 {
			return mapped
		}
	}

	return rootID
}

// resolveMappedSourceRootID tries to resolve a possibly-nested reply root to a mapped discussion-root id.
// It walks up the reply chain within the source linked chat until it finds a RootMapping entry.
func resolveMappedSourceRootID(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, startID int) int {
	if startID <= 0 {
		return 0
	}
	if cfg == nil || cfg.LocalDB == nil {
		return 0
	}

	db := cfg.LocalDB
	id := startID

	seen := make(map[int]struct{}, 8)
	for depth := 0; depth < 8 && id > 0; depth++ {
		if _, ok := seen[id]; ok {
			return 0
		}
		seen[id] = struct{}{}

		if hasRootMapping(db, id) {
			return id
		}

		if api == nil || cfg.SourceLinkedPeer == nil {
			return 0
		}

		msg, err := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, id)
		if err != nil || msg == nil {
			return 0
		}

		next := extractCommentRootMsgID(msg)
		if next <= 0 || next == id {
			return 0
		}
		id = next
	}

	return 0
}
