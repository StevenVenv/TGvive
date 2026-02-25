package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type TGAccountSessionCheckResult struct {
	Ok         bool  `json:"ok"`
	CheckedAt  int64 `json:"checked_at"`
	Authorized bool  `json:"authorized"`
	Error      string `json:"error,omitempty"`

	UserID   int64  `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
	Phone    string `json:"phone,omitempty"`

	Restricted          bool     `json:"restricted,omitempty"`
	RestrictionReasons  []string `json:"restriction_reasons,omitempty"`
	Country             string   `json:"country,omitempty"`
	ThisDC              int      `json:"this_dc,omitempty"`
	NearestDC           int      `json:"nearest_dc,omitempty"`
}

type TGAccountSpamBotCheckResult struct {
	Ok        bool  `json:"ok"`
	CheckedAt int64 `json:"checked_at"`

	// Status: ok | restricted | blocked | unknown | error
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`

	// SpamBot reply text (best-effort, truncated).
	Text string `json:"text,omitempty"`
}

func CheckTGAccountSession(ctx context.Context, key string) TGAccountSessionCheckResult {
	res := TGAccountSessionCheckResult{
		Ok:        false,
		CheckedAt: time.Now().Unix(),
	}

	key = strings.TrimSpace(key)
	if key == "" {
		res.Error = "key 不能为空"
		return res
	}

	sessionPath := GetSessionPathForKey(key)
	if _, err := os.Stat(sessionPath); err != nil {
		if os.IsNotExist(err) {
			res.Error = "session 不存在"
		} else {
			res.Error = "检查 session 失败: " + err.Error()
		}
		return res
	}

	apiID, apiHash, err := pickTelegramApp()
	if err != nil {
		res.Error = err.Error()
		return res
	}

	client, err := newTelegramClient(apiID, apiHash, sessionPath, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	runErr := client.Run(ctx, func(ctx context.Context) error {
		st, err := client.Auth().Status(ctx)
		if err != nil {
			res.Error = err.Error()
			return nil
		}

		res.Ok = true
		res.Authorized = st.Authorized
		if !st.Authorized {
			res.Error = "telegram 未授权：请重新登录生成 session 文件"
			return nil
		}

		api := client.API()
		users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
		if err != nil {
			res.Ok = false
			res.Error = err.Error()
			return nil
		}
		if len(users) == 0 {
			res.Ok = false
			res.Error = "empty users response"
			return nil
		}
		u, ok := users[0].(*tg.User)
		if !ok || u == nil {
			res.Ok = false
			res.Error = fmt.Sprintf("unexpected user type: %T", users[0])
			return nil
		}

		res.UserID = u.ID
		res.Username = strings.TrimSpace(u.Username)
		res.Phone = maskPhone(strings.TrimSpace(u.Phone))
		res.Restricted = u.Restricted

		if rr, ok := u.GetRestrictionReason(); ok && len(rr) > 0 {
			out := make([]string, 0, len(rr))
			for _, r := range rr {
				txt := strings.TrimSpace(r.Text)
				if txt == "" {
					txt = strings.TrimSpace(r.Reason)
				}
				if txt == "" {
					txt = strings.TrimSpace(r.Platform)
				}
				if txt != "" {
					out = append(out, txt)
				}
			}
			if len(out) > 0 {
				res.RestrictionReasons = out
			}
		}

		if nd, err := api.HelpGetNearestDC(ctx); err == nil && nd != nil {
			res.Country = strings.TrimSpace(nd.Country)
			res.ThisDC = nd.ThisDC
			res.NearestDC = nd.NearestDC
		}

		return nil
	})
	if runErr != nil {
		res.Ok = false
		if res.Error == "" {
			res.Error = runErr.Error()
		}
	}

	return res
}

func CheckTGAccountSpamBot(ctx context.Context, key string, autoUnblock bool) TGAccountSpamBotCheckResult {
	res := TGAccountSpamBotCheckResult{
		Ok:        false,
		CheckedAt: time.Now().Unix(),
		Status:    "unknown",
	}

	key = strings.TrimSpace(key)
	if key == "" {
		res.Status = "error"
		res.Error = "key 不能为空"
		return res
	}

	sessionPath := GetSessionPathForKey(key)
	if _, err := os.Stat(sessionPath); err != nil {
		res.Status = "error"
		if os.IsNotExist(err) {
			res.Error = "session 不存在"
		} else {
			res.Error = "检查 session 失败: " + err.Error()
		}
		return res
	}

	apiID, apiHash, err := pickTelegramApp()
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}

	client, err := newTelegramClient(apiID, apiHash, sessionPath, nil)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}

	runErr := client.Run(ctx, func(ctx context.Context) error {
		st, err := client.Auth().Status(ctx)
		if err != nil {
			res.Status = "error"
			res.Error = err.Error()
			return nil
		}
		if !st.Authorized {
			res.Status = "error"
			res.Error = "telegram 未授权：请重新登录生成 session 文件"
			return nil
		}

		api := client.API()

		botUser, peer, err := resolveUsernameToInputPeerUser(ctx, api, "SpamBot")
		if err != nil {
			res.Status = "error"
			res.Error = err.Error()
			return nil
		}

		sendStart := func() (tg.UpdatesClass, error) {
			rid, err := randomID()
			if err != nil {
				return nil, err
			}
			return api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
				Peer:     peer,
				Message:  "/start",
				RandomID: rid,
			})
		}

		upd, err := sendStart()
		if err != nil {
			// If user blocked SpamBot, allow optional auto-unblock once.
			if rpcErr, ok := tgerr.As(err); ok && rpcErr != nil {
				if rpcErr.IsOneOf("USER_IS_BLOCKED", "YOU_BLOCKED_USER") {
					if autoUnblock {
						_, _ = api.ContactsUnblock(ctx, &tg.ContactsUnblockRequest{ID: peer})
						upd, err = sendStart()
					} else {
						res.Ok = true
						res.Status = "blocked"
						res.Error = "SpamBot 被当前账号屏蔽（可手动解除屏蔽或开启自动解除）"
						return nil
					}
				}
			}
			if err != nil {
				res.Status = "error"
				res.Error = err.Error()
				return nil
			}
		}

		sentID := 0
		for _, id := range extractSentMsgIDs(upd) {
			if id > sentID {
				sentID = id
			}
		}

		txt, ok, werr := waitSpamBotReply(ctx, api, peer, botUser.ID, sentID)
		if werr != nil {
			res.Status = "unknown"
			res.Error = werr.Error()
			return nil
		}
		if !ok {
			res.Status = "unknown"
			res.Error = "未收到 SpamBot 回复（可能网络较慢/被屏蔽/被限流）"
			return nil
		}

		res.Text = truncateUTF8(strings.TrimSpace(txt), 2000)
		res.Status = classifySpamBotText(res.Text)
		if res.Status == "" {
			res.Status = "unknown"
		}
		res.Ok = true
		res.Error = ""
		return nil
	})
	if runErr != nil {
		res.Ok = false
		res.Status = "error"
		if res.Error == "" {
			res.Error = runErr.Error()
		}
	}

	return res
}

