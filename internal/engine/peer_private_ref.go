package engine

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type telegramPeerRefKind int

const (
	telegramPeerRefUnknown telegramPeerRefKind = iota
	telegramPeerRefUsername
	telegramPeerRefChannelID
	telegramPeerRefGroupID
)

type telegramPeerRef struct {
	Kind      telegramPeerRefKind
	Username  string
	ChannelID int64
	ChatID    int64
	BotChatID string
}

func parseTelegramPeerRef(raw string) (telegramPeerRef, bool, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return telegramPeerRef{}, false, nil
	}

	if strings.HasPrefix(strings.ToLower(s), "tg://") {
		return parseTelegramDeepLink(s)
	}
	if u, ok, err := parseTelegramWebLink(s); ok || err != nil {
		if err != nil {
			return telegramPeerRef{}, true, err
		}
		return parseTelegramWebPeer(u)
	}

	if strings.HasPrefix(s, "-100") && len(s) > len("-100") {
		return parseBotChannelID(strings.TrimPrefix(s, "-100"))
	}
	if strings.HasPrefix(s, "-") && len(s) > 1 && isDigits(s[1:]) {
		id, err := strconv.ParseInt(s[1:], 10, 64)
		if err != nil || id <= 0 {
			return telegramPeerRef{}, true, errors.New("invalid negative chat id")
		}
		return telegramPeerRef{Kind: telegramPeerRefGroupID, ChatID: id, BotChatID: "-" + strconv.FormatInt(id, 10)}, true, nil
	}

	if strings.HasPrefix(s, "@") {
		username := strings.TrimSpace(strings.TrimPrefix(s, "@"))
		return parseTelegramUsername(username, "invalid @username")
	}
	if isTelegramUsername(s) {
		return telegramPeerRef{Kind: telegramPeerRefUsername, Username: s}, true, nil
	}

	return telegramPeerRef{}, false, nil
}

func parseBotChannelID(idStr string) (telegramPeerRef, bool, error) {
	idStr = strings.TrimSpace(idStr)
	if !isDigits(idStr) {
		return telegramPeerRef{}, true, errors.New("invalid -100... chat id")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return telegramPeerRef{}, true, errors.New("invalid -100... chat id")
	}
	return telegramPeerRef{Kind: telegramPeerRefChannelID, ChannelID: id, BotChatID: "-100" + idStr}, true, nil
}

func parseTelegramDeepLink(raw string) (telegramPeerRef, bool, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return telegramPeerRef{}, true, fmt.Errorf("invalid tg link: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "tg") {
		return telegramPeerRef{}, false, nil
	}

	host := strings.ToLower(strings.TrimSpace(u.Host))
	switch host {
	case "resolve":
		username := strings.TrimSpace(u.Query().Get("domain"))
		if username == "" {
			return telegramPeerRef{}, true, errors.New("invalid tg://resolve url: domain missing")
		}
		return parseTelegramUsername(username, "invalid tg://resolve domain")
	case "privatepost":
		channel := strings.TrimSpace(u.Query().Get("channel"))
		if channel == "" {
			return telegramPeerRef{}, true, errors.New("invalid tg://privatepost url: channel missing")
		}
		channel = strings.TrimPrefix(channel, "-100")
		return parseBotChannelID(channel)
	case "join":
		return telegramPeerRef{}, true, errors.New("telegram invite links must be joined first; use @username or -100 chat id after the account can access the chat")
	default:
		return telegramPeerRef{}, true, fmt.Errorf("unsupported tg link: %s", host)
	}
}

func parseTelegramWebLink(raw string) (*url.URL, bool, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, false, nil
	}
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "://") && hasTelegramHostPrefix(lower) {
		s = "https://" + s
	}

	u, err := url.Parse(s)
	if err != nil || u == nil {
		if hasTelegramHostPrefix(lower) || strings.Contains(lower, "t.me/") || strings.Contains(lower, "telegram.me/") {
			return nil, true, fmt.Errorf("invalid telegram url: %w", err)
		}
		return nil, false, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false, nil
	}
	if !isTelegramWebHost(u.Hostname()) {
		return nil, false, nil
	}
	return u, true, nil
}

func parseTelegramWebPeer(u *url.URL) (telegramPeerRef, bool, error) {
	if u == nil {
		return telegramPeerRef{}, false, nil
	}
	parts := cleanTelegramPathParts(u.Path)
	if len(parts) == 0 {
		return telegramPeerRef{}, true, errors.New("invalid telegram url: path is empty")
	}

	switch strings.ToLower(parts[0]) {
	case "c":
		if len(parts) < 2 {
			return telegramPeerRef{}, true, errors.New("invalid t.me/c url")
		}
		return parseBotChannelID(parts[1])
	case "s":
		if len(parts) < 2 {
			return telegramPeerRef{}, true, errors.New("invalid t.me/s url")
		}
		return parseTelegramUsername(parts[1], "invalid t.me/s username")
	case "joinchat":
		return telegramPeerRef{}, true, errors.New("telegram invite links must be joined first; use @username or -100 chat id after the account can access the chat")
	}

	first := strings.TrimSpace(parts[0])
	if strings.HasPrefix(first, "+") {
		return telegramPeerRef{}, true, errors.New("telegram invite links must be joined first; use @username or -100 chat id after the account can access the chat")
	}
	if isTelegramServicePath(first) {
		return telegramPeerRef{}, true, fmt.Errorf("unsupported telegram url path: %s", strings.Join(parts, "/"))
	}
	return parseTelegramUsername(first, "invalid telegram username")
}

func parseTelegramUsername(username string, msg string) (telegramPeerRef, bool, error) {
	username = strings.TrimSpace(strings.TrimPrefix(username, "@"))
	if !isTelegramUsername(username) {
		return telegramPeerRef{}, true, errors.New(msg)
	}
	return telegramPeerRef{Kind: telegramPeerRefUsername, Username: username}, true, nil
}

func hasTelegramHostPrefix(s string) bool {
	return strings.HasPrefix(s, "t.me/") || strings.HasPrefix(s, "telegram.me/") || strings.HasPrefix(s, "www.t.me/") || strings.HasPrefix(s, "www.telegram.me/")
}

func isTelegramWebHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	return host == "t.me" || host == "telegram.me"
}

func cleanTelegramPathParts(path string) []string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return nil
	}
	raw := strings.Split(path, "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if decoded, err := url.PathUnescape(part); err == nil {
			part = decoded
		}
		parts = append(parts, part)
	}
	return parts
}

func isTelegramServicePath(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "addemoji", "addstickers", "bg", "confirmphone", "iv", "login", "proxy", "setlanguage", "share", "socks":
		return true
	default:
		return false
	}
}

func isTelegramUsername(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 5 || len(s) > 32 {
		return false
	}
	hasLetter := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			hasLetter = true
		}
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return hasLetter
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
