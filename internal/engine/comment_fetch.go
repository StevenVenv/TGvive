package engine

import (
	"context"
	"errors"
	"sort"

	"github.com/gotd/td/tg"
)

const (
	commentFetchPageSize = 50
	commentFetchMaxTotal = 200
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

	offsetID := 0
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
			return nil, err
		}

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

	res, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  sourceChannelPeer,
		MsgID: sourceChannelMsgID,
	})
	if err != nil {
		return nil, err
	}
	rootID := findDiscussionRootMsgID(res, cfg.SourceLinkedChatID)
	if rootID <= 0 {
		return nil, nil
	}

	return fetchRepliesByRoot(ctx, api, cfg.SourceLinkedPeer, rootID, commentFetchPageSize, commentFetchMaxTotal)
}
