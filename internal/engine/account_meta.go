package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/tg"
)

type tgAccountMeta struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Phone     string `json:"phone,omitempty"`

	UpdatedAt int64 `json:"updated_at"`
}

func (m tgAccountMeta) displayName() string {
	name := strings.TrimSpace(strings.TrimSpace(m.FirstName) + " " + strings.TrimSpace(m.LastName))
	if name != "" {
		return name
	}
	if u := strings.TrimSpace(m.Username); u != "" {
		return "@" + u
	}
	return ""
}

func sessionMetaPathForKey(key string) string {
	sessionPath := GetSessionPathForKey(key)
	dir := filepath.Dir(sessionPath)
	base := filepath.Base(sessionPath)
	if strings.HasSuffix(base, ".json") {
		base = strings.TrimSuffix(base, ".json")
	}
	return filepath.Join(dir, base+".meta.json")
}

func loadAccountMeta(key string) (tgAccountMeta, bool) {
	path := sessionMetaPathForKey(key)
	b, err := os.ReadFile(path)
	if err != nil {
		return tgAccountMeta{}, false
	}

	var meta tgAccountMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return tgAccountMeta{}, false
	}
	return meta, true
}

func saveAccountMeta(key string, meta tgAccountMeta) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("session key is required")
	}

	meta.UpdatedAt = time.Now().Unix()
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	path := sessionMetaPathForKey(key)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "session_meta_*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func updateAccountMetaFromAPI(ctx context.Context, key string, api *tg.Client) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("session key is required")
	}
	if api == nil {
		return errors.New("tg api is nil")
	}

	users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return errors.New("empty users response")
	}

	u, ok := users[0].(*tg.User)
	if !ok || u == nil {
		return fmt.Errorf("unexpected user type: %T", users[0])
	}

	meta := tgAccountMeta{
		UserID:    u.ID,
		Username:  strings.TrimSpace(u.Username),
		FirstName: strings.TrimSpace(u.FirstName),
		LastName:  strings.TrimSpace(u.LastName),
		Phone:     maskPhone(strings.TrimSpace(u.Phone)),
	}
	return saveAccountMeta(key, meta)
}

func maskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	phone = strings.ReplaceAll(phone, " ", "")

	prefix := ""
	if strings.HasPrefix(phone, "+") {
		prefix = "+"
		phone = strings.TrimPrefix(phone, "+")
	}

	digits := make([]rune, 0, len(phone))
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) <= 4 {
		return prefix + string(digits)
	}

	start := 3
	end := 2
	if len(digits) < start+end {
		start = 1
		end = 1
	}

	return prefix + string(digits[:start]) + strings.Repeat("*", len(digits)-start-end) + string(digits[len(digits)-end:])
}
