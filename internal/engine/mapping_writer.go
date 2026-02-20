package engine

import "github.com/gotd/td/tg"

func findDiscussionRootMsgID(res *tg.MessagesDiscussionMessage, linkedChatID int64) int {
	if res == nil || linkedChatID == 0 {
		return 0
	}
	for _, mc := range res.Messages {
		msg, ok := mc.(*tg.Message)
		if !ok || msg == nil || msg.ID <= 0 {
			continue
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok && chID == linkedChatID {
			return msg.ID
		}
	}
	return 0
}
