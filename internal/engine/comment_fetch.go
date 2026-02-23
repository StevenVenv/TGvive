package engine

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const (
	commentFetchPageSize = 200
	commentFetchMaxTotal = 200

	commentFetchMsgIDInvalidMaxRetries = 4
	commentFetchMsgIDInvalidDelayMin   = 250 * time.Millisecond
	commentFetchMsgIDInvalidDelayMax   = 900 * time.Millisecond
)

func fetchRepliesByRoot(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, rootMsgID int, limit int, maxTotal int) ([]*tg.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}
	if rootMsgID <= 0 {
		return nil, nil
	}

	if limit <= 0 {
		limit = commentFetchPageSize
	}
	if maxTotal <= 0 {
		maxTotal = commentFetchMaxTotal
	}
	if limit > maxTotal {
		limit = maxTotal
	}

	seen := make(map[int]struct{}, maxTotal)
	out := make([]*tg.Message, 0, maxTotal)

	didFallbackLimit := false
	offsetID := 0
	invalidRetries := 0
	invalidRetriesOffsetID := -1
	for len(out) < maxTotal {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		need := maxTotal - len(out)
		pageLimit := limit
		if need < pageLimit {
			pageLimit = need
		}

		r, err := api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{
			Peer:     peer,
			MsgID:    rootMsgID,
			OffsetID: offsetID,
			Limit:    pageLimit,
		})
		if err != nil {
			// FloodWait: wait and retry.
			if ok, _ := tgerr.FloodWait(ctx, err); ok {
				continue
			}
			// Some Telegram backends reject large limits; fallback to a smaller page size.
			if !didFallbackLimit && offsetID == 0 && limit > 50 && tgerr.Is(err, "LIMIT_INVALID") {
				limit = 50
				didFallbackLimit = true
				continue
			}
			// Occasionally Telegram returns MSG_ID_INVALID for discussion roots that are not fully ready yet.
			// Retry a few times with jitter before giving up.
			if tgerr.Is(err, "MSG_ID_INVALID") {
				if invalidRetriesOffsetID != offsetID {
					invalidRetriesOffsetID = offsetID
					invalidRetries = 0
				}
				if invalidRetries < commentFetchMsgIDInvalidMaxRetries {
					invalidRetries++
					sleepRandom(ctx, commentFetchMsgIDInvalidDelayMin, commentFetchMsgIDInvalidDelayMax)
					continue
				}
			}
			return nil, err
		}
		invalidRetries = 0
		invalidRetriesOffsetID = -1

		msgs := extractTGMessages(r)
		if len(msgs) == 0 {
			break
		}

		pageMinID := 0
		added := 0
		for _, msg := range msgs {
			if msg == nil || msg.ID <= 0 {
				continue
			}
			if msg.ID == rootMsgID {
				continue
			}
			if pageMinID == 0 || msg.ID < pageMinID {
				pageMinID = msg.ID
			}
			if _, ok := seen[msg.ID]; ok {
				continue
			}
			seen[msg.ID] = struct{}{}
			out = append(out, msg)
			added++
			if len(out) >= maxTotal {
				break
			}
		}

		// Stop when we can't advance anymore.
		if pageMinID <= 0 {
			break
		}
		if offsetID > 0 && pageMinID >= offsetID {
			break
		}
		offsetID = pageMinID

		// Some threads are small; avoid spinning if the API returns overlapping pages.
		if added == 0 {
			break
		}
		if len(msgs) < pageLimit {
			break
		}
	}

	if len(out) == 0 {
		return nil, nil
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// FetchComments fetches replies for a channel post using discussion root in its linked chat.
// It returns at most commentFetchMaxTotal comments, sorted from old to new.
func (cfg *commentPipelineConfig) FetchComments(ctx context.Context, api *tg.Client, sourceChannelPeer tg.InputPeerClass, sourceChannelMsgID int) ([]*tg.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if sourceChannelPeer == nil {
		return nil, errors.New("source peer is nil")
	}
	if sourceChannelMsgID <= 0 {
		return nil, nil
	}
	if cfg.SourceLinkedChatID == 0 || cfg.SourceLinkedPeer == nil {
		return nil, nil
	}

	rootID, hasReplies, err := getDiscussionRootMetaWithRetry(ctx, api, sourceChannelPeer, sourceChannelMsgID, cfg.SourceLinkedChatID)
	if err != nil {
		return nil, err
	}
	if rootID <= 0 {
		return nil, nil
	}
	if !hasReplies {
		return nil, nil
	}

	return fetchRepliesByRoot(ctx, api, cfg.SourceLinkedPeer, rootID, commentFetchPageSize, commentFetchMaxTotal)
}
