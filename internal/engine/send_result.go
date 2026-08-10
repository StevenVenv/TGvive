package engine

import (
	"context"
	"errors"
	"sort"
	"strings"

	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
)

func extractSentMsgIDs(upd tg.UpdatesClass) []int {
	if upd == nil {
		return nil
	}

	seen := make(map[int]struct{}, 8)
	out := make([]int, 0, 8)
	add := func(id int) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	walkUpdate := func(u tg.UpdateClass) {
		switch v := u.(type) {
		case *tg.UpdateNewMessage:
			if m, ok := v.Message.(*tg.Message); ok && m != nil && m.ID > 0 {
				add(m.ID)
			}
		case *tg.UpdateNewChannelMessage:
			if m, ok := v.Message.(*tg.Message); ok && m != nil && m.ID > 0 {
				add(m.ID)
			}
		case *tg.UpdateMessageID:
			add(v.ID)
		}
	}

	switch v := upd.(type) {
	case *tg.UpdateShortSentMessage:
		add(v.ID)
	case *tg.UpdateShortMessage:
		add(v.ID)
	case *tg.UpdateShortChatMessage:
		add(v.ID)
	case *tg.UpdateShort:
		if v.Update != nil {
			walkUpdate(v.Update)
		}
	case *tg.Updates:
		for _, u := range v.Updates {
			if u != nil {
				walkUpdate(u)
			}
		}
	case *tg.UpdatesCombined:
		for _, u := range v.Updates {
			if u != nil {
				walkUpdate(u)
			}
		}
	}

	if len(out) == 0 {
		return nil
	}
	sort.Ints(out)
	return out
}

func minPositiveInt(ids []int) int {
	min := 0
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if min == 0 || id < min {
			min = id
		}
	}
	return min
}

func sendTextUpdates(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msg *tg.Message, replyTo tg.InputReplyToClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}
	if msg == nil || strings.TrimSpace(msg.Message) == "" {
		return nil, nil
	}

	rid, err := randomID()
	if err != nil {
		return nil, err
	}

	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		ReplyTo:  replyTo,
		Message:  msg.Message,
		RandomID: rid,
	}
	if len(msg.Entities) > 0 {
		req.Entities = msg.Entities
	}

	return sendTelegramUpdatesWithRetry(ctx, "发送文本", sendSubjectMsgID(msg.ID), func(callCtx context.Context) (tg.UpdatesClass, error) {
		return api.MessagesSendMessage(callCtx, req)
	})
}

func sendMediaUpdates(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msg *tg.Message, replyTo tg.InputReplyToClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}
	if msg == nil || msg.Media == nil {
		return nil, nil
	}

	caption := msg.Message
	entities := msg.Entities
	if out, truncated := sanitizeMediaCaptionText(caption); truncated {
		caption = out
		entities = nil
	}

	media, err := convertMessageMediaToInput(msg.Media)
	if err != nil {
		return nil, err
	}

	rid, err := randomID()
	if err != nil {
		return nil, err
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		ReplyTo:  replyTo,
		Media:    media,
		Message:  caption,
		RandomID: rid,
	}
	if len(entities) > 0 {
		req.Entities = entities
	}
	return sendTelegramUpdatesWithRetry(ctx, "发送媒体", sendSubjectMsgID(msg.ID), func(callCtx context.Context) (tg.UpdatesClass, error) {
		return api.MessagesSendMedia(callCtx, req)
	})
}

