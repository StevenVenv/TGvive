package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
)

func (m *TaskManager) processSingleMessageResultWithPublisher(
	ctx context.Context,
	crawlerAPI *tg.Client,
	sourcePeer tg.InputPeerClass,
	peer tg.InputPeerClass,
	task model.Task,
	msg *tg.Message,
	pub *taskPublisherRuntime,
) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	if pub == nil {
		return nil, errors.New("publisher is nil")
	}

	// Separated publish requires upload mode.
	if task.CloneMode != 3 {
		task.CloneMode = 3
	}

	// Apply text processors.
	msgToSend := msg
	if procs := m.processors(); procs.Text != nil && procs.Text.Enabled() {
		if out, changed := procs.Text.Process(msg.Message); changed {
			cp := *msg
			cp.Message = out
			cp.Entities = nil
			msgToSend = &cp
		}
	}

	if msgToSend.Media == nil {
		switch pub.Kind {
		case publisherKindMTProto:
			dstPeer := pub.Peer
			if dstPeer == nil {
				dstPeer = peer
			}
			if dstPeer == nil {
				return nil, errors.New("target peer is nil")
			}
			if pub.API == nil {
				return nil, errors.New("publisher tg api is nil")
			}
			return m.SendTextResult(ctx, pub.API, msgToSend, task, dstPeer)
		case publisherKindBot:
			id, err := m.botSendTextFromTGMessageResult(ctx, msgToSend, task, pub)
			if err != nil {
				return nil, err
			}
			if id > 0 {
				return []int{id}, nil
			}
			return nil, nil
		default:
			return nil, errors.New("unknown publisher kind")
		}
	}

	if _, err := convertMessageMediaToInput(msgToSend.Media); err != nil {
		if errors.Is(err, ErrUnsupportedMedia) {
			switch pub.Kind {
			case publisherKindMTProto:
				dstPeer := pub.Peer
				if dstPeer == nil {
					dstPeer = peer
				}
				if dstPeer == nil {
					return nil, errors.New("target peer is nil")
				}
				if pub.API == nil {
					return nil, errors.New("publisher tg api is nil")
				}
				return m.SendTextResult(ctx, pub.API, msgToSend, task, dstPeer)
			case publisherKindBot:
				id, err := m.botSendTextFromTGMessageResult(ctx, msgToSend, task, pub)
				if err != nil {
					return nil, err
				}
				if id > 0 {
					return []int{id}, nil
				}
				return nil, nil
			default:
				return nil, errors.New("unknown publisher kind")
			}
		}
		return nil, err
	}

	switch pub.Kind {
	case publisherKindMTProto:
		dstPeer := pub.Peer
		if dstPeer == nil {
			dstPeer = peer
		}
		if dstPeer == nil {
			return nil, errors.New("target peer is nil")
		}
		if pub.API == nil {
			return nil, errors.New("publisher tg api is nil")
		}

		upd, err := m.sendUploadedMediaUpdatesSeparated(ctx, crawlerAPI, pub.API, sourcePeer, msgToSend, task, dstPeer)
		if err != nil {
			return nil, err
		}
		return extractSentMsgIDs(upd), nil
	case publisherKindBot:
		replyID := 0
		if rt, ok := buildKeepReplyInput(ctx, task, msgToSend).(*tg.InputReplyToMessage); ok && rt != nil {
			replyID = rt.ReplyToMsgID
		}
		id, err := m.botSendUploadedMediaFromTGMessageResult(ctx, crawlerAPI, sourcePeer, msgToSend, task, pub, replyID)
		if err != nil {
			return nil, err
		}
		if id > 0 {
			return []int{id}, nil
		}
		return nil, nil
	default:
		return nil, errors.New("unknown publisher kind")
	}
}

func (m *TaskManager) processAlbumBatchResultWithPublisher(
	ctx context.Context,
	crawlerAPI *tg.Client,
	sourcePeer tg.InputPeerClass,
	peer tg.InputPeerClass,
	task model.Task,
	msgs []*tg.Message,
	pub *taskPublisherRuntime,
) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pub == nil {
		return nil, errors.New("publisher is nil")
	}

	// Separated publish requires upload mode.
	if task.CloneMode != 3 {
		task.CloneMode = 3
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
		return m.processSingleMessageResultWithPublisher(ctx, crawlerAPI, sourcePeer, peer, task, filtered[0], pub)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	// Caption/entities only on the first item.
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

	switch pub.Kind {
	case publisherKindMTProto:
		dstPeer := pub.Peer
		if dstPeer == nil {
			dstPeer = peer
		}
		if dstPeer == nil {
			return nil, errors.New("target peer is nil")
		}
		if pub.API == nil {
			return nil, errors.New("publisher tg api is nil")
		}

		upd, err := m.sendUploadedAlbumUpdatesSeparated(ctx, crawlerAPI, pub.API, sourcePeer, filtered, task, dstPeer)
		if err != nil {
			return nil, err
		}
		return extractSentMsgIDs(upd), nil
	case publisherKindBot:
		replyCarrier := filtered[0]
		for _, mm := range filtered {
			if extractReplyToSourceMsgID(mm) > 0 {
				replyCarrier = mm
				break
			}
		}
		replyID := 0
		if rt, ok := buildKeepReplyInput(ctx, task, replyCarrier).(*tg.InputReplyToMessage); ok && rt != nil {
			replyID = rt.ReplyToMsgID
		}

		ids, err := m.botSendUploadedAlbumFromTGMessagesResult(ctx, crawlerAPI, sourcePeer, filtered, task, pub, replyID)
		return ids, err
	default:
		return nil, fmt.Errorf("unknown publisher kind: %v", pub.Kind)
	}
}

