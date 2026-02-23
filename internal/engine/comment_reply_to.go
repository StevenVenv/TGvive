package engine

import "github.com/gotd/td/tg"

// extractCommentReplyToMsgIDInLinkedChat extracts the message id that a comment replies to,
// but only when the reply target is within the same linked discussion chat.
//
// It returns 0 when:
// - the comment is a top-level reply to the discussion root
// - the reply points to the source channel post (cross-peer), or any other peer
func extractCommentReplyToMsgIDInLinkedChat(msg *tg.Message, sourceRootID int, cfg *commentPipelineConfig) int {
	if msg == nil || msg.ReplyTo == nil || sourceRootID <= 0 {
		return 0
	}
	hdr, ok := msg.ReplyTo.(*tg.MessageReplyHeader)
	if !ok || hdr == nil {
		return 0
	}

	mid, ok := hdr.GetReplyToMsgID()
	if !ok || mid <= 0 || mid == sourceRootID {
		return 0
	}

	// When reply_to_peer_id is present, ensure it points to the linked chat itself.
	if peer, ok := hdr.GetReplyToPeerID(); ok && peer != nil {
		if ch, ok := peer.(*tg.PeerChannel); ok && ch != nil {
			if cfg != nil && cfg.SourceLinkedChatID != 0 && ch.ChannelID == cfg.SourceLinkedChatID {
				return mid
			}
			return 0
		}
		// Unknown/cross-peer reply target: don't try to keep it.
		return 0
	}

	// No explicit peer: treat it as a reply within the same linked chat.
	return mid
}