func resolveUsernameToInputPeerUser(ctx context.Context, api *tg.Client, username string) (*tg.User, *tg.InputPeerUser, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if api == nil {
		return nil, nil, errors.New("tg api is nil")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil, errors.New("empty username")
	}

	r, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return nil, nil, err
	}

	peerUser, ok := r.Peer.(*tg.PeerUser)
	if !ok || peerUser == nil || peerUser.UserID == 0 {
		return nil, nil, fmt.Errorf("unexpected peer type: %T", r.Peer)
	}

	var u *tg.User
	for _, item := range r.Users {
		uu, ok := item.(*tg.User)
		if !ok || uu == nil {
			continue
		}
		if uu.ID == peerUser.UserID {
			u = uu
			break
		}
	}
	if u == nil {
		return nil, nil, errors.New("resolve username: user not found")
	}

	peer := &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}
	return u, peer, nil
}

func waitSpamBotReply(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, botUserID int64, minMsgID int) (text string, ok bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if api == nil {
		return "", false, errors.New("tg api is nil")
	}
	if peer == nil {
		return "", false, errors.New("tg peer is nil")
	}

	// Poll history for a short period; SpamBot usually replies quickly.
	deadline := time.Now().Add(8 * time.Second)
	if dl, ok := ctx.Deadline(); ok {
		deadline = dl
	}

	for time.Now().Before(deadline) {
		r, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: 6})
		if err == nil {
			msgs := extractTGMessages(r)
			for _, m := range msgs {
				if m == nil {
					continue
				}
				if minMsgID > 0 && m.ID <= minMsgID {
					continue
				}
				if m.Out {
					continue
				}
				if botUserID > 0 {
					if p, ok := m.FromID.(*tg.PeerUser); ok && p != nil {
						if p.UserID != botUserID {
							continue
						}
					}
				}
				if strings.TrimSpace(m.Message) == "" {
					continue
				}
				return m.Message, true, nil
			}
		}

		// Wait a bit then retry.
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(450 * time.Millisecond):
		}
	}

	return "", false, nil
}

func classifySpamBotText(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return ""
	}

	// "OK" patterns first (they may contain the word "limit").
	okNeedles := []string{
		"no limits",
		"no limit",
		"not limited",
		"no restrictions",
		"free as a bird",
		"good news",
		"未受到任何限制",
		"未受到限制",
		"没有限制",
		"当前没有限制",
		"未发现限制",
	}
	for _, n := range okNeedles {
		if n == "" {
			continue
		}
		if strings.Contains(t, strings.ToLower(n)) {
			return "ok"
		}
	}

	restrictedNeedles := []string{
		"limited",
		"restriction",
		"spam",
		"due to",
		"受限",
		"限制",
		"封禁",
		"违规",
		"举报",
	}
	for _, n := range restrictedNeedles {
		if n == "" {
			continue
		}
		if strings.Contains(t, strings.ToLower(n)) {
			return "restricted"
		}
	}

	return "unknown"
}

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	b = b[:maxBytes]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		return ""
	}
	return string(b)
}
