package engine

import "github.com/gotd/td/tg"

func classifyRootReply(msg *tg.Message) (rootDelta int, replyDelta int) {
	if msg == nil {
		return 0, 0
	}
	if extractReplyToSourceMsgID(msg) > 0 {
		return 0, 1
	}
	return 1, 0
}

func classifyRootReplyBatch(msgs []*tg.Message) (rootDelta int, replyDelta int) {
	for _, msg := range msgs {
		r, rp := classifyRootReply(msg)
		rootDelta += r
		replyDelta += rp
	}
	return rootDelta, replyDelta
}
