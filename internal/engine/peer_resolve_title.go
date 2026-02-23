package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// ResolveAccountPeer resolves a user-provided peer reference (e.g. @username, -100..., -123)
// to a displayable dialog item (kind/title/username + ids) using the given account session.
//
// It is best-effort: if the account has not joined a private channel/supergroup, resolving
// may fail due to missing access hash.
func ResolveAccountPeer(ctx context.Context, sessionKey string, raw string) (TGDialogItem, error) {
	if err := ctx.Err(); err != nil {
		return TGDialogItem{}, err
	}
	sessionKey = strings.TrimSpace(sessionKey)
	raw = strings.TrimSpace(raw)
	if sessionKey == "" {
		return TGDialogItem{}, errors.New("session key is required")
	}
	if raw == "" {
		return TGDialogItem{}, errors.New("peer is required")
	}

	if Manager == nil {
		return TGDialogItem{}, errors.New("task manager is nil")
	}
	tgRT, err := Manager.ensureTelegramForTask(ctx, model.Task{ExecuteBy: sessionKey})
	if err != nil {
		return TGDialogItem{}, err
	}
	if tgRT == nil || tgRT.api == nil {
		return TGDialogItem{}, errors.New("tg api is nil")
	}

	peer, err := resolveInputPeer(ctx, tgRT.api, raw)
	if err != nil {
		return TGDialogItem{}, err
	}

	switch p := peer.(type) {
	case *tg.InputPeerChannel:
		if p == nil || p.ChannelID == 0 {
			return TGDialogItem{}, errors.New("channel peer is invalid")
		}
		if p.AccessHash == 0 {
			return TGDialogItem{}, fmt.Errorf("channel access hash missing (channel_id=%d)", p.ChannelID)
		}

		var chats tg.MessagesChatsClass
		for attempt := 0; attempt < 5; attempt++ {
			chats, err = tgRT.api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
				&tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			})
			if err == nil {
				break
			}
			if ok, _ := tgerr.FloodWait(ctx, err); ok {
				continue
			}
			return TGDialogItem{}, err
		}
		if err != nil {
			return TGDialogItem{}, err
		}

		title := ""
		username := ""
		kind := "channel"

		for _, c := range chats.GetChats() {
			switch ch := c.(type) {
			case *tg.Channel:
				if ch == nil || ch.ID != p.ChannelID {
					continue
				}
				title = strings.TrimSpace(ch.Title)
				username = strings.TrimSpace(ch.Username)
				if ch.Megagroup {
					kind = "supergroup"
				} else if ch.Broadcast {
					kind = "channel"
				}
			case *tg.ChannelForbidden:
				if ch == nil || ch.ID != p.ChannelID {
					continue
				}
				title = strings.TrimSpace(ch.Title)
				if ch.Megagroup {
					kind = "supergroup"
				} else if ch.Broadcast {
					kind = "channel"
				}
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
		}, nil

	case *tg.InputPeerChat:
		if p == nil || p.ChatID == 0 {
			return TGDialogItem{}, errors.New("chat peer is invalid")
		}

		var chats tg.MessagesChatsClass
		for attempt := 0; attempt < 5; attempt++ {
			chats, err = tgRT.api.MessagesGetChats(ctx, []int64{p.ChatID})
			if err == nil {
				break
			}
			if ok, _ := tgerr.FloodWait(ctx, err); ok {
				continue
			}
			return TGDialogItem{}, err
		}
		if err != nil {
			return TGDialogItem{}, err
		}

		title := ""
		for _, c := range chats.GetChats() {
			switch ch := c.(type) {
			case *tg.Chat:
				if ch == nil || ch.ID != p.ChatID {
					continue
				}
				title = strings.TrimSpace(ch.Title)
			case *tg.ChatForbidden:
				if ch == nil || ch.ID != p.ChatID {
					continue
				}
				title = strings.TrimSpace(ch.Title)
			}
		}
		if title == "" {
			title = fmt.Sprintf("group %d", p.ChatID)
		}

		// Bot API basic group chat_id is negative.
		botChatID := "-" + strconv.FormatInt(p.ChatID, 10)

		return TGDialogItem{
			Kind:      "group",
			Title:     title,
			ChatID:    p.ChatID,
			BotChatID: botChatID,
			TaskValue: botChatID,
		}, nil

	default:
		return TGDialogItem{}, fmt.Errorf("unsupported peer type: %T", peer)
	}
}
