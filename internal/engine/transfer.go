package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"my-go-server/internal/global"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

type uploadByteProgress struct {
	last int64
}

func (p *uploadByteProgress) Chunk(ctx context.Context, st uploader.ProgressState) error {
	_ = ctx
	cur := st.Uploaded
	if cur <= 0 {
		return nil
	}
	for {
		prev := atomic.LoadInt64(&p.last)
		if cur <= prev {
			return nil
		}
		if atomic.CompareAndSwapInt64(&p.last, prev, cur) {
			delta := cur - prev
			if delta > 0 {
				global.AddUploadBytes(uint64(delta))
			}
			return nil
		}
	}
}

// DownloadFile downloads Photo/Document media from msg into a local file and returns (path, meta, cleanup).
// The file is stored under tmpMediaRoot.
func (m *TaskManager) DownloadFile(ctx context.Context, api *tg.Client, msg *tg.Message, taskID uint) (localPath string, meta mediaMeta, cleanup func() error, err error) {
	_ = m
	return downloadMessageMedia(ctx, api, msg, taskID)
}

// DownloadFileWithPeer downloads Photo/Document media from msg into a local file and returns (path, meta, cleanup).
// It uses sourcePeer for channel file-reference refresh on transient LOCATION_INVALID / FILE_REFERENCE_EXPIRED errors.
func (m *TaskManager) DownloadFileWithPeer(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, taskID uint) (localPath string, meta mediaMeta, cleanup func() error, err error) {
	_ = m
	return downloadMessageMediaWithPeer(ctx, api, sourcePeer, msg, taskID)
}

// UploadFile uploads localPath to Telegram and returns the InputFile.
func (m *TaskManager) UploadFile(ctx context.Context, api *tg.Client, localPath string) (tg.InputFileClass, error) {
	_ = m
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return nil, errors.New("empty local path")
	}

	var size int64
	if fi, err := os.Stat(localPath); err == nil && fi != nil {
		size = fi.Size()
	}

	threads := bestTelegramTransferThreads(size)
	uploadAPI, uploadDC := mediaUploadClient(ctx, api, threads)
	started := time.Now()
	progress := &uploadByteProgress{}
	inputFile, err := newTelegramMediaUploader(uploadAPI).
		WithThreads(threads).
		WithProgress(progress).
		FromPath(ctx, localPath)
	if err != nil {
		return nil, err
	}

	if size > 0 {
		uploaded := atomic.LoadInt64(&progress.last)
		if uploaded <= 0 {
			uploaded = size
		}
		stats := formatTransferStats(uploaded, time.Since(started))
		dcLabel := transferDCLabel(uploadDC)
		global.BroadcastLog(fmt.Sprintf("Uploaded %s (%s, %s, threads=%d)", filepath.Base(localPath), stats, dcLabel, threads))
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("上传完成: %s (%s, %s, threads=%d)", filepath.Base(localPath), stats, dcLabel, threads))
	}
	return inputFile, nil
}

// TransferMedia downloads Photo/Document media from msg into a temporary local file,
// uploads it back to Telegram, and returns the uploaded InputFile for sending.
//
// Deprecated: kept for backward-compatibility, prefer DownloadFile + UploadFile.
func (m *TaskManager) TransferMedia(ctx context.Context, client *telegram.Client, msg *tg.Message) (tg.InputFileClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("telegram client is nil")
	}

	path, _, cleanup, err := m.DownloadFile(ctx, client.API(), msg, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cleanup() }()

	inputFile, err := m.UploadFile(ctx, client.API(), path)
	if err != nil {
		return nil, fmt.Errorf("upload media from %q: %w", path, err)
	}

	return inputFile, nil
}
