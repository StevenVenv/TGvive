package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"my-go-server/internal/model"

	"github.com/gotd/td/pool"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type TGDialogItem struct {
	Kind string `json:"kind"` // channel | supergroup | group

	Title    string `json:"title"`
	Username string `json:"username,omitempty"` // without "@"

	ChannelID int64 `json:"channel_id,omitempty"`
	ChatID    int64 `json:"chat_id,omitempty"`

	// BotChatID is the Bot API chat_id format:
	// - channel/supergroup: -100<channel_id>
	// - group: -<chat_id>
	BotChatID string `json:"bot_chat_id"`

	// TaskValue is the recommended value to paste into source_url/target_url.
	// It prefers @username when available, otherwise falls back to BotChatID.
	TaskValue string `json:"task_value"`
}

func ListAccountDialogs(ctx context.Context, sessionKey string, limit int) ([]TGDialogItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, errors.New("session key is required")
	}

	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	if Manager == nil {
		return nil, errors.New("task manager is nil")
	}
	tgRT, err := Manager.ensureTelegramForTask(ctx, model.Task{ExecuteBy: sessionKey})
	if err != nil {
		return nil, err
	}
	if tgRT == nil || tgRT.api == nil {
		return nil, errors.New("tg api is nil")
	}

	seen := make(map[string]struct{}, limit)
	out := make([]TGDialogItem, 0, minInt(400, limit))

	// Default dialogs + archived dialogs (folder_id=1).
	for _, folderID := range []int{0, 1} {
		need := limit - len(out)
		if need <= 0 {
			break
		}

		items, err := collectDialogsFromFolder(ctx, tgRT.api, folderID, need)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			key := it.Kind + ":" + it.BotChatID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, it)
			if len(out) >= limit {
				break
			}
		}
	}

	return out, nil
}

func collectDialogsFromFolder(ctx context.Context, api *tg.Client, folderID int, limit int) ([]TGDialogItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if limit <= 0 {
		return nil, nil
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

	out := make([]TGDialogItem, 0, minInt(100, limit))
	for iter.Next(ctx) {
		elem := iter.Value()
		item, ok := dialogElemToItem(elem)
		if !ok {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func dialogElemToItem(elem dialogs.Elem) (TGDialogItem, bool) {
	switch p := elem.Peer.(type) {
	case *tg.InputPeerChannel:
		if p == nil || p.ChannelID == 0 {
			return TGDialogItem{}, false
		}

		title := ""
		username := ""
		kind := "channel"

		if ch, ok := elem.Entities.Channel(p.ChannelID); ok && ch != nil {
			title = strings.TrimSpace(ch.Title)
			username = strings.TrimSpace(ch.Username)
			if ch.Megagroup {
				kind = "supergroup"
			} else if ch.Broadcast {
				kind = "channel"
			}
		}

		if title == "" {
			title = fmt.Sprintf("channel %d", p.ChannelID)
		}

		idStr := strconv.FormatInt(p.ChannelID, 10)
		botChatID := "-100" + idStr

		taskValue := botChatID
		if username != "" {
			taskValue = "@" + username
		}

		return TGDialogItem{
			Kind:      kind,
			Title:     title,
			Username:  username,
			ChannelID: p.ChannelID,
			BotChatID: botChatID,
			TaskValue: taskValue,
		}, true

	case *tg.InputPeerChat:
		if p == nil || p.ChatID == 0 {
			return TGDialogItem{}, false
		}

		title := ""
		if ch, ok := elem.Entities.Chat(p.ChatID); ok && ch != nil {
			title = strings.TrimSpace(ch.Title)
		}
		if title == "" {
			title = fmt.Sprintf("group %d", p.ChatID)
		}

		idStr := strconv.FormatInt(p.ChatID, 10)
		botChatID := "-" + idStr

		return TGDialogItem{
			Kind:      "group",
			Title:     title,
			ChatID:    p.ChatID,
			BotChatID: botChatID,
			TaskValue: botChatID,
		}, true

	default:
		return TGDialogItem{}, false
	}
}
