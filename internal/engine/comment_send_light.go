package engine

import (
	"context"
	"errors"
	"strings"

	"my-go-server/internal/engine/localdb"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func (m *TaskManager) sendLightPayloadAsComment(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, payload localdb.LightPayload, replyToRootID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if cfg == nil || cfg.TargetLinkedPeer == nil {
		return errors.New("target peer is nil")
	}
	if replyToRootID <= 0 {
		return errors.New("reply_to_root_id is required")
	}

	replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToRootID}

	// If media reference exists, try sendMedia first.
	if len(payload.MediaBytes) > 0 {
		im, err := tg.DecodeInputMedia(&bin.Buffer{Buf: payload.MediaBytes})
		if err == nil && im != nil {
			rid, err := randomID()
			if err != nil {
				return err
			}
			_, err = api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
				Peer:     cfg.TargetLinkedPeer,
				ReplyTo:  replyTo,
				Media:    im,
				Message:  payload.Text,
				RandomID: rid,
			})
			return err
		}
	}

	// Text-only.
	if strings.TrimSpace(payload.Text) == "" {
		return nil
	}
	rid, err := randomID()
	if err != nil {
		return err
	}
	_, err = api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     cfg.TargetLinkedPeer,
		ReplyTo:  replyTo,
		Message:  payload.Text,
		RandomID: rid,
	})
	return err
}

