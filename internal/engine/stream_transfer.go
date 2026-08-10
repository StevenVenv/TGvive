package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"

	"my-go-server/internal/global"

	"github.com/gotd/td/tg"
)

type countingWriter struct {
	dst     io.Writer
	onWrite func(n int)
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.dst.Write(p)
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
	return n, err
}

// UploadFromReader uploads a streamed payload to Telegram without loading it into memory.
// Size is used only for transfer tuning; the stream still controls the actual payload.
func (m *TaskManager) UploadFromReader(ctx context.Context, api *tg.Client, name string, r io.Reader, size int64) (tg.InputFileClass, error) {
	_ = m
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "stream.bin"
	}
	if r == nil {
		return nil, errors.New("reader is nil")
	}

	progress := &uploadByteProgress{}
	inputFile, err := newTelegramMediaUploader(api).
		WithThreads(bestTelegramTransferThreads(size)).
		WithProgress(progress).
		FromReader(ctx, name, r)
	if err != nil {
		return nil, err
	}

	if up := atomic.LoadInt64(&progress.last); up > 0 {
		global.BroadcastLog(fmt.Sprintf("Uploaded %s (%.1fMB)", filepath.Base(name), float64(up)/1024.0/1024.0))
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("上传完成: %s (%.1fMB)", filepath.Base(name), float64(up)/1024.0/1024.0))
	}

	return inputFile, nil
}

func streamDownloadOnce(ctx context.Context, api *tg.Client, loc tg.InputFileLocationClass, w io.Writer, verify bool, size int64) error {
	dl := newTelegramMediaDownloader()
	_, err := dl.Download(api, loc).
		WithThreads(bestTelegramTransferThreads(size)).
		WithVerify(verify).
		Stream(ctx, w)
	return err
}

// TransferMediaStream streams message media from Telegram download to Telegram upload without touching disk.
// It is only safe when no local mutations are needed (no watermark / no processors / no MD5 change).
func (m *TaskManager) TransferMediaStream(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message) (tg.InputFileClass, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if api == nil {
		return nil, "", errors.New("tg api is nil")
	}
	if msg == nil || msg.Media == nil {
		return nil, "", ErrUnsupportedMedia
	}

	spec, err := buildMediaDownloadSpec(msg)
	if err != nil {
		return nil, "", err
	}

	act := "流式传输"
	switch spec.meta.Kind {
	case mediaKindPhoto:
		act = "流式传输图片"
	case mediaKindDocument:
		act = "流式传输文件"
	}
	if msg.ID > 0 {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s (msg_id=%d)", act, spec.baseName, msg.ID))
	} else {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s", act, spec.baseName))
	}

	pr, pw := io.Pipe()
	downloadDone := make(chan error, 1)

	go func() {
		defer close(downloadDone)

		w := countingWriter{
			dst: pw,
			onWrite: func(n int) {
				if n > 0 {
					global.AddDownloadBytes(uint64(n))
				}
			},
		}

		err := streamDownloadOnce(ctx, api, spec.loc, w, true, spec.size)
		if err != nil && strings.Contains(err.Error(), "get hashes") {
			// Some locations may fail on upload.getFileHashes (verify path) but still be downloadable.
			err = streamDownloadOnce(ctx, api, spec.loc, w, false, spec.size)
		}

		// Attempt to refresh file reference once on transient file-location errors.
		if err != nil && sourcePeer != nil && msg.ID > 0 && isFileLocationRefreshable(err) {
			refreshed, rerr := refreshMessageForDownload(ctx, api, sourcePeer, msg.ID)
			if rerr == nil && refreshed != nil && refreshed.Media != nil {
				if rspec, rerr := buildMediaDownloadSpec(refreshed); rerr == nil {
					err = streamDownloadOnce(ctx, api, rspec.loc, w, true, rspec.size)
					if err != nil && strings.Contains(err.Error(), "get hashes") {
						err = streamDownloadOnce(ctx, api, rspec.loc, w, false, rspec.size)
					}
				}
			}
		}

		if err != nil {
			_ = pw.CloseWithError(err)
			downloadDone <- err
			return
		}
		_ = pw.Close()
		downloadDone <- nil
	}()

	inputFile, upErr := m.UploadFromReader(ctx, api, spec.baseName, pr, spec.size)
	_ = pr.Close()

	dlErr := <-downloadDone
	if upErr != nil {
		if dlErr != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrMediaDownload, dlErr)
		}
		return nil, "", upErr
	}
	if dlErr != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrMediaDownload, dlErr)
	}

	return inputFile, spec.baseName, nil
}
