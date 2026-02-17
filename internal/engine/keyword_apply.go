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

func applyKeywordPolicyToAlbum(m *TaskManager, msgs []*tg.Message, allowedTypes map[string]struct{}, kw *keywordPolicy) ([]*tg.Message, bool) {
	if kw == nil || m == nil || len(msgs) == 0 {
		return msgs, false
	}

	// Album caption/entities are kept only on the first (sendable) item after ordering.
	// processAlbumBatch sorts by ID asc, and SendAlbum/SendUploadedAlbum also skip unsupported media,
	// so we pick the smallest ID among supported items as the caption carrier.
	captionIdx := -1
	captionID := 0
	for i, msg := range msgs {
		if msg == nil || msg.Media == nil {
			continue
		}
		if allowedTypes != nil {
			ct := m.DetectContentType(msg)
			if _, ok := allowedTypes[ct]; !ok {
				continue
			}
		}
		if _, err := convertMessageMediaToInput(msg.Media); err != nil {
			continue
		}
		if captionIdx == -1 || msg.ID < captionID {
			captionIdx = i
			captionID = msg.ID
		}
	}
	if captionIdx == -1 {
		return msgs, false
	}

	updated, skip := applyKeywordPolicyToMessage(msgs[captionIdx], kw)
	if skip {
		return msgs, true
	}
	if updated != nil && updated != msgs[captionIdx] {
		copied := make([]*tg.Message, len(msgs))
		copy(copied, msgs)
		copied[captionIdx] = updated
		return copied, false
	}
	return msgs, false
}

