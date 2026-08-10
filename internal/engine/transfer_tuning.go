package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/multierr"

	"my-go-server/pkg/retry"
)

const (
	telegramDownloadMaxPartSize = 1024 * 1024
	telegramUploadMaxPartSize   = uploader.MaximumPartSize
	telegramTransferThreads     = 4
	telegramDownloadVerify      = false
	telegramTransferRPCAttempts = 5
)

func newTelegramMediaDownloader() *downloader.Downloader {
	return downloader.NewDownloader().WithPartSize(telegramDownloadMaxPartSize)
}

func newTelegramMediaUploader(api *tg.Client) *uploader.Uploader {
	return uploader.NewUploader(api).WithPartSize(telegramUploadMaxPartSize)
}

func bestTelegramTransferThreads(size int64) int {
	_ = size
	return telegramTransferThreads
}

type telegramTransferPools struct {
	mu    sync.Mutex
	pools map[*tg.Client]*telegramTransferPool
}

type telegramTransferPool struct {
	client   *telegram.Client
	api      *tg.Client
	size     int64
	mu       sync.Mutex
	invokers map[int]telegram.CloseInvoker
}

func newTelegramTransferPools() *telegramTransferPools {
	return &telegramTransferPools{pools: map[*tg.Client]*telegramTransferPool{}}
}

func (p *telegramTransferPools) Add(client *telegram.Client, api *tg.Client, size int64) {
	if p == nil || client == nil || api == nil {
		return
	}
	if size <= 0 {
		size = telegramTransferThreads
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pools[api]; ok {
		return
	}
	p.pools[api] = &telegramTransferPool{
		client:   client,
		api:      api,
		size:     size,
		invokers: map[int]telegram.CloseInvoker{},
	}
}

func (p *telegramTransferPools) Get(api *tg.Client) *telegramTransferPool {
	if p == nil || api == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pools[api]
}

func (p *telegramTransferPools) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	pools := make([]*telegramTransferPool, 0, len(p.pools))
	for _, pool := range p.pools {
		pools = append(pools, pool)
	}
	p.mu.Unlock()

	var err error
	for _, pool := range pools {
		err = multierr.Append(err, pool.Close())
	}
	return err
}

func (p *telegramTransferPool) currentDC() int {
	if p == nil || p.client == nil {
		return 0
	}
	return p.client.Config().ThisDC
}

func (p *telegramTransferPool) Client(ctx context.Context, dcID int) (*tg.Client, int, error) {
	if p == nil || p.client == nil || p.api == nil {
		return nil, 0, fmt.Errorf("telegram transfer pool is nil")
	}
	if dcID <= 0 {
		dcID = p.currentDC()
	}
	if dcID <= 0 {
		return p.api, 0, nil
	}

	p.mu.Lock()
	if invoker, ok := p.invokers[dcID]; ok {
		p.mu.Unlock()
		return tg.NewClient(invoker), dcID, nil
	}
	p.mu.Unlock()

	var (
		invoker telegram.CloseInvoker
		err     error
	)
	if dcID == p.currentDC() {
		invoker, err = p.client.Pool(p.size)
	} else {
		invoker, err = p.client.DC(ctx, dcID, p.size)
	}
	if err != nil {
		return nil, 0, err
	}
	invoker = wrapTelegramTransferInvoker(invoker)

	p.mu.Lock()
	if existing, ok := p.invokers[dcID]; ok {
		p.mu.Unlock()
		_ = invoker.Close()
		return tg.NewClient(existing), dcID, nil
	}
	p.invokers[dcID] = invoker
	p.mu.Unlock()

	return tg.NewClient(invoker), dcID, nil
}

type telegramRetryCloseInvoker struct {
	telegram.CloseInvoker
}

func wrapTelegramTransferInvoker(invoker telegram.CloseInvoker) telegram.CloseInvoker {
	if invoker == nil {
		return nil
	}
	return telegramRetryCloseInvoker{CloseInvoker: invoker}
}

