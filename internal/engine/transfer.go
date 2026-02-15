package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// TransferMedia downloads Photo/Document media from msg into a temporary local file,
// uploads it back to Telegram, and returns the uploaded InputFile for sending.
//
// NOTE: caller should wrap returned file into:
//   - *tg.InputMediaUploadedPhoto for photos
//   - *tg.InputMediaUploadedDocument for documents
func (_ *TaskManager) TransferMedia(ctx context.Context, client *telegram.Client, msg *tg.Message) (tg.InputFileClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("telegram client is nil")
	}
	if msg == nil || msg.Media == nil {
		return nil, ErrUnsupportedMedia
	}

	loc, err := getLocation(msg.Media)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(tmpMediaRoot, 0o700); err != nil {
		return nil, err
	}

	pattern := "tgmedia_*"
	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		pattern = "tgphoto_*.jpg"
	case *tg.MessageMediaDocument:
		ext := bestDocExt(media)
		if ext != "" {
			pattern = "tgdoc_*" + ext
		} else {
			pattern = "tgdoc_*"
		}
	}

	f, err := os.CreateTemp(tmpMediaRoot, pattern)
	if err != nil {
		return nil, err
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	defer func() { _ = f.Close() }()

	d := downloader.NewDownloader()
	if _, err := d.Download(client.API(), loc).
		WithThreads(4).
		WithVerify(true).
		Parallel(ctx, f); err != nil {
		return nil, fmt.Errorf("download media to %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close temp file %q: %w", path, err)
	}

	up := uploader.NewUploader(client.API()).WithThreads(4)
	inputFile, err := up.FromPath(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("upload media from %q: %w", path, err)
	}

	return inputFile, nil
}

// getLocation extracts download location for Photo/Document.
func getLocation(media tg.MessageMediaClass) (tg.InputFileLocationClass, error) {
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		if m.Photo == nil {
			return nil, ErrUnsupportedMedia
		}
		photo, ok := m.Photo.AsNotEmpty()
		if !ok || photo == nil {
			return nil, ErrUnsupportedMedia
		}

		thumb, ok := bestPhotoThumbType(photo)
		if !ok {
			return nil, ErrUnsupportedMedia
		}

		return &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumb,
		}, nil
	case *tg.MessageMediaDocument:
		if m.Document == nil {
			return nil, ErrUnsupportedMedia
		}
		doc, ok := m.Document.AsNotEmpty()
		if !ok || doc == nil {
			return nil, ErrUnsupportedMedia
		}
		return doc.AsInputDocumentFileLocation(), nil
	default:
		return nil, ErrUnsupportedMedia
	}
}

func bestDocExt(media *tg.MessageMediaDocument) string {
	if media == nil || media.Document == nil {
		return ""
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok || doc == nil {
		return ""
	}
	if name, ok := findDocumentFilename(doc.Attributes); ok {
		ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
		if len(ext) <= 10 && strings.HasPrefix(ext, ".") {
			return ext
		}
	}
	return ""
}
