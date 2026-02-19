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

	var walkUpdate func(u tg.UpdateClass)
	walkUpdate = func(u tg.UpdateClass) {
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

func sendTextUpdates(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msg *tg.Message) (tg.UpdatesClass, error) {
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
		Message:  msg.Message,
		RandomID: rid,
	}
	if len(msg.Entities) > 0 {
		req.Entities = msg.Entities
	}

	return api.MessagesSendMessage(ctx, req)
}

func sendMediaUpdates(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msg *tg.Message) (tg.UpdatesClass, error) {
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
		Media:    media,
		Message:  msg.Message,
		RandomID: rid,
	}
	if len(msg.Entities) > 0 {
		req.Entities = msg.Entities
	}
	return api.MessagesSendMedia(ctx, req)
}

func sendAlbumUpdates(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msgs []*tg.Message) (tg.UpdatesClass, error) {
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
		return sendMediaUpdates(ctx, api, peer, mediaMsgs[0])
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
			item.Message = msg.Message
			if len(msg.Entities) > 0 {
				item.Entities = msg.Entities
			}
		}
		multi = append(multi, item)
	}

	if len(multi) == 0 {
		return nil, nil
	}
	if len(multi) == 1 {
		return sendMediaUpdates(ctx, api, peer, mediaMsgs[0])
	}

	return api.MessagesSendMultiMedia(ctx, &tg.MessagesSendMultiMediaRequest{
		Peer:       peer,
		MultiMedia: multi,
	})
}

func forwardMessagesUpdates(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, msgs []*tg.Message) (tg.UpdatesClass, error) {
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

	return api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: sourcePeer,
		ToPeer:   peer,
		ID:       ids,
		RandomID: rids,
	})
}

func (m *TaskManager) ForwardMessagesResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, msgs []*tg.Message) ([]int, error) {
	upd, err := forwardMessagesUpdates(ctx, api, sourcePeer, peer, msgs)
	if err != nil || upd == nil {
		return nil, err
	}
	return extractSentMsgIDs(upd), nil
}

func (m *TaskManager) ForwardMessagesWithFallbackResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	upd, err := forwardMessagesUpdates(ctx, api, sourcePeer, peer, msgs)
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
			sent, serr := m.SendTextResult(ctx, api, peer, msg)
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

func (m *TaskManager) SendTextResult(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msg *tg.Message) ([]int, error) {
	upd, err := sendTextUpdates(ctx, api, peer, msg)
	if err != nil || upd == nil {
		return nil, err
	}
	return extractSentMsgIDs(upd), nil
}

func (m *TaskManager) SendMediaResult(ctx context.Context, api *tg.Client, msg *tg.Message, peer tg.InputPeerClass) ([]int, error) {
	upd, err := sendMediaUpdates(ctx, api, peer, msg)
	if err != nil || upd == nil {
		return nil, err
	}
	return extractSentMsgIDs(upd), nil
}

func (m *TaskManager) SendAlbumResult(ctx context.Context, api *tg.Client, msgs []*tg.Message, peer tg.InputPeerClass) ([]int, error) {
	upd, err := sendAlbumUpdates(ctx, api, peer, msgs)
	if err != nil || upd == nil {
		return nil, err
	}
	return extractSentMsgIDs(upd), nil
}

func (m *TaskManager) SendMediaWithFallbackResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) ([]int, error) {
	upd, err := sendMediaUpdates(ctx, api, peer, msg)
	if err == nil {
		return extractSentMsgIDs(upd), nil
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
	upd, err := sendAlbumUpdates(ctx, api, peer, msgs)
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
