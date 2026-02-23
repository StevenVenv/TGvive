package engine

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type privatePeerRef struct {
	ChannelID int64
	BotChatID string // -100<channel_id>
}

func parsePrivatePeerRef(raw string) (privatePeerRef, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return privatePeerRef{}, false, nil
	}

	// Bot API channel/supergroup chat id, e.g. -100123456789.
	if strings.HasPrefix(raw, "-100") && len(raw) > len("-100") {
		idStr := strings.TrimPrefix(raw, "-100")
		if !isDigits(idStr) {
			return privatePeerRef{}, true, errors.New("invalid -100... chat id")
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			return privatePeerRef{}, true, errors.New("invalid -100... chat id")
		}
		return privatePeerRef{ChannelID: id, BotChatID: "-100" + idStr}, true, nil
	}

	// Telegram private post links, e.g. https://t.me/c/123456789/10
	if strings.Contains(raw, "t.me/") || strings.Contains(raw, "telegram.me/") {
		s := raw
		// url.Parse without scheme treats the input as path.
		if !strings.Contains(s, "://") && (strings.HasPrefix(s, "t.me/") || strings.HasPrefix(s, "telegram.me/")) {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err == nil && u != nil {
			path := strings.Trim(strings.TrimSpace(u.Path), "/")
			parts := strings.Split(path, "/")
			if len(parts) >= 2 && parts[0] == "c" {
				idStr := strings.TrimSpace(parts[1])
				if idStr == "" {
					return privatePeerRef{}, true, errors.New("invalid t.me/c url")
				}
				if !isDigits(idStr) {
					return privatePeerRef{}, true, errors.New("invalid t.me/c channel id")
				}
				id, err := strconv.ParseInt(idStr, 10, 64)
				if err != nil || id <= 0 {
					return privatePeerRef{}, true, errors.New("invalid t.me/c channel id")
				}
				return privatePeerRef{ChannelID: id, BotChatID: "-100" + idStr}, true, nil
			}
		}
	}

	// tg://privatepost?channel=123456789&post=10 (Telegram Desktop "Copy link" variant)
	if strings.HasPrefix(raw, "tg://") {
		u, err := url.Parse(raw)
		if err == nil && u != nil && strings.EqualFold(u.Scheme, "tg") && strings.EqualFold(u.Host, "privatepost") {
			ch := strings.TrimSpace(u.Query().Get("channel"))
			if ch == "" {
				return privatePeerRef{}, true, errors.New("invalid tg://privatepost url: channel missing")
			}
			if !isDigits(ch) {
				return privatePeerRef{}, true, errors.New("invalid tg://privatepost channel id")
			}
			id, err := strconv.ParseInt(ch, 10, 64)
			if err != nil || id <= 0 {
				return privatePeerRef{}, true, errors.New("invalid tg://privatepost channel id")
			}
			return privatePeerRef{ChannelID: id, BotChatID: "-100" + ch}, true, nil
		}
	}

	return privatePeerRef{}, false, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

