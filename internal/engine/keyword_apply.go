package engine

import "github.com/gotd/td/tg"

func applyKeywordPolicyToMessage(msg *tg.Message, kw *keywordPolicy) (*tg.Message, bool) {
	if kw == nil || msg == nil {
		return msg, false
	}
	if kw.shouldSkip(msg.Message) {
		return msg, true
	}
	out, changed := kw.replaceText(msg.Message)
	if !changed {
		return msg, false
	}
	cp := *msg
	cp.Message = out
	// Drop entities because offsets will likely be invalid after replacement.
	cp.Entities = nil
	return &cp, false
}
