package engine

import (
	"context"
	"testing"

	"my-go-server/internal/global"
)

func TestTelegramClientOptions_AlwaysHasResolver(t *testing.T) {
	opts, err := telegramClientOptions("sessions/session_test.json", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Resolver == nil {
		t.Fatalf("expected Resolver non-nil")
	}
}

func TestDialMTProtoWithRuntimeProxy_InvalidConfigFastFail(t *testing.T) {
	old := global.ProxyRuntime
	store := global.NewProxyRuntimeStore("")
	store.Init(global.ProxyConfig{Enabled: true, Type: "socks5", Host: "", Port: 0})
	global.ProxyRuntime = store
	t.Cleanup(func() { global.ProxyRuntime = old })

	_, err := dialMTProtoWithRuntimeProxy(context.Background(), "tcp", "example.com:443")
	if err == nil {
		t.Fatalf("expected error for invalid proxy config")
	}
}
