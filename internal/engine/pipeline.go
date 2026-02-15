package engine

import (
	"context"
	"errors"
	"sort"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

var ErrUnsupportedCloneMode = errors.New("unsupported clone mode")

// ProcessMessage is the engine pipeline entry for a single incoming Telegram message.
// It aggregates album messages (same GroupedID) to avoid album fragmentation.
func (m *TaskManager) ProcessMessage(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, task model.Task, msg *tg.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || msg == nil {
		return nil
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
	}

	allowedTypes := normalizeTypeSet(task.ContentTypes.Strings())
	contentType := m.DetectContentType(msg)
	if allowedTypes != nil {
		if _, ok := allowedTypes[contentType]; !ok {
			return nil
		}
	}

	if msg.GroupedID != 0 && msg.Media != nil && m.grouper != nil && task.ID != 0 {
		taskID := task.ID
		groupedID := msg.GroupedID
		m.grouper.Add(taskID, groupedID, msg, func(batch []*tg.Message) {
			if err := m.processAlbumBatch(ctx, api, peer, task, batch, allowedTypes); err != nil && global.Logger != nil {
				global.Logger.Error(
					"process album batch failed",
					zap.Uint("task_id", taskID),
					zap.Int64("grouped_id", groupedID),
					zap.Error(err),
				)
			}
		})
		return nil
	}

	return m.processSingleMessage(ctx, api, peer, task, msg)
}

func (m *TaskManager) processSingleMessage(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, task model.Task, msg *tg.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if msg == nil {
		return nil
	}

	if msg.Media == nil {
		return m.SendText(ctx, api, peer, msg)
	}

	switch task.CloneMode {
	case 2:
		return m.SendMedia(ctx, api, msg, task, peer)
	case 3:
		if m == nil || m.tg == nil || m.tg.client == nil {
			return errors.New("telegram client is nil")
		}

		inputFile, err := m.TransferMedia(ctx, m.tg.client, msg)
		if err != nil {
			return err
		}
		inputMedia := m.WrapUploadedMedia(inputFile, msg)
		if inputMedia == nil {
			return ErrUnsupportedMedia
		}

		rid, err := randomID()
		if err != nil {
			return err
		}

		req := &tg.MessagesSendMediaRequest{
			Peer:     peer,
			Media:    inputMedia,
			Message:  msg.Message,
			RandomID: rid,
		}
		if len(msg.Entities) > 0 {
			req.Entities = msg.Entities
		}

		_, err = api.MessagesSendMedia(ctx, req)
		return err
	default:
		return ErrUnsupportedCloneMode
	}
}

func (m *TaskManager) processAlbumBatch(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, task model.Task, msgs []*tg.Message, allowedTypes map[string]struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Filter nil/unsupported/filtered-out messages.
	filtered := make([]*tg.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.Media == nil {
			continue
		}
		if allowedTypes != nil {
			ct := m.DetectContentType(msg)
			if _, ok := allowedTypes[ct]; !ok {
				continue
			}
		}
		filtered = append(filtered, msg)
	}

	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return m.processSingleMessage(ctx, api, peer, task, filtered[0])
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	switch task.CloneMode {
	case 2:
		return m.SendAlbum(ctx, api, filtered, task, peer)
	case 3:
		return m.SendUploadedAlbum(ctx, api, filtered, task, peer)
	default:
		return ErrUnsupportedCloneMode
	}
}
