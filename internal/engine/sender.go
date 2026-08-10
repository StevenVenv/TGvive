package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

func isForwardOrCopyRestricted(err error) bool {
	if err == nil {
		return false
	}

	if tgerr.Is(err,
		"CHAT_FORWARDS_RESTRICTED",
		"FORWARDS_RESTRICTED",
		"RIGHTS_NOT_ENOUGH",
	) {
		return true
	}

	if rpcErr, ok := tgerr.As(err); ok {
		t := strings.ToUpper(strings.TrimSpace(rpcErr.Type))
		return strings.Contains(t, "FORWARDS_RESTRICTED")
	}

	return false
}

func logSmartFallback(task model.Task, msgID int, err error) {
	line := fmt.Sprintf("Forward/Copy restricted, automatically falling back to Upload mode for msgID: %d", msgID)
	if global.Logger != nil {
		global.Logger.Warn(
			line,
			zap.Uint("task_id", task.ID),
			zap.Int("msg_id", msgID),
			zap.Error(err),
		)
	}
	global.BroadcastLog("[WARN] " + line)
}

// ForwardMessages forwards one or more messages by ID (CloneMode=1).
func (m *TaskManager) ForwardMessages(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, msgs []*tg.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if sourcePeer == nil {
		return errors.New("source peer is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
	}

	ids := make([]int, 0, len(msgs))
	rids := make([]int64, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.ID <= 0 {
			continue
		}
		rid, err := randomID()
		if err != nil {
			return err
		}
		ids = append(ids, msg.ID)
		rids = append(rids, rid)
	}
	if len(ids) == 0 {
		return nil
	}

	req := &tg.MessagesForwardMessagesRequest{
		FromPeer: sourcePeer,
		ToPeer:   peer,
		ID:       ids,
		RandomID: rids,
	}
	subject := sendSubjectAlbum(0, len(ids))
	if len(ids) == 1 {
		subject = sendSubjectMsgID(ids[0])
	}
	_, err := sendTelegramUpdatesWithRetry(ctx, "转发消息", subject, func(callCtx context.Context) (tg.UpdatesClass, error) {
		return api.MessagesForwardMessages(callCtx, req)
	})
	return err
}

func (m *TaskManager) ForwardMessagesWithFallback(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) error {
	if err := m.ForwardMessages(ctx, api, sourcePeer, peer, msgs); err != nil {
		if !isForwardOrCopyRestricted(err) {
			return err
		}

		msgID := 0
		for _, msg := range msgs {
			if msg != nil && msg.ID > 0 {
				msgID = msg.ID
				break
			}
		}
		logSmartFallback(task, msgID, err)

		mediaMsgs := make([]*tg.Message, 0, len(msgs))
		for _, msg := range msgs {
			if msg == nil {
				continue
			}
			if msg.Media == nil {
				if serr := m.SendText(ctx, api, msg, task, peer); serr != nil {
					return serr
				}
				continue
			}
			mediaMsgs = append(mediaMsgs, msg)
		}

		switch len(mediaMsgs) {
		case 0:
			return nil
		case 1:
			return m.SendUploadedMedia(ctx, api, sourcePeer, mediaMsgs[0], task, peer)
		default:
			return m.SendUploadedAlbum(ctx, api, sourcePeer, mediaMsgs, task, peer)
		}
	}

	return nil
}

func (m *TaskManager) SendMediaWithFallback(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) error {
	err := m.SendMedia(ctx, api, msg, task, peer)
	if err == nil {
		return nil
	}
	if !isForwardOrCopyRestricted(err) {
		return err
	}
	msgID := 0
	if msg != nil {
		msgID = msg.ID
	}
	logSmartFallback(task, msgID, err)
	return m.SendUploadedMedia(ctx, api, sourcePeer, msg, task, peer)
}

func (m *TaskManager) SendAlbumWithFallback(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) error {
	err := m.SendAlbum(ctx, api, msgs, task, peer)
	if err == nil {
		return nil
	}
	if !isForwardOrCopyRestricted(err) {
		return err
	}

	msgID := 0
	for _, msg := range msgs {
		if msg != nil && msg.ID > 0 {
			msgID = msg.ID
			break
		}
	}
	logSmartFallback(task, msgID, err)

	return m.SendUploadedAlbum(ctx, api, sourcePeer, msgs, task, peer)
}
