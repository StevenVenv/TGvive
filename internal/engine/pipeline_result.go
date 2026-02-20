package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
)

func (m *TaskManager) processSingleMessageResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, task model.Task, msg *tg.Message) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	if msg.ID > 0 {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("收到消息: msg_id=%d", msg.ID))
	}

	if task.CloneMode != 3 {
		task.EnableMediaEdit = false
	}

	if task.CloneMode == 1 {
		if !task.KeepReply {
			return m.ForwardMessagesWithFallbackResult(ctx, api, sourcePeer, []*tg.Message{msg}, task, peer, nil)
		}

		replyTo := tg.InputReplyToClass(nil)
		if srcReplyID := extractReplyToSourceMsgID(msg); srcReplyID > 0 {
			replyTo = buildKeepReplyInput(task, msg)
			if replyTo == nil {
				recordTaskDetailFromCtx(ctx, fmt.Sprintf("保留回复: 映射缺失 reply_to=%d (msg_id=%d)", srcReplyID, msg.ID))
			}
		}

		ids, err := m.ForwardMessagesWithFallbackResult(ctx, api, sourcePeer, []*tg.Message{msg}, task, peer, replyTo)
		if err != nil {
			return nil, err
		}
		if msg.ID > 0 {
			if targetID := minPositiveInt(ids); targetID > 0 {
				storeMsgMapping(task, msg.ID, targetID)
			}
		}
		return ids, nil
	}

	msgToSend := msg
	if procs := m.processors(); procs.Text != nil && procs.Text.Enabled() {
		if out, changed := procs.Text.Process(msg.Message); changed {
			cp := *msg
			cp.Message = out
			cp.Entities = nil
			msgToSend = &cp
		}
	}

	if msg.Media == nil {
		return m.SendTextResult(ctx, api, msgToSend, task, peer)
	}

	if _, err := convertMessageMediaToInput(msgToSend.Media); err != nil {
		if errors.Is(err, ErrUnsupportedMedia) {
			return m.SendTextResult(ctx, api, msgToSend, task, peer)
		}
		return nil, err
	}

	switch task.CloneMode {
	case 2:
		return m.SendMediaWithFallbackResult(ctx, api, sourcePeer, msgToSend, task, peer)
	case 3:
		return m.SendUploadedMediaResult(ctx, api, sourcePeer, msgToSend, task, peer)
	default:
		return nil, ErrUnsupportedCloneMode
	}
}

func (m *TaskManager) processAlbumBatchResult(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, task model.Task, msgs []*tg.Message) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if task.CloneMode != 3 {
		task.EnableMediaEdit = false
	}

	if len(msgs) > 0 && msgs[0] != nil && msgs[0].GroupedID != 0 {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("收到专辑: grouped_id=%d items=%d", msgs[0].GroupedID, len(msgs)))
	}

	// Filter nil/unsupported messages.
	filtered := make([]*tg.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.Media == nil {
			continue
		}
		if _, err := convertMessageMediaToInput(msg.Media); err != nil {
			continue
		}
		filtered = append(filtered, msg)
	}

	switch len(filtered) {
	case 0:
		return nil, nil
	case 1:
		return m.processSingleMessageResult(ctx, api, sourcePeer, peer, task, filtered[0])
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	// Caption/entities only on the first item.
	if task.CloneMode != 1 {
		if procs := m.processors(); procs.Text != nil && procs.Text.Enabled() && len(filtered) > 0 {
			first := filtered[0]
			if first != nil {
				if out, changed := procs.Text.Process(first.Message); changed {
					cp := *first
					cp.Message = out
					cp.Entities = nil
					copied := make([]*tg.Message, len(filtered))
					copy(copied, filtered)
					copied[0] = &cp
					filtered = copied
				}
			}
		}
	}

	switch task.CloneMode {
	case 1:
		if !task.KeepReply {
			return m.ForwardMessagesWithFallbackResult(ctx, api, sourcePeer, filtered, task, peer, nil)
		}

		replyCarrier := filtered[0]
		srcReplyID := 0
		for _, fm := range filtered {
			if id := extractReplyToSourceMsgID(fm); id > 0 {
				replyCarrier = fm
				srcReplyID = id
				break
			}
		}
		replyTo := buildKeepReplyInput(task, replyCarrier)
		if srcReplyID > 0 && replyTo == nil {
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("保留回复: 映射缺失 reply_to=%d (grouped_id=%d)", srcReplyID, replyCarrier.GroupedID))
		}

		ids, err := m.ForwardMessagesWithFallbackResult(ctx, api, sourcePeer, filtered, task, peer, replyTo)
		if err != nil {
			return nil, err
		}
		storeMsgMappingsInOrder(task, filtered, ids)
		return ids, nil
	case 2:
		return m.SendAlbumWithFallbackResult(ctx, api, sourcePeer, filtered, task, peer)
	case 3:
		return m.SendUploadedAlbumResult(ctx, api, sourcePeer, filtered, task, peer)
	default:
		return nil, ErrUnsupportedCloneMode
	}
}
