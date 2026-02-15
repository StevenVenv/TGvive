package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"my-go-server/internal/global"
)

type TGAccount struct {
	Key       string `json:"key"`
	UpdatedAt int64  `json:"updated_at"`
	Size      int64  `json:"size"`
}

func ListTGAccounts() ([]TGAccount, error) {
	base := strings.TrimSpace(global.Config.Telegram.SessionPath)
	if base == "" {
		base = "./sessions/"
	}

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

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].Key < out[j].Key
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})

	return out, nil
}
