package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/multierr"
)

const (
	telegramDownloadMaxPartSize = 1024 * 1024
	telegramUploadMaxPartSize   = uploader.MaximumPartSize
	telegramTransferThreads     = 4
	telegramDownloadVerify      = false
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

func (p *telegramTransferPool) Default(ctx context.Context) (*tg.Client, int, error) {
	if p == nil {
		return nil, 0, fmt.Errorf("telegram transfer pool is nil")
	}
	return p.Client(ctx, p.currentDC())
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
