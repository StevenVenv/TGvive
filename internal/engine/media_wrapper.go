package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// WrapUploadedMedia 将上传后的文件封装为可发送的媒体对象（CloneMode=3）。
// 使用原始消息的元数据（如 Attributes / Spoiler / TTLSeconds）来尽量还原显示效果。
func (m *TaskManager) WrapUploadedMedia(ctx context.Context, api *tg.Client, inputFile tg.InputFileClass, originalMsg *tg.Message, randomFilename bool) (tg.InputMediaClass, error) {
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

		origName, _ := findDocumentFilename(doc.Attributes)
		randomName := ""
		if randomFilename {
			if name, err := randomizeFilename(origName, doc.MimeType); err == nil && strings.TrimSpace(name) != "" {
				randomName = name
			}
		}

		if randomName != "" {
			before := strings.TrimSpace(origName)
			if before == "" {
				before = "(empty)"
			}
			after := strings.TrimSpace(sanitizeFilename(randomName))
			if after == "" {
				after = "(empty)"
			}
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("随机文件名: %q -> %q (msg_id=%d)", before, after, originalMsg.ID))
		}

		attrs := make([]tg.DocumentAttributeClass, 0, len(doc.Attributes)+1)
		for _, a := range doc.Attributes {
			if _, ok := a.(*tg.DocumentAttributeFilename); ok && randomName != "" {
				continue
			}
			attrs = append(attrs, a)
		}
		if randomName != "" {
			attrs = append(attrs, &tg.DocumentAttributeFilename{FileName: sanitizeFilename(randomName)})
		}

		if randomName == "" && !hasFilenameAttr(attrs) {
			name := ""
			if v, ok := findDocumentFilename(attrs); ok {
				name = v
			}
			if strings.TrimSpace(name) == "" {
				name = fmt.Sprintf("doc_%d.bin", doc.ID)
			}
			safeName := sanitizeFilename(name)
			attrs = append(attrs, &tg.DocumentAttributeFilename{FileName: safeName})
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("补全文件名: %q (msg_id=%d)", safeName, originalMsg.ID))
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

func randomizeFilename(origName, mime string) (string, error) {
	ext := extFromOrigName(origName)
	if ext == "" {
		ext = extFromMime(mime)
	}
	if ext == "" {
		ext = ".bin"
	}

	b := make([]byte, 10) // 20 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "file_" + hex.EncodeToString(b) + ext, nil
}

func extFromOrigName(origName string) string {
	name := strings.TrimSpace(origName)
	if name == "" {
		return ""
	}
	ext1 := sanitizeExt(filepath.Ext(name))
	if ext1 == "" {
		return ""
	}
	stem := strings.TrimSuffix(strings.ToLower(name), ext1)
	ext2 := sanitizeExt(filepath.Ext(stem))
	if ext2 == "" {
		return ext1
	}

	// Preserve common compound extensions.
	if ext2 == ".tar" && isTarCompressedExt(ext1) {
		return ext2 + ext1
	}
	if isNumericExt(ext1) && (ext2 == ".7z" || ext2 == ".zip" || ext2 == ".rar") {
		return ext2 + ext1
	}

	return ext1
}

func isTarCompressedExt(ext string) bool {
	switch ext {
	case ".gz", ".bz2", ".xz", ".zst", ".br", ".lz", ".lz4", ".lzo":
		return true
	default:
		return false
	}
}

func isNumericExt(ext string) bool {
	if len(ext) < 2 || len(ext) > 8 || !strings.HasPrefix(ext, ".") {
		return false
	}
	for i := 1; i < len(ext); i++ {
		if ext[i] < '0' || ext[i] > '9' {
			return false
		}
	}
	return true
}

func sanitizeExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		return ""
	}
	if len(ext) > 32 {
		return ""
	}
	for i := 1; i < len(ext); i++ {
		ch := ext[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			continue
		}
		return ""
	}
	return strings.ToLower(ext)
}

func extFromMime(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/x-matroska":
		return ".mkv"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "application/x-7z-compressed":
		return ".7z"
	case "application/x-rar-compressed":
		return ".rar"
	default:
		return ""
	}
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
