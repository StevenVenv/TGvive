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
func (m *TaskManager) ProcessMessage(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, task model.Task, msg *tg.Message) error {
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

	strategy := ResolveRuntimeStrategy(task)
	hotTask := MergeHotFieldsIntoTask(task, strategy)
	allowedTypes, _ := ResolveAllowedTypes(task, strategy)
	allowFileSuffixes, blockFileSuffixes, _ := ResolveFileSuffixRules(task, strategy)

	if msg.GroupedID != 0 && msg.Media != nil && m.grouper != nil && task.ID != 0 {
		taskID := task.ID
		groupedID := msg.GroupedID
		m.grouper.Add(taskID, groupedID, msg, func(batch []*tg.Message) {
			plan := PlanMediaGroup(m, batch, allowedTypes, allowFileSuffixes, blockFileSuffixes)
			if plan.Skipped > 0 {
				global.AddFiltered(uint64(plan.Skipped))
			}
			if plan.Need == 0 {
				return
			}
			if plan.Text != nil {
				if err := m.processSingleMessage(ctx, api, sourcePeer, peer, hotTask, plan.Text); err != nil && global.Logger != nil {
					global.Logger.Error(
						"process album downgraded text failed",
						zap.Uint("task_id", taskID),
						zap.Int64("grouped_id", groupedID),
						zap.Error(err),
					)
				}
				return
			}

			if err := m.processAlbumBatch(ctx, api, sourcePeer, peer, hotTask, plan.Media, nil); err != nil && global.Logger != nil {
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

	contentType := m.DetectContentType(msg)
	if allowedTypes != nil {
		if _, ok := allowedTypes[contentType]; !ok {
			global.IncFiltered()
			return nil
		}
	}
	if contentType == "file" && !fileSuffixAllowed(msg, allowFileSuffixes, blockFileSuffixes) {
		global.IncFiltered()
		return nil
	}

	return m.processSingleMessage(ctx, api, sourcePeer, peer, hotTask, msg)
}

func (m *TaskManager) processSingleMessage(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, task model.Task, msg *tg.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if msg == nil {
		return nil
	}

	if task.CloneMode != 3 {
		task.EnableMediaEdit = false
	}

	if task.CloneMode == 1 {
		return m.ForwardMessagesWithFallback(ctx, api, sourcePeer, []*tg.Message{msg}, task, peer)
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
		return m.SendText(ctx, api, peer, msgToSend)
	}

	if _, err := convertMessageMediaToInput(msgToSend.Media); err != nil {
		if errors.Is(err, ErrUnsupportedMedia) {
			return m.SendText(ctx, api, peer, msgToSend)
		}
		return err
	}

	switch task.CloneMode {
	case 2:
		return m.SendMediaWithFallback(ctx, api, sourcePeer, msgToSend, task, peer)
	case 3:
		return m.SendUploadedMedia(ctx, api, sourcePeer, msgToSend, task, peer)
	default:
		return ErrUnsupportedCloneMode
	}
}

func (m *TaskManager) processAlbumBatch(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, task model.Task, msgs []*tg.Message, allowedTypes map[string]struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = allowedTypes
	if task.CloneMode != 3 {
		task.EnableMediaEdit = false
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
		return nil
	case 1:
		return m.processSingleMessage(ctx, api, sourcePeer, peer, task, filtered[0])
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
		return m.ForwardMessagesWithFallback(ctx, api, sourcePeer, filtered, task, peer)
	case 2:
		return m.SendAlbumWithFallback(ctx, api, sourcePeer, filtered, task, peer)
	case 3:
		return m.SendUploadedAlbum(ctx, api, sourcePeer, filtered, task, peer)
	default:
		return ErrUnsupportedCloneMode
	}
}
