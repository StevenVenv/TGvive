package engine

import (
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

const (
	telegramDownloadMaxPartSize = 1024 * 1024
	telegramUploadMaxPartSize   = uploader.MaximumPartSize
	telegramTransferMaxThreads  = 4
)

var telegramTransferThreadLevels = []struct {
	threads int
	size    int64
}{
	{threads: 1, size: 1 << 20},
	{threads: 2, size: 5 << 20},
	{threads: 4, size: 20 << 20},
}

func newTelegramMediaDownloader() *downloader.Downloader {
	return downloader.NewDownloader().WithPartSize(telegramDownloadMaxPartSize)
}

func newTelegramMediaUploader(api *tg.Client) *uploader.Uploader {
	return uploader.NewUploader(api).WithPartSize(telegramUploadMaxPartSize)
}

func bestTelegramTransferThreads(size int64) int {
	return bestTelegramTransferThreadsWithMax(size, telegramTransferMaxThreads)
}

func bestTelegramTransferThreadsWithMax(size int64, max int) int {
	if max <= 0 {
		return 1
	}
	if size <= 0 {
		return max
	}
	for _, level := range telegramTransferThreadLevels {
		if size < level.size {
			return minTransferThreads(level.threads, max)
		}
	}
	return max
}

func minTransferThreads(a, b int) int {
	if a < b {
		return a
	}
	return b
}
