package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// WrapUploadedMedia 将上传后的文件封装为可发送的媒体对象（CloneMode=3）。
// 使用原始消息的元数据（如 Attributes / Spoiler / TTLSeconds）来尽量还原显示效果。
func (m *TaskManager) WrapUploadedMedia(ctx context.Context, api *tg.Client, inputFile tg.InputFileClass, originalMsg *tg.Message) (tg.InputMediaClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, fmt.Errorf("tg api is nil")
	}
	if inputFile == nil || originalMsg == nil || originalMsg.Media == nil {
		return nil, nil
	}

	switch media := originalMsg.Media.(type) {
	case *tg.MessageMediaPhoto:
		return &tg.InputMediaUploadedPhoto{
			File:       inputFile,
			Spoiler:    media.Spoiler,
			TTLSeconds: media.TTLSeconds,
		}, nil

	case *tg.MessageMediaDocument:
		if media.Document == nil {
			return nil, nil
		}
		doc, ok := media.Document.AsNotEmpty()
		if !ok || doc == nil {
			return nil, nil
		}

		attrs := make([]tg.DocumentAttributeClass, len(doc.Attributes))
		copy(attrs, doc.Attributes)

		if !hasFilenameAttr(attrs) {
			name := ""
			if v, ok := findDocumentFilename(attrs); ok {
				name = v
			}
			if strings.TrimSpace(name) == "" {
				name = fmt.Sprintf("doc_%d.bin", doc.ID)
			}
			attrs = append(attrs, &tg.DocumentAttributeFilename{FileName: sanitizeFilename(name)})
		}

		mime := strings.TrimSpace(doc.MimeType)
		if mime == "" {
			mime = "application/octet-stream"
		}

		out := &tg.InputMediaUploadedDocument{
			File:       inputFile,
			MimeType:   mime,
			Attributes: attrs,
			ForceFile:  false,
			Spoiler:    media.Spoiler,
			TTLSeconds: media.TTLSeconds,
		}

		if thumb := m.transferDocumentThumb(ctx, api, doc); thumb != nil {
			out.Thumb = thumb
		}

		if media.VideoCover != nil {
			if cover, ok := media.VideoCover.AsNotEmpty(); ok && cover != nil {
				out.VideoCover = cover.AsInput()
			}
		}
		if media.VideoTimestamp != 0 {
			out.VideoTimestamp = media.VideoTimestamp
		}

		return out, nil
	}

	return nil, nil
}

// transferDocumentThumb downloads the best available document thumbnail and uploads it as InputFile.
// If anything fails, it returns nil (best-effort, should not block main media sending).
func (m *TaskManager) transferDocumentThumb(ctx context.Context, api *tg.Client, doc *tg.Document) tg.InputFileClass {
	_ = m
	if err := ctx.Err(); err != nil {
		return nil
	}
	if api == nil || doc == nil || len(doc.Thumbs) == 0 {
		return nil
	}

	thumbType, ok := bestDocThumbType(doc.Thumbs)
	if !ok {
		return nil
	}

	loc := &tg.InputDocumentFileLocation{
		ID:            doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
		ThumbSize:     thumbType,
	}

	if err := os.MkdirAll(tmpMediaRoot, 0o700); err != nil {
		return nil
	}

	f, err := os.CreateTemp(tmpMediaRoot, "tgthumb_*.jpg")
	if err != nil {
		return nil
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	defer func() { _ = f.Close() }()

	dl := downloader.NewDownloader()
	if _, err := dl.Download(api, loc).
		WithThreads(2).
		WithVerify(true).
		Parallel(ctx, f); err != nil {
		return nil
	}
	if err := f.Close(); err != nil {
		return nil
	}

	up := uploader.NewUploader(api).WithThreads(2)
	inputFile, err := up.FromPath(ctx, path)
	if err != nil {
		return nil
	}

	return inputFile
}

func bestDocThumbType(thumbs []tg.PhotoSizeClass) (string, bool) {
	bestType := ""
	bestArea := -1

	for _, t := range thumbs {
		switch v := t.(type) {
		case *tg.PhotoSize:
			area := v.W * v.H
			if area > bestArea {
				bestArea = area
				bestType = v.Type
			}
		case *tg.PhotoCachedSize:
			area := v.W * v.H
			if area > bestArea {
				bestArea = area
				bestType = v.Type
			}
		case *tg.PhotoSizeProgressive:
			area := v.W * v.H
			if area > bestArea {
				bestArea = area
				bestType = v.Type
			}
		}
	}

	bestType = strings.TrimSpace(bestType)
	if bestType != "" {
		return bestType, true
	}

	// Fallback: use any non-empty type.
	for i := len(thumbs) - 1; i >= 0; i-- {
		if thumbs[i] == nil {
			continue
		}
		if t := strings.TrimSpace(thumbs[i].GetType()); t != "" {
			return t, true
		}
	}

	return "", false
}