func sendAlbumUpdates(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msgs []*tg.Message, replyTo tg.InputReplyToClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}

	var mediaMsgs []*tg.Message
	for _, msg := range msgs {
		if msg == nil || msg.Media == nil {
			continue
		}
		if _, err := convertMessageMediaToInput(msg.Media); err != nil {
			continue
		}
		mediaMsgs = append(mediaMsgs, msg)
	}

	switch len(mediaMsgs) {
	case 0:
		return nil, nil
	case 1:
		return sendMediaUpdates(ctx, api, peer, mediaMsgs[0], replyTo)
	}

	multi := make([]tg.InputSingleMedia, 0, len(mediaMsgs))
	for i, msg := range mediaMsgs {
		inputMedia, err := convertMessageMediaToInput(msg.Media)
		if err != nil {
			continue
		}
		rid, err := randomID()
		if err != nil {
			return nil, err
		}

		item := tg.InputSingleMedia{
			Media:    inputMedia,
			RandomID: rid,
		}
		if i == 0 {
			caption := msg.Message
			entities := msg.Entities
			if out, truncated := sanitizeMediaCaptionText(caption); truncated {
				caption = out
				entities = nil
			}
			item.Message = caption
			if len(entities) > 0 {
				item.Entities = entities
			}
		}
		multi = append(multi, item)
	}

	if len(multi) == 0 {
		return nil, nil
	}
	if len(multi) == 1 {
		return sendMediaUpdates(ctx, api, peer, mediaMsgs[0], replyTo)
	}

	req := &tg.MessagesSendMultiMediaRequest{
		Peer:       peer,
		ReplyTo:    replyTo,
		MultiMedia: multi,
	}
	return sendTelegramUpdatesWithRetry(ctx, "发送专辑", sendSubjectAlbum(mediaMsgs[0].GroupedID, len(mediaMsgs)), func(callCtx context.Context) (tg.UpdatesClass, error) {
		return api.MessagesSendMultiMedia(callCtx, req)
	})
}

