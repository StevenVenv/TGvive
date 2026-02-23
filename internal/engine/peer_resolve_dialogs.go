package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gotd/td/pool"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func resolveChannelPeerByID(ctx context.Context, api *tg.Client, channelID int64) (*tg.InputPeerChannel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if channelID <= 0 {
		return nil, errors.New("channel_id is required")
	}

	// Search default dialogs and archived folder (folder_id=1).
	for _, folderID := range []int{0, 1} {
		peer, err := findChannelPeerInDialogs(ctx, api, channelID, folderID)
		if err != nil {
			return nil, err
		}
		if peer != nil {
			return peer, nil
		}
	}

	return nil, fmt.Errorf("channel not found in dialogs (channel_id=%d): join it with this account first", channelID)
}

func findChannelPeerInDialogs(ctx context.Context, api *tg.Client, channelID int64, folderID int) (*tg.InputPeerChannel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if channelID <= 0 {
		return nil, errors.New("channel_id is required")
	}

	q := dialogs.QueryFunc(func(ctx context.Context, req dialogs.Request) (tg.MessagesDialogsClass, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		r := &tg.MessagesGetDialogsRequest{
			OffsetDate: req.OffsetDate,
			OffsetID:   req.OffsetID,
			OffsetPeer: req.OffsetPeer,
			Limit:      req.Limit,
			Hash:       0,
		}
		if folderID != 0 {
			r.SetFolderID(folderID)
		}

		for attempt := 0; attempt < 5; attempt++ {
			out, err := api.MessagesGetDialogs(ctx, r)
			if err == nil {
				return out, nil
			}
			if ok, _ := tgerr.FloodWait(ctx, err); ok {
				continue
			}
			if errors.Is(err, pool.ErrConnDead) {
				sleepRandom(ctx, 250*time.Millisecond, 900*time.Millisecond)
				continue
			}
			return nil, err
		}
		return nil, errors.New("messages.getDialogs retries exceeded")
	})

	iter := dialogs.NewIterator(q, 100)
	for iter.Next(ctx) {
		elem := iter.Value()
		ch, ok := elem.Peer.(*tg.InputPeerChannel)
		if !ok || ch == nil || ch.ChannelID != channelID {
			continue
		}
		if ch.AccessHash == 0 {
			return nil, fmt.Errorf("channel access hash missing (channel_id=%d)", channelID)
		}
		return ch, nil
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

