package engine

import (
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

const (
	telegramDownloadMaxPartSize = 1024 * 1024
	telegramUploadMaxPartSize   = uploader.MaximumPartSize
	telegramTransferThreads     = 4
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
