package engine

import (
	"context"
	"errors"
	"fmt"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"gorm.io/gorm/clause"
)

func findDiscussionRootMsgID(res *tg.MessagesDiscussionMessage, linkedChatID int64) int {
	if res == nil || linkedChatID == 0 {
		return 0
	}
	for _, mc := range res.Messages {
		msg, ok := mc.(*tg.Message)
		if !ok || msg == nil || msg.ID <= 0 {
			continue
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok && chID == linkedChatID {
			return msg.ID
		}
	}
	return 0
}

func (m *TaskManager) writeMessageMapping(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, sourcePeer tg.InputPeerClass, targetPeer tg.InputPeerClass, sourceChannelMsgID int, targetChannelMsgID int) error {
	_, _, err := m.writeMessageMappingWithRoots(ctx, api, cfg, sourcePeer, targetPeer, sourceChannelMsgID, targetChannelMsgID)
	return err
}

func (m *TaskManager) writeMessageMappingWithRoots(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, sourcePeer tg.InputPeerClass, targetPeer tg.InputPeerClass, sourceChannelMsgID int, targetChannelMsgID int) (int, int, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if cfg == nil || !cfg.Enabled {
		return 0, 0, nil
	}
	if api == nil {
		return 0, 0, errors.New("tg api is nil")
	}
	if sourcePeer == nil || targetPeer == nil {
		return 0, 0, errors.New("tg peer is nil")
	}
	if sourceChannelMsgID <= 0 || targetChannelMsgID <= 0 {
		return 0, 0, nil
	}

	// Only channels have linked discussions.
	_, okSrc := sourcePeer.(*tg.InputPeerChannel)
	_, okDst := targetPeer.(*tg.InputPeerChannel)
	if !okSrc || !okDst {
		return 0, 0, nil
	}

	srcRes, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  sourcePeer,
		MsgID: sourceChannelMsgID,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("get source discussion message: %w", err)
	}
	srcRoot := findDiscussionRootMsgID(srcRes, cfg.SourceLinkedChatID)
	if srcRoot <= 0 {
		return 0, 0, nil
	}

	dstRes, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  targetPeer,
		MsgID: targetChannelMsgID,
	})
	if err != nil {
		return srcRoot, 0, fmt.Errorf("get target discussion message: %w", err)
	}
	dstRoot := findDiscussionRootMsgID(dstRes, cfg.TargetLinkedChatID)
	if dstRoot <= 0 {
		return srcRoot, 0, nil
	}

	rec := model.MessageMapping{
		SourceChannelID: cfg.SourceChannelID,
		SourceMsgID:     srcRoot,
		TargetChannelID: cfg.TargetChannelID,
		TargetMsgID:     dstRoot,
	}

	if global.DB == nil {
		return srcRoot, dstRoot, nil
	}

	return srcRoot, dstRoot, global.DB.
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "source_channel_id"},
				{Name: "source_msg_id"},
				{Name: "target_channel_id"},
			},
			DoNothing: true,
		}).
		Create(&rec).Error
}
