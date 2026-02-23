package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
)

// SetupTaskPeers resolves SourceURL/TargetURL into InputPeer and ensures SourceChannelID is filled.
// - sourceAPI is used to resolve source peer (crawler).
// - targetAPI is used to resolve target peer (publisher, optional; when nil uses sourceAPI).
// - when task.PublishType == "bot", target peer resolution is skipped.
func (m *TaskManager) SetupTaskPeers(ctx context.Context, sourceAPI *tg.Client, targetAPI *tg.Client, task *model.Task) (sourcePeer tg.InputPeerClass, targetPeer tg.InputPeerClass, sourceChannelID int64, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, 0, err
	}
	if sourceAPI == nil {
		return nil, nil, 0, errors.New("tg api is nil")
	}
	if task == nil || task.ID == 0 {
		return nil, nil, 0, errors.New("task is nil or id is empty")
	}

	sourcePeer, err = resolveInputPeer(ctx, sourceAPI, task.SourceURL)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("resolve source peer %q: %w", task.SourceURL, err)
	}

	if strings.TrimSpace(task.PublishType) != "bot" {
		if targetAPI == nil {
			targetAPI = sourceAPI
		}
		targetPeer, err = resolveInputPeer(ctx, targetAPI, task.TargetURL)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("resolve target peer %q: %w", task.TargetURL, err)
		}
	}

	chID, ok := inputPeerToChannelID(sourcePeer)
	if !ok || chID == 0 {
		return nil, nil, 0, fmt.Errorf("source peer is not a channel: %T", sourcePeer)
	}

	sourceChannelID = chID
	if task.SourceChannelID != chID {
		task.SourceChannelID = chID
		if global.DB != nil {
			if err := global.DB.WithContext(ctx).Model(&model.Task{}).
				Where("id = ?", task.ID).
				Update("source_channel_id", chID).Error; err != nil {
				return nil, nil, 0, fmt.Errorf("persist source_channel_id: %w", err)
			}
		}
	}

	return sourcePeer, targetPeer, sourceChannelID, nil
}

func inputPeerToChannelID(peer tg.InputPeerClass) (int64, bool) {
	switch v := peer.(type) {
	case *tg.InputPeerChannel:
		return v.ChannelID, true
	default:
		return 0, false
	}
}
