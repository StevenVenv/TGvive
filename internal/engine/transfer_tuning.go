package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
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

func mediaDownloadClient(ctx context.Context, fallback *tg.Client, dcID int, threads int) (*tg.Client, func(), int) {
	if fallback == nil || dcID <= 0 {
		return fallback, func() {}, 0
	}
	client, ok := telegramClientFromCtx(ctx)
	if !ok || client == nil {
		return fallback, func() {}, 0
	}
	if threads <= 0 {
		threads = telegramTransferThreads
	}

	invoker, err := client.MediaOnly(ctx, dcID, int64(threads))
	if err != nil {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("媒体 DC 下载连接失败: dc=%d err=%v (已回退默认连接)", dcID, err))
		return fallback, func() {}, 0
	}

	return tg.NewClient(invoker), func() { _ = invoker.Close() }, dcID
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