func (i telegramRetryCloseInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	if i.CloseInvoker == nil {
		return fmt.Errorf("telegram transfer invoker is nil")
	}

	var last error
	for attempt := 1; attempt <= telegramTransferRPCAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := i.CloseInvoker.Invoke(ctx, input, output)
		if err == nil {
			return nil
		}
		last = err

		if attempt >= telegramTransferRPCAttempts || !isTelegramTransferRPCRetryable(ctx, err) {
			return err
		}

		wait := retry.WithJitter(retry.Backoff(attempt, 500*time.Millisecond, 5*time.Second), 0.2)
		if serr := retry.Sleep(ctx, wait); serr != nil {
			return serr
		}
	}
	return last
}

func isTelegramTransferRPCRetryable(ctx context.Context, err error) bool {
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
		)
	}
	return false
}

func (p *telegramTransferPool) Default(ctx context.Context) (*tg.Client, int, error) {
	if p == nil {
		return nil, 0, fmt.Errorf("telegram transfer pool is nil")
	}
	return p.Client(ctx, p.currentDC())
}

func (p *telegramTransferPool) Invalidate(dcID int) {
	if p == nil || dcID <= 0 {
		return
	}
	p.mu.Lock()
	invoker, ok := p.invokers[dcID]
	if ok {
		delete(p.invokers, dcID)
	}
	p.mu.Unlock()
	if ok && invoker != nil {
		_ = invoker.Close()
	}
}

func (p *telegramTransferPool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	invokers := make([]telegram.CloseInvoker, 0, len(p.invokers))
	for _, invoker := range p.invokers {
		invokers = append(invokers, invoker)
	}
	p.invokers = map[int]telegram.CloseInvoker{}
	p.mu.Unlock()

	var err error
	for _, invoker := range invokers {
		err = multierr.Append(err, invoker.Close())
	}
	return err
}

func telegramTransferPoolFromCtx(ctx context.Context, api *tg.Client) *telegramTransferPool {
	pools, ok := telegramTransferPoolsFromCtx(ctx)
	if !ok || pools == nil {
		return nil
	}
	return pools.Get(api)
}

func invalidateMediaClient(ctx context.Context, api *tg.Client, dcID int) {
	pool := telegramTransferPoolFromCtx(ctx, api)
	if pool == nil {
		return
	}
	pool.Invalidate(dcID)
}

func mediaDownloadClient(ctx context.Context, fallback *tg.Client, dcID int, threads int) (*tg.Client, int) {
	_ = threads
	if fallback == nil {
		return fallback, 0
	}
	pool := telegramTransferPoolFromCtx(ctx, fallback)
	if pool == nil {
		return fallback, 0
	}
	client, usedDC, err := pool.Client(ctx, dcID)
	if err != nil {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("Telegram DC 下载连接失败: dc=%d err=%v (已回退默认连接)", dcID, err))
		return fallback, 0
	}
	return client, usedDC
}

func mediaUploadClient(ctx context.Context, fallback *tg.Client, threads int) (*tg.Client, int) {
	_ = threads
	if fallback == nil {
		return fallback, 0
	}
	pool := telegramTransferPoolFromCtx(ctx, fallback)
	if pool == nil {
		return fallback, 0
	}
	client, usedDC, err := pool.Default(ctx)
	if err != nil {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("Telegram DC 上传连接失败: dc=current err=%v (已回退默认连接)", err))
		return fallback, 0
	}
	return client, usedDC
}

func transferSpeedMBps(bytes int64, elapsed time.Duration) float64 {
	if bytes <= 0 || elapsed <= 0 {
		return 0
	}
	return float64(bytes) / 1024.0 / 1024.0 / elapsed.Seconds()
}

func formatTransferStats(bytes int64, elapsed time.Duration) string {
	return fmt.Sprintf("%.1fMB, %.1fs, %.1fMB/s", float64(bytes)/1024.0/1024.0, elapsed.Seconds(), transferSpeedMBps(bytes, elapsed))
}

func transferDCLabel(dcID int) string {
	if dcID > 0 {
		return fmt.Sprintf("dc=%d", dcID)
	}
	return "dc=default"
}
