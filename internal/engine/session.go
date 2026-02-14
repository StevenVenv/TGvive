package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"my-go-server/internal/global"
)

// FileSessionStorage stores gotd session bytes in a local file.
// The file contains sensitive credentials; it is written with 0600 permissions.
type FileSessionStorage struct {
	Path string
}

func (f *FileSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(f.Path) == "" {
		return nil, nil
	}

	data, err := os.ReadFile(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

func (f *FileSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(f.Path) == "" {
		return fmt.Errorf("empty session path")
	}

	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f.Path, data, 0o600)
}

func GetSessionPath(phone string) string {
	basePath := strings.TrimSpace(global.Config.Telegram.SessionPath)
	if basePath == "" {
		basePath = "./sessions/"
	}

	safe := sanitizePhone(phone)
	if safe == "" {
		safe = "unknown"
	}

	return filepath.Join(basePath, fmt.Sprintf("session_%s.json", safe))
}

func GetSessionPathForKey(key string) string {
	basePath := strings.TrimSpace(global.Config.Telegram.SessionPath)
	if basePath == "" {
		basePath = "./sessions/"
	}

	safe := sanitizeSessionKey(key)
	if safe == "" {
		safe = "unknown"
	}

	return filepath.Join(basePath, fmt.Sprintf("session_%s.json", safe))
}

func sanitizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(phone))
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sanitizeSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
