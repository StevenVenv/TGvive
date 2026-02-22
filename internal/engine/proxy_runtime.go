package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"my-go-server/internal/global"

	"go.uber.org/zap"
)

// HotApplyProxy triggers a best-effort reconnect for all running Telegram runtimes
// so new proxy settings take effect without restarting the server.
func (m *TaskManager) HotApplyProxy(ctx context.Context) int {
	if m == nil || m.tg == nil {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}

	snap := global.ProxyRuntime.Snapshot()
	cfg := snap.Config

	runtimes := m.tg.snapshot()
	applied := 0
	for _, rt := range runtimes {
		if rt == nil {
			continue
		}

		rt.mu.Lock()
		client := rt.client
		sessionPath := rt.sessionPath
		rt.mu.Unlock()

		if client == nil {
			continue
		}

		applyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		err := client.MigrateTo(applyCtx, 2)
		cancel()
		if err != nil {
			if global.Logger != nil {
				global.Logger.Warn("proxy hot apply: telegram restart failed", zap.String("session", sessionPath), zap.Error(err))
			}
			continue
		}
		applied++
	}

	// Flush idle HTTP connections (Bot API, etc.) so new proxy settings take effect immediately.
	closeRuntimeHTTPIdleConns()

	if global.Logger != nil {
		global.Logger.Info(
			"proxy hot applied",
			zap.Int("runtimes", applied),
			zap.Bool("enabled", cfg.Enabled),
			zap.String("type", cfg.Type),
			zap.String("host", cfg.Host),
			zap.Int("port", cfg.Port),
		)
	}

	return applied
}

type ProxyTestResult struct {
	Target string `json:"target"`
	PingMS int64  `json:"ping_ms"`
}

func TestProxyDial(ctx context.Context, cfg global.ProxyConfig, target string) (ProxyTestResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	target = strings.TrimSpace(target)
	if target == "" {
		target = "api.telegram.org:443"
	}

	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	if cfg.Type == "" {
		cfg.Type = "http"
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)

	start := time.Now()
	var (
		conn net.Conn
		err  error
	)
	switch {
	case !cfg.Enabled:
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", target)
	case cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535:
		err = errors.New("proxy 配置无效: Host/Port")
	default:
		proxyAddr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
		switch cfg.Type {
		case "http":
			conn, err = dialHTTPProxyCONNECT(ctx, proxyAddr, cfg.Username, cfg.Password, target)
		case "socks5":
			conn, err = dialSOCKS5(ctx, proxyAddr, cfg.Username, cfg.Password, "tcp", target)
		default:
			err = fmt.Errorf("proxy.type 不支持: %q (期望: http|socks5)", cfg.Type)
		}
	}
	if err != nil {
		return ProxyTestResult{Target: target}, err
	}
	_ = conn.Close()

	return ProxyTestResult{
		Target: target,
		PingMS: time.Since(start).Milliseconds(),
	}, nil
}
