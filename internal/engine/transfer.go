package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// DownloadFile downloads Photo/Document media from msg into a local file and returns (path, meta, cleanup).
// The file is stored under tmpMediaRoot.
func (m *TaskManager) DownloadFile(ctx context.Context, api *tg.Client, msg *tg.Message, taskID uint) (localPath string, meta mediaMeta, cleanup func() error, err error) {
	_ = m
	return downloadMessageMedia(ctx, api, msg, taskID)
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

	return uploader.NewUploader(api).WithThreads(4).FromPath(ctx, localPath)
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
