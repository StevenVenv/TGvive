package engine

import (
	"context"
	"errors"
	"strings"

	"my-go-server/internal/engine/localdb"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func (m *TaskManager) sendLightPayloadAsComment(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, payload localdb.LightPayload, replyToRootID int) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if cfg == nil || cfg.TargetLinkedPeer == nil {
		return nil, errors.New("target peer is nil")
	}
	if replyToRootID <= 0 {
		return nil, errors.New("reply_to_root_id is required")
	}

	replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToRootID}

	// If media reference exists, try sendMedia first.
	if len(payload.MediaBytes) > 0 {
		im, err := tg.DecodeInputMedia(&bin.Buffer{Buf: payload.MediaBytes})
		if err == nil && im != nil {
			rid, err := randomID()
			if err != nil {
				return nil, err
			}
			caption := payload.Text
			if out, truncated := sanitizeMediaCaptionText(caption); truncated {
				caption = out
			}
			upd, err := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
				Peer:     cfg.TargetLinkedPeer,
				ReplyTo:  replyTo,
				Media:    im,
				Message:  caption,
				RandomID: rid,
			})
			return extractSentMsgIDs(upd), err
		}
	}

	// Text-only.
	if strings.TrimSpace(payload.Text) == "" {
		return nil, nil
	}
	rid, err := randomID()
	if err != nil {
		return nil, err
	}
	upd, err := api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     cfg.TargetLinkedPeer,
		ReplyTo:  replyTo,
		Message:  payload.Text,
		RandomID: rid,
	})
	return extractSentMsgIDs(upd), err
}

func (m *TaskManager) sendLightPayloadAlbumAsComment(ctx context.Context, api *tg.Client, cfg *commentPipelineConfig, payloads []localdb.LightPayload, replyToRootID int) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if cfg == nil || cfg.TargetLinkedPeer == nil {
		return nil, errors.New("target peer is nil")
	}
	if replyToRootID <= 0 {
		return nil, errors.New("reply_to_root_id is required")
	}

	if len(payloads) == 0 {
		return nil, nil
	}
	if len(payloads) == 1 {
		return m.sendLightPayloadAsComment(ctx, api, cfg, payloads[0], replyToRootID)
	}

	replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToRootID}

	multi := make([]tg.InputSingleMedia, 0, len(payloads))
	for i, p := range payloads {
		if len(p.MediaBytes) == 0 {
			return nil, errors.New("album item missing media_bytes")
		}
		im, err := tg.DecodeInputMedia(&bin.Buffer{Buf: p.MediaBytes})
		if err != nil || im == nil {
			if err == nil {
				err = errors.New("decode input media returned nil")
			}
			return nil, err
		}
		rid, err := randomID()
		if err != nil {
			return nil, err
		}

		item := tg.InputSingleMedia{
			Media:    im,
			RandomID: rid,
		}
		if i == 0 {
			caption := p.Text
			if out, truncated := sanitizeMediaCaptionText(caption); truncated {
				caption = out
			}
			item.Message = caption
		}

		multi = append(multi, item)
	}

	upd, err := api.MessagesSendMultiMedia(ctx, &tg.MessagesSendMultiMediaRequest{
		Peer:       cfg.TargetLinkedPeer,
		ReplyTo:    replyTo,
		MultiMedia: multi,
	})
	return extractSentMsgIDs(upd), err
}
