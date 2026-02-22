package engine

import (
	"net/http"
	"time"
)

// runtimeHTTPTransport routes all outbound HTTP traffic through the runtime proxy config.
// When proxy is disabled, it dials direct (and ignores environment proxy variables).
var runtimeHTTPTransport = &http.Transport{
	Proxy: nil,
	// Reuse the same dialer used by MTProto so "http|socks5" proxy setting applies consistently.
	DialContext:           dialMTProtoWithRuntimeProxy,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   20,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

func runtimeHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: runtimeHTTPTransport,
	}
}

func closeRuntimeHTTPIdleConns() {
	if runtimeHTTPTransport != nil {
		runtimeHTTPTransport.CloseIdleConnections()
	}
}
