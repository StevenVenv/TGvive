package engine

import (
	"context"
	"errors"
	"fmt"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
)

// SetupTaskPeers resolves SourceURL/TargetURL into InputPeer and ensures SourceChannelID is filled.
func (m *TaskManager) SetupTaskPeers(ctx context.Context, api *tg.Client, task *model.Task) (sourcePeer tg.InputPeerClass, targetPeer tg.InputPeerClass, sourceChannelID int64, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, 0, err
	}
	if api == nil {
		return nil, nil, 0, errors.New("tg api is nil")
	}
	if task == nil || task.ID == 0 {
		return nil, nil, 0, errors.New("task is nil or id is empty")
	}

	sourcePeer, err = resolveInputPeer(ctx, api, task.SourceURL)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("resolve source peer %q: %w", task.SourceURL, err)
	}

	targetPeer, err = resolveInputPeer(ctx, api, task.TargetURL)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("resolve target peer %q: %w", task.TargetURL, err)
	}

	chID, ok := inputPeerToChannelID(sourcePeer)
	if !ok || chID == 0 {
		return nil, nil, 0, fmt.Errorf("source peer is not a channel: %T", sourcePeer)
	}

	sourceChannelID = chID
	if task.SourceChannelID != chID {
		task.SourceChannelID = chID
		if global.DB != nil {
			if err := global.DB.Model(&model.Task{}).
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

