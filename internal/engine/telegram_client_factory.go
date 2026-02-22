package engine

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"my-go-server/internal/global"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"golang.org/x/net/proxy"
)

func newTelegramClient(apiID int, apiHash, sessionPath string, updateHandler telegram.UpdateHandler) (*telegram.Client, error) {
	opts, err := telegramClientOptions(sessionPath, updateHandler)
	if err != nil {
		return nil, err
	}
	return telegram.NewClient(apiID, apiHash, opts), nil
}

func telegramClientOptions(sessionPath string, updateHandler telegram.UpdateHandler) (telegram.Options, error) {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return telegram.Options{}, errors.New("session_path is required")
	}

	opts := telegram.Options{
		SessionStorage: &FileSessionStorage{Path: sessionPath},
		// Always use a runtime-configurable resolver so proxy can be toggled
		// from the web panel without restarting the server.
		Resolver: dcs.Plain(dcs.PlainOptions{
			Dial: dialMTProtoWithRuntimeProxy,
		}),
		Device: defaultDeviceConfig(),
	}
	if updateHandler != nil {
		opts.UpdateHandler = updateHandler
	}
	return opts, nil
}

func dialMTProtoWithRuntimeProxy(ctx context.Context, network, addr string) (net.Conn, error) {
	snap := global.ProxyRuntime.Snapshot()
	cfg := snap.Config
	if !cfg.Enabled {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}

	t := strings.ToLower(strings.TrimSpace(cfg.Type))
	if t == "" {
		t = "http"
	}

	host := strings.TrimSpace(cfg.Host)
	if host == "" || cfg.Port < 1 || cfg.Port > 65535 {
		return nil, errors.New("proxy 配置无效: 请在 Web 面板保存正确的 Host/Port")
	}
	proxyAddr := net.JoinHostPort(host, strconv.Itoa(cfg.Port))

	switch t {
	case "http":
		return dialHTTPProxyCONNECT(ctx, proxyAddr, cfg.Username, cfg.Password, addr)
	case "socks5":
		return dialSOCKS5(ctx, proxyAddr, cfg.Username, cfg.Password, network, addr)
	default:
		return nil, fmt.Errorf("proxy.type 不支持: %q (期望: http|socks5)", cfg.Type)
	}
}

func dialSOCKS5(ctx context.Context, proxyAddr, username, password, network, addr string) (net.Conn, error) {
	var auth *proxy.Auth
	user := strings.TrimSpace(username)
	pass := strings.TrimSpace(password)
	if user != "" || pass != "" {
		auth = &proxy.Auth{User: user, Password: pass}
	}

	dialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, &net.Dialer{})
	if err != nil {
		return nil, fmt.Errorf("创建 SOCKS5 代理失败: %w", err)
	}
	ctxDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("SOCKS5 dialer 不支持 DialContext")
	}
	return ctxDialer.DialContext(ctx, network, addr)
}

func dialHTTPProxyCONNECT(ctx context.Context, proxyAddr, username, password, addr string) (_ net.Conn, rerr error) {
	if ctx == nil {
		ctx = context.Background()
	}

	user := strings.TrimSpace(username)
	pass := strings.TrimSpace(password)

	var authHeader string
	if user != "" || pass != "" {
		token := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		authHeader = "Proxy-Authorization: Basic " + token + "\r\n"
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr != nil {
			_ = conn.Close()
		}
	}()

	deadline := time.Now().Add(35 * time.Second)
	if dl, ok := ctx.Deadline(); ok {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	// CONNECT <host:port> HTTP/1.1
	var b strings.Builder
	b.Grow(128 + len(addr) + len(authHeader))
	b.WriteString("CONNECT ")
	b.WriteString(addr)
	b.WriteString(" HTTP/1.1\r\n")
	b.WriteString("Host: ")
	b.WriteString(addr)
	b.WriteString("\r\n")
	if authHeader != "" {
		b.WriteString(authHeader)
	}
	b.WriteString("Proxy-Connection: Keep-Alive\r\n\r\n")

	if _, err := io.WriteString(conn, b.String()); err != nil {
		return nil, err
	}

	br := bufio.NewReader(conn)
	tp := textproto.NewReader(br)

	statusLine, err := tp.ReadLine()
	if err != nil {
		return nil, err
	}
	_, err = tp.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("HTTP 代理响应无效: %q", statusLine)
	}
	code, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("HTTP 代理响应无效: %q", statusLine)
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("HTTP 代理 CONNECT 失败: %s", statusLine)
	}

	// Clear handshake deadline for long-lived MTProto connection.
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
