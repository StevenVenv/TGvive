package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"my-go-server/pkg/retry"
)

const (
	telegramSendRPCAttempts = 3
	telegramSendRPCTimeout  = 2 * time.Minute
)

func sendTelegramUpdatesWithRetry(ctx context.Context, action string, subject string, fn func(context.Context) (tg.UpdatesClass, error)) (tg.UpdatesClass, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return nil, nil
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "发送"
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "telegram request"
	}

	var last error
	for attempt := 1; attempt <= telegramSendRPCAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if attempt == 1 {
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s", action, subject))
		} else {
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s重试: %s attempt=%d/%d", action, subject, attempt, telegramSendRPCAttempts))
		}

		callCtx, cancel := context.WithTimeout(ctx, telegramSendRPCTimeout)
		upd, err := fn(callCtx)
		cancel()
		if err == nil {
			ids := extractSentMsgIDs(upd)
			if targetID := minPositiveInt(ids); targetID > 0 {
				recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s完成: %s target_id=%d", action, subject, targetID))
			} else {
				recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s完成: %s", action, subject))
			}
			return upd, nil
		}
		last = normalizeTelegramSendError(ctx, err)

		if wait, ok := tgerr.AsFloodWait(err); ok {
			if attempt >= telegramSendRPCAttempts {
				break
			}
			if wait < 0 {
				wait = 0
			}
			wait += time.Second
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s限速等待: %s wait=%.1fs err=%v", action, subject, wait.Seconds(), err))
			if serr := retry.Sleep(ctx, wait); serr != nil {
				return nil, serr
			}
			continue
		}

		if attempt >= telegramSendRPCAttempts || !isTelegramSendRPCRetryable(ctx, err) {
			break
		}

		wait := retry.WithJitter(retry.Backoff(attempt, 2*time.Second, 15*time.Second), 0.2)
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s重试等待: %s attempt=%d/%d wait=%.1fs err=%v", action, subject, attempt+1, telegramSendRPCAttempts, wait.Seconds(), last))
		if serr := retry.Sleep(ctx, wait); serr != nil {
			return nil, serr
		}
	}

	return nil, last
}

func normalizeTelegramSendError(parent context.Context, err error) error {
	if err == nil {
		return nil
	}
	if parent != nil && parent.Err() != nil {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("telegram send timeout after %s: %w", telegramSendRPCTimeout, err)
	}
	return err
}

func isTelegramSendRPCRetryable(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || retry.IsRetryableNetErr(err) {
		return true
	}
	if rpcErr, ok := tgerr.As(err); ok && rpcErr != nil {
		if rpcErr.Code >= 500 {
			return true
		}
		return rpcErr.IsOneOf(
			"Timedout",
			"No workers running",
			"RPC_CALL_FAIL",
			"RPC_MCGET_FAIL",
			"WORKER_BUSY_TOO_LONG_RETRY",
			"memory limit exit",
			"TIMEOUT",
			"INTERNAL",
			"SERVER_ERROR",
			"SERVICE_UNAVAILABLE",
		)
	}
	return false
}

func sendSubjectMsgID(msgID int) string {
	if msgID > 0 {
		return fmt.Sprintf("msg_id=%d", msgID)
	}
	return "msg_id=unknown"
}

func sendSubjectAlbum(groupedID int64, count int) string {
	if groupedID != 0 {
		return fmt.Sprintf("grouped_id=%d items=%d", groupedID, count)
	}
	return fmt.Sprintf("items=%d", count)
}
