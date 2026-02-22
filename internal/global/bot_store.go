package global

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type TGBotStore struct {
	mu        sync.RWMutex
	path      string
	bots      []StoredBot
	updatedAt int64
}

type StoredBot struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Token    string `json:"token,omitempty"`
	APIBase  string `json:"api_base,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`

	CreatedAt int64 `json:"created_at,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

type botStorePayload struct {
	UpdatedAt int64       `json:"updated_at,omitempty"`
	Bots      []StoredBot `json:"bots"`
}

func NewTGBotStore(path string) *TGBotStore {
	return &TGBotStore{path: strings.TrimSpace(path)}
}

func defaultBotStorePath() string {
	if p := strings.TrimSpace(os.Getenv("TGVIVE_BOT_STORE_FILE")); p != "" {
		return p
	}
	return filepath.Join("data", "bots.store.json")
}

var BotStore = NewTGBotStore(defaultBotStorePath())

func (s *TGBotStore) Init() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.bots = nil
	s.updatedAt = 0
	s.mu.Unlock()
	_ = s.Load()
}

func normalizeAPIBase(apiBase string) string {
	apiBase = strings.TrimSpace(apiBase)
	apiBase = strings.TrimRight(apiBase, "/")
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	return apiBase
}

func normalizeBot(b StoredBot) (StoredBot, bool) {
	b.ID = strings.TrimSpace(b.ID)
	b.Name = strings.TrimSpace(b.Name)
	b.Token = strings.TrimSpace(b.Token)
	b.APIBase = normalizeAPIBase(b.APIBase)
	if b.ID == "" || b.Token == "" {
		return StoredBot{}, false
	}
	if b.CreatedAt <= 0 {
		b.CreatedAt = b.UpdatedAt
	}
	return b, true
}

func (s *TGBotStore) Load() error {
	if s == nil {
		return errors.New("bot store is nil")
	}
	path := strings.TrimSpace(s.path)
	if path == "" {
		return errors.New("bot store path is empty")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var payload botStorePayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Errorf("parse bot store: %w", err)
	}

	out := make([]StoredBot, 0, len(payload.Bots))
	seen := make(map[string]struct{}, len(payload.Bots))
	for _, raw := range payload.Bots {
		bot, ok := normalizeBot(raw)
		if !ok {
			continue
		}
		if _, dup := seen[bot.ID]; dup {
			continue
		}
		seen[bot.ID] = struct{}{}
		out = append(out, bot)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})

	s.mu.Lock()
	s.bots = out
	s.updatedAt = payload.UpdatedAt
	s.mu.Unlock()
	return nil
}

func (s *TGBotStore) listUnsafe() []StoredBot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	out := append([]StoredBot(nil), s.bots...)
	s.mu.RUnlock()
	return out
}

func (s *TGBotStore) Get(id string) (StoredBot, bool) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return StoredBot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, b := range s.bots {
		if b.ID == id {
			return b, true
		}
	}
	return StoredBot{}, false
}

func (s *TGBotStore) List() []StoredBot {
	return s.listUnsafe()
}

func newBotID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *TGBotStore) saveLocked(path string, payload botStorePayload) error {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
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

func (s *TGBotStore) Add(name, token, apiBase string) (StoredBot, error) {
	if s == nil {
		return StoredBot{}, errors.New("bot store is nil")
	}
	path := strings.TrimSpace(s.path)
	if path == "" {
		return StoredBot{}, errors.New("bot store path is empty")
	}

	name = strings.TrimSpace(name)
	token = strings.TrimSpace(token)
	apiBase = normalizeAPIBase(apiBase)
	if token == "" {
		return StoredBot{}, errors.New("token is required")
	}

	id, err := newBotID()
	if err != nil {
		return StoredBot{}, err
	}

	now := time.Now().Unix()
	bot := StoredBot{
		ID:        id,
		Name:      name,
		Token:     token,
		APIBase:   apiBase,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	bots := append([]StoredBot(nil), s.bots...)
	bots = append(bots, bot)

	payload := botStorePayload{UpdatedAt: now, Bots: bots}
	if err := s.saveLocked(path, payload); err != nil {
		return StoredBot{}, err
	}
	s.bots = bots
	s.updatedAt = now
	return bot, nil
}

type BotStoreUpdate struct {
	Name     *string
	Token    *string
	APIBase  *string
	Disabled *bool
}

func (s *TGBotStore) Update(id string, upd BotStoreUpdate) (StoredBot, error) {
	if s == nil {
		return StoredBot{}, errors.New("bot store is nil")
	}
	path := strings.TrimSpace(s.path)
	if path == "" {
		return StoredBot{}, errors.New("bot store path is empty")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return StoredBot{}, errors.New("id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	bots := append([]StoredBot(nil), s.bots...)
	idx := -1
	for i := range bots {
		if bots[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return StoredBot{}, errors.New("bot not found")
	}

	next := bots[idx]
	if upd.Name != nil {
		next.Name = strings.TrimSpace(*upd.Name)
	}
	if upd.Token != nil {
		next.Token = strings.TrimSpace(*upd.Token)
	}
	if upd.APIBase != nil {
		next.APIBase = normalizeAPIBase(*upd.APIBase)
	}
	if upd.Disabled != nil {
		next.Disabled = *upd.Disabled
	}

	if strings.TrimSpace(next.Token) == "" {
		return StoredBot{}, errors.New("token is required")
	}

	now := time.Now().Unix()
	next.UpdatedAt = now
	bots[idx] = next

	payload := botStorePayload{UpdatedAt: now, Bots: bots}
	if err := s.saveLocked(path, payload); err != nil {
		return StoredBot{}, err
	}
	s.bots = bots
	s.updatedAt = now
	return next, nil
}

func (s *TGBotStore) Remove(id string) error {
	if s == nil {
		return errors.New("bot store is nil")
	}
	path := strings.TrimSpace(s.path)
	if path == "" {
		return errors.New("bot store path is empty")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	bots := make([]StoredBot, 0, len(s.bots))
	found := false
	for _, b := range s.bots {
		if b.ID == id {
			found = true
			continue
		}
		bots = append(bots, b)
	}
	if !found {
		return errors.New("bot not found")
	}

	now := time.Now().Unix()
	payload := botStorePayload{UpdatedAt: now, Bots: bots}
	if err := s.saveLocked(path, payload); err != nil {
		return err
	}
	s.bots = bots
	s.updatedAt = now
	return nil
}