func forwardMessagesUpdates(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, msgs []*tg.Message, replyTo tg.InputReplyToClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if sourcePeer == nil {
		return nil, errors.New("source peer is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}

	ids := make([]int, 0, len(msgs))
	rids := make([]int64, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.ID <= 0 {
			continue
		}
		rid, err := randomID()
		if err != nil {
			return nil, err
		}
		ids = append(ids, msg.ID)
		rids = append(rids, rid)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	req := &tg.MessagesForwardMessagesRequest{
		FromPeer: sourcePeer,
		ToPeer:   peer,
		ID:       ids,
		RandomID: rids,
	}
	if replyTo != nil {
		req.ReplyTo = replyTo
	}
	subject := sendSubjectAlbum(0, len(ids))
	if len(ids) == 1 {
		subject = sendSubjectMsgID(ids[0])
	}
	return sendTelegramUpdatesWithRetry(ctx, "转发消息", subject, func(callCtx context.Context) (tg.UpdatesClass, error) {
		return api.MessagesForwardMessages(callCtx, req)
	})
}

func (m *TaskManager) ForwardMessagesResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, msgs []*tg.Message) ([]int, error) {
	upd, err := forwardMessagesUpdates(ctx, api, sourcePeer, peer, msgs, nil)
	if err != nil || upd == nil {
		return nil, err
	}
	return extractSentMsgIDs(upd), nil
}

func (m *TaskManager) ForwardMessagesWithFallbackResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass, replyTo tg.InputReplyToClass) ([]int, error) {
	upd, err := forwardMessagesUpdates(ctx, api, sourcePeer, peer, msgs, replyTo)
	if err == nil {
		return extractSentMsgIDs(upd), nil
	}
	if !isForwardOrCopyRestricted(err) {
		return nil, err
	}

	msgID := 0
	for _, msg := range msgs {
		if msg != nil && msg.ID > 0 {
			msgID = msg.ID
			break
		}
	}
	logSmartFallback(task, msgID, err)

	ids := make([]int, 0, len(msgs))

	mediaMsgs := make([]*tg.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Media == nil {
			sent, serr := m.SendTextResult(ctx, api, msg, task, peer)
			if serr != nil {
				return nil, serr
			}
			ids = append(ids, sent...)
			continue
		}
		mediaMsgs = append(mediaMsgs, msg)
	}

	switch len(mediaMsgs) {
	case 0:
		return ids, nil
	case 1:
		sent, serr := m.SendUploadedMediaResult(ctx, api, sourcePeer, mediaMsgs[0], task, peer)
		if serr != nil {
			return nil, serr
		}
		ids = append(ids, sent...)
	default:
		sent, serr := m.SendUploadedAlbumResult(ctx, api, sourcePeer, mediaMsgs, task, peer)
		if serr != nil {
			return nil, serr
		}
		ids = append(ids, sent...)
	}

	sort.Ints(ids)
	return ids, nil
}

func (m *TaskManager) SendTextResult(ctx context.Context, api *tg.Client, msg *tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	replyTo := buildKeepReplyInput(ctx, task, msg)
	upd, err := sendTextUpdates(ctx, api, peer, msg, replyTo)
	if err != nil || upd == nil {
		return nil, err
	}

	ids := extractSentMsgIDs(upd)
	if msg != nil && msg.ID > 0 {
		if sentID := minPositiveInt(ids); sentID > 0 {
			storeMsgMapping(task, msg.ID, sentID)
		}
	}
	return ids, nil
}

func (m *TaskManager) SendMediaResult(ctx context.Context, api *tg.Client, msg *tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	replyTo := buildKeepReplyInput(ctx, task, msg)
	upd, err := sendMediaUpdates(ctx, api, peer, msg, replyTo)
	if err != nil || upd == nil {
		return nil, err
	}

	ids := extractSentMsgIDs(upd)
	if msg != nil && msg.ID > 0 {
		if sentID := minPositiveInt(ids); sentID > 0 {
			storeMsgMapping(task, msg.ID, sentID)
		}
	}
	return ids, nil
}

func (m *TaskManager) SendAlbumResult(ctx context.Context, api *tg.Client, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	var mediaMsgs []*tg.Message
	for _, msg := range msgs {
		if msg == nil || msg.Media == nil {
			continue
		}
		if _, err := convertMessageMediaToInput(msg.Media); err != nil {
			continue
		}
		mediaMsgs = append(mediaMsgs, msg)
	}
	if len(mediaMsgs) == 0 {
		return nil, nil
	}
	if len(mediaMsgs) == 1 {
		return m.SendMediaResult(ctx, api, mediaMsgs[0], task, peer)
	}

	replyCarrier := mediaMsgs[0]
	for _, mm := range mediaMsgs {
		if extractReplyToSourceMsgID(mm) > 0 {
			replyCarrier = mm
			break
		}
	}
	replyTo := buildKeepReplyInput(ctx, task, replyCarrier)

	upd, err := sendAlbumUpdates(ctx, api, peer, mediaMsgs, replyTo)
	if err != nil || upd == nil {
		return nil, err
	}
	ids := extractSentMsgIDs(upd)
	storeMsgMappingsInOrder(task, mediaMsgs, ids)
	return ids, nil
}

func (m *TaskManager) SendMediaWithFallbackResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	ids, err := m.SendMediaResult(ctx, api, msg, task, peer)
	if err == nil {
		return ids, nil
	}
	if !isForwardOrCopyRestricted(err) {
		return nil, err
	}

	msgID := 0
	if msg != nil {
		msgID = msg.ID
	}
	logSmartFallback(task, msgID, err)

	return m.SendUploadedMediaResult(ctx, api, sourcePeer, msg, task, peer)
}

func (m *TaskManager) SendAlbumWithFallbackResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	ids, err := m.SendAlbumResult(ctx, api, msgs, task, peer)
	if err == nil {
		return ids, nil
	}
	if !isForwardOrCopyRestricted(err) {
		return nil, err
	}

	msgID := 0
	for _, msg := range msgs {
		if msg != nil && msg.ID > 0 {
			msgID = msg.ID
			break
		}
	}
	logSmartFallback(task, msgID, err)

	return m.SendUploadedAlbumResult(ctx, api, sourcePeer, msgs, task, peer)
}

func (m *TaskManager) SendUploadedMediaResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	upd, err := m.sendUploadedMediaUpdates(ctx, api, sourcePeer, msg, task, peer)
	if err != nil || upd == nil {
		return nil, err
	}
	return extractSentMsgIDs(upd), nil
}

func (m *TaskManager) SendUploadedAlbumResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	upd, err := m.sendUploadedAlbumUpdates(ctx, api, sourcePeer, msgs, task, peer)
	if err != nil || upd == nil {
		return nil, err
	}
	return extractSentMsgIDs(upd), nil
}
