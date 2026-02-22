package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"my-go-server/internal/global"
)

type TGBot struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	APIBase  string `json:"api_base"`
	Disabled bool   `json:"disabled,omitempty"`

	TokenSet  bool   `json:"token_set"`
	TokenMask string `json:"token_mask,omitempty"`

	CreatedAt int64 `json:"created_at,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

type TGBotUpdate struct {
	Name     *string
	Token    *string
	APIBase  *string
	Disabled *bool
}

type TGBotTestResult struct {
	OK         bool   `json:"ok"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Error      string `json:"error,omitempty"`

	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`

	BotUserID int64  `json:"bot_user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	Name      string `json:"name,omitempty"`
}

func maskToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "***"
	}
	if len(token) <= 16 {
		return token[:2] + "***" + token[len(token)-2:]
	}
	return token[:6] + "..." + token[len(token)-4:]
}

func toPublicBot(b global.StoredBot) TGBot {
	token := strings.TrimSpace(b.Token)
	return TGBot{
		ID:        strings.TrimSpace(b.ID),
		Name:      strings.TrimSpace(b.Name),
		APIBase:   strings.TrimSpace(b.APIBase),
		Disabled:  b.Disabled,
		TokenSet:  token != "",
		TokenMask: maskToken(token),
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}

func ListTGBots() ([]TGBot, error) {
	raw := global.BotStore.List()
	out := make([]TGBot, 0, len(raw))
	for _, b := range raw {
		out = append(out, toPublicBot(b))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

func AddTGBot(name, token, apiBase string) (TGBot, error) {
	bot, err := global.BotStore.Add(name, token, apiBase)
	if err != nil {
		return TGBot{}, err
	}
	return toPublicBot(bot), nil
}

func UpdateTGBot(id string, upd TGBotUpdate) (TGBot, error) {
	next, err := global.BotStore.Update(id, global.BotStoreUpdate{
		Name:     upd.Name,
		Token:    upd.Token,
		APIBase:  upd.APIBase,
		Disabled: upd.Disabled,
	})
	if err != nil {
		return TGBot{}, err
	}
	return toPublicBot(next), nil
}

func RemoveTGBot(id string) error {
	return global.BotStore.Remove(id)
}

type tgBotAPIResp[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

type tgBotMe struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

func botAPIURL(apiBase, token, method string) string {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	method = strings.TrimLeft(strings.TrimSpace(method), "/")
	if method == "" {
		method = "getMe"
	}
	token = strings.TrimSpace(token)
	return apiBase + "/bot" + token + "/" + method
}

func TestTGBot(ctx context.Context, id string, timeout time.Duration) (TGBotTestResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TGBotTestResult{}, errors.New("id is required")
	}
	bot, ok := global.BotStore.Get(id)
	if !ok {
		return TGBotTestResult{}, errors.New("bot not found")
	}

	token := strings.TrimSpace(bot.Token)
	if token == "" {
		return TGBotTestResult{}, errors.New("token is empty")
	}

	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if timeout > 25*time.Second {
		timeout = 25 * time.Second
	}

	u := botAPIURL(bot.APIBase, token, "getMe")

	start := time.Now()
	client := &http.Client{Timeout: timeout}

	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return TGBotTestResult{}, err
	}

	res, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return TGBotTestResult{OK: false, LatencyMS: latency, Error: err.Error()}, nil
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return TGBotTestResult{OK: false, LatencyMS: latency, HTTPStatus: res.StatusCode, Error: err.Error()}, nil
	}

	var parsed tgBotAPIResp[tgBotMe]
	if err := json.Unmarshal(body, &parsed); err != nil {
		return TGBotTestResult{OK: false, LatencyMS: latency, HTTPStatus: res.StatusCode, Error: "invalid json: " + err.Error()}, nil
	}

	out := TGBotTestResult{
		OK:          parsed.OK,
		LatencyMS:   latency,
		HTTPStatus:  res.StatusCode,
		ErrorCode:   parsed.ErrorCode,
		Description: strings.TrimSpace(parsed.Description),
	}
	if parsed.OK {
		name := strings.TrimSpace(strings.TrimSpace(parsed.Result.FirstName) + " " + strings.TrimSpace(parsed.Result.LastName))
		out.BotUserID = parsed.Result.ID
		out.Username = strings.TrimSpace(parsed.Result.Username)
		out.Name = name
	} else if out.Description == "" && out.ErrorCode == 0 {
		out.Description = "bot api returned ok=false"
	}
	return out, nil
}
