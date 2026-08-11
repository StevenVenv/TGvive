package engine

import (
	"context"
	"errors"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func getDiscussionRootIDWithRetry(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, channelMsgID int, linkedChatID int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if api == nil {
		return 0, errors.New("tg api is nil")
	}
	if peer == nil {
		return 0, errors.New("tg peer is nil")
	}
	if channelMsgID <= 0 || linkedChatID == 0 {
		return 0, nil
	}

	const maxAttempts = 6
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		res, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
			Peer:  peer,
			MsgID: channelMsgID,
		})
		if err != nil {
			// FloodWait: wait then retry.
			if ok, _ := tgerr.FloodWait(ctx, err); ok {
				continue
			}
			// Sometimes Telegram returns MSG_ID_INVALID for newly-created discussion roots.
			// Retry a few times with jitter before giving up.
			if isTGMsgIDInvalid(err) {
				if attempt < maxAttempts-1 {
					sleepRandom(ctx, 250*time.Millisecond, 900*time.Millisecond)
					continue
				}
				return 0, nil
			}
			return 0, err
		}

		rootID := findDiscussionRootMsgID(res, linkedChatID)
		if rootID > 0 {
			return rootID, nil
		}

		// Root not ready yet (commonly happens right after sending target trunk).
		if attempt < maxAttempts-1 {
			sleepRandom(ctx, 200*time.Millisecond, 600*time.Millisecond)
		}
	}

	return 0, nil
}

func getDiscussionRootMetaWithRetry(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, channelMsgID int, linkedChatID int64) (rootID int, hasReplies bool, err error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if api == nil {
		return 0, false, errors.New("tg api is nil")
	}
	if peer == nil {
		return 0, false, errors.New("tg peer is nil")
	}
	if channelMsgID <= 0 || linkedChatID == 0 {
		return 0, false, nil
	}

	const maxAttempts = 6
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}

		res, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
			Peer:  peer,
			MsgID: channelMsgID,
		})
		if err != nil {
			// FloodWait: wait then retry.
			if ok, _ := tgerr.FloodWait(ctx, err); ok {
				continue
			}
			// Sometimes Telegram returns MSG_ID_INVALID for newly-created discussion roots.
			// Retry a few times with jitter before giving up.
			if isTGMsgIDInvalid(err) {
				if attempt < maxAttempts-1 {
					sleepRandom(ctx, 250*time.Millisecond, 900*time.Millisecond)
					continue
				}
				return 0, false, nil
			}
			return 0, false, err
		}

		rootID = findDiscussionRootMsgID(res, linkedChatID)
		if rootID > 0 {
			if maxID, ok := res.GetMaxID(); ok && maxID > rootID {
				hasReplies = true
			}
			return rootID, hasReplies, nil
		}

		// Root not ready yet (commonly happens right after sending target trunk).
		if attempt < maxAttempts-1 {
			sleepRandom(ctx, 200*time.Millisecond, 600*time.Millisecond)
		}
	}

	return 0, false, nil
}
