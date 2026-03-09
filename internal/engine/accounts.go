package engine

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type TGAccount struct {
	Key       string `json:"key"`
	UpdatedAt int64  `json:"updated_at"`
	Size      int64  `json:"size"`

	// Best-effort metadata (filled after login).
	UserID   int64  `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Avatar   string `json:"avatar,omitempty"` // data URI (base64)

	MetaUpdatedAt int64 `json:"meta_updated_at,omitempty"`
}

func ListTGAccounts() ([]TGAccount, error) {
	base := SessionBasePath()
	pattern := filepath.Join(base, "session_*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	out := make([]TGAccount, 0, len(matches))
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil || info == nil || info.IsDir() {
			continue
		}

		name := filepath.Base(p)
		if strings.HasSuffix(name, ".meta.json") {
			continue
		}
		name = strings.TrimSuffix(name, ".json")
		name = strings.TrimPrefix(name, "session_")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		out = append(out, TGAccount{
			Key:       name,
			UpdatedAt: info.ModTime().Unix(),
			Size:      info.Size(),
		})
	}

	for i := range out {
		if meta, ok := loadAccountMeta(out[i].Key); ok {
			out[i].UserID = meta.UserID
			out[i].Username = meta.Username
			out[i].Name = meta.displayName()
			out[i].Phone = meta.Phone
			out[i].Avatar = meta.Avatar
			out[i].MetaUpdatedAt = meta.UpdatedAt
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].Key < out[j].Key
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})

	return out, nil
}

func RemoveTGAccount(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key is required")
	}

	sessionPath := GetSessionPathForKey(key)
	metaPath := sessionMetaPathForKey(key)

	if Manager != nil && Manager.tg != nil {
		Manager.tg.stopAndDelete(sessionPath)
	}

	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
