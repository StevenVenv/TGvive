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
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if global.DB == nil {
		return nil
	}
	if sourcePeer == nil || targetPeer == nil {
		return errors.New("tg peer is nil")
	}
	if sourceChannelMsgID <= 0 || targetChannelMsgID <= 0 {
		return nil
	}

	// Only channels have linked discussions.
	_, okSrc := sourcePeer.(*tg.InputPeerChannel)
	_, okDst := targetPeer.(*tg.InputPeerChannel)
	if !okSrc || !okDst {
		return nil
	}

	srcRes, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  sourcePeer,
		MsgID: sourceChannelMsgID,
	})
	if err != nil {
		return fmt.Errorf("get source discussion message: %w", err)
	}
	srcRoot := findDiscussionRootMsgID(srcRes, cfg.SourceLinkedChatID)
	if srcRoot <= 0 {
		return nil
	}

	dstRes, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  targetPeer,
		MsgID: targetChannelMsgID,
	})
	if err != nil {
		return fmt.Errorf("get target discussion message: %w", err)
	}
	dstRoot := findDiscussionRootMsgID(dstRes, cfg.TargetLinkedChatID)
	if dstRoot <= 0 {
		return nil
	}

	rec := model.MessageMapping{
		SourceChannelID: cfg.SourceChannelID,
		SourceMsgID:     srcRoot,
		TargetChannelID: cfg.TargetChannelID,
		TargetMsgID:     dstRoot,
	}

	return global.DB.
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
