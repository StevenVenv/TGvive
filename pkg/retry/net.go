package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
)

// IsRetryableNetErr returns true if err is likely caused by transient network conditions.
func IsRetryableNetErr(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Common io layer issues.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// Syscall-level transient errors.
	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	// net.Error covers timeout / temporary conditions.
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return true
		}
		// Temporary() is deprecated but still implemented by many errors.
		type temporary interface{ Temporary() bool }
		if te, ok := ne.(temporary); ok && te.Temporary() {
			return true
		}
	}

	// os.IsTimeout covers wrapped net errors.
	if os.IsTimeout(err) {
		return true
	}

	// Fallback by string match (best-effort for wrapped/opaque errors).
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "connection reset by peer"),
		strings.Contains(s, "broken pipe"),
		strings.Contains(s, "connection refused"),
		strings.Contains(s, "i/o timeout"),
		strings.Contains(s, "tls handshake timeout"),
		strings.Contains(s, "no such host"),
		strings.Contains(s, "network is unreachable"),
		strings.Contains(s, "retryuntilack"),
		strings.Contains(s, "retry limit reached"),
		strings.Contains(s, "rpc_call_fail"),
		strings.Contains(s, "rpc_mcget_fail"),
		strings.Contains(s, "worker_busy_too_long_retry"),
		strings.Contains(s, "no workers running"):
		return true
	}

	return false
}
