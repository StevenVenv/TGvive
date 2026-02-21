package global

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ProxyRuntimeSnapshot struct {
	Config      ProxyConfig
	UpdatedAt   int64
	PasswordSet bool
}

type ProxyRuntimeStore struct {
	mu        sync.RWMutex
	path      string
	cfg       ProxyConfig
	updatedAt int64
}

func NewProxyRuntimeStore(path string) *ProxyRuntimeStore {
	return &ProxyRuntimeStore{
		path: strings.TrimSpace(path),
	}
}

func defaultProxyRuntimePath() string {
	if p := strings.TrimSpace(os.Getenv("TGVIVE_PROXY_STORE_FILE")); p != "" {
		return p
	}
	return filepath.Join("data", "proxy.runtime.json")
}

var ProxyRuntime = NewProxyRuntimeStore(defaultProxyRuntimePath())

// Init seeds runtime proxy config with defaults from config file/env and then
// tries to override it from runtime store file (best-effort).
func (s *ProxyRuntimeStore) Init(defaultCfg ProxyConfig) {
	s.mu.Lock()
	s.cfg = normalizeProxyConfig(defaultCfg)
	s.updatedAt = 0
	s.mu.Unlock()

	_ = s.Load()
}

func (s *ProxyRuntimeStore) Load() error {
	if s == nil {
		return errors.New("proxy runtime store is nil")
	}
	path := strings.TrimSpace(s.path)
	if path == "" {
		return errors.New("proxy runtime store path is empty")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var payload proxyRuntimePayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Errorf("parse proxy runtime store: %w", err)
	}

	cfg := ProxyConfig{
		Enabled:  payload.Enabled,
		Type:     payload.Type,
		Host:     payload.Host,
		Port:     payload.Port,
		Username: payload.Username,
		Password: payload.Password,
	}
	if err := validateProxyConfig(cfg); err != nil {
		return err
	}
	cfg = normalizeProxyConfig(cfg)

	s.mu.Lock()
	s.cfg = cfg
	s.updatedAt = payload.UpdatedAt
	s.mu.Unlock()
	return nil
}

func (s *ProxyRuntimeStore) Snapshot() ProxyRuntimeSnapshot {
	if s == nil {
		return ProxyRuntimeSnapshot{}
	}
	s.mu.RLock()
	cfg := s.cfg
	updatedAt := s.updatedAt
	s.mu.RUnlock()

	return ProxyRuntimeSnapshot{
		Config:      cfg,
		UpdatedAt:   updatedAt,
		PasswordSet: strings.TrimSpace(cfg.Password) != "",
	}
}

func (s *ProxyRuntimeStore) Put(cfg ProxyConfig) error {
	if s == nil {
		return errors.New("proxy runtime store is nil")
	}

	if err := validateProxyConfig(cfg); err != nil {
		return err
	}
	cfg = normalizeProxyConfig(cfg)

	path := strings.TrimSpace(s.path)
	if path == "" {
		return errors.New("proxy runtime store path is empty")
	}

	now := time.Now().Unix()
	payload := proxyRuntimePayload{
		Enabled:   cfg.Enabled,
		Type:      cfg.Type,
		Host:      cfg.Host,
		Port:      cfg.Port,
		Username:  cfg.Username,
		Password:  cfg.Password,
		UpdatedAt: now,
	}

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
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	s.mu.Lock()
	s.cfg = cfg
	s.updatedAt = now
	s.mu.Unlock()
	return nil
}

type proxyRuntimePayload struct {
	Enabled   bool   `json:"enabled"`
	Type      string `json:"type"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

func normalizeProxyConfig(cfg ProxyConfig) ProxyConfig {
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	if cfg.Type == "" {
		cfg.Type = "http"
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	// Password is intentionally NOT trimmed: allow leading/trailing spaces.
	return cfg
}

func validateProxyConfig(cfg ProxyConfig) error {
	if !cfg.Enabled {
		return nil
	}

	t := strings.ToLower(strings.TrimSpace(cfg.Type))
	if t == "" {
		t = "http"
	}
	if t != "http" && t != "socks5" {
		return fmt.Errorf("proxy.type 不支持: %q (期望: http|socks5)", cfg.Type)
	}
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return errors.New("proxy.enabled=true 时必须配置 proxy.host")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("proxy.enabled=true 时必须配置有效的 proxy.port (1-65535)")
	}
	return nil
}
