package engine

import (
	"fmt"
	"strings"

	"github.com/gotd/td/tg"
)

// WrapUploadedMedia 将上传后的文件封装为可发送的媒体对象（CloneMode=3）。
// 使用原始消息的元数据（如 Attributes / Spoiler / TTLSeconds）来尽量还原显示效果。
func (_ *TaskManager) WrapUploadedMedia(inputFile tg.InputFileClass, originalMsg *tg.Message) tg.InputMediaClass {
	if inputFile == nil || originalMsg == nil || originalMsg.Media == nil {
		return nil
	}

	switch media := originalMsg.Media.(type) {
	case *tg.MessageMediaPhoto:
		return &tg.InputMediaUploadedPhoto{
			File:       inputFile,
			Spoiler:    media.Spoiler,
			TTLSeconds: media.TTLSeconds,
		}

	case *tg.MessageMediaDocument:
		if media.Document == nil {
			return nil
		}
		doc, ok := media.Document.AsNotEmpty()
		if !ok || doc == nil {
			return nil
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

		if media.VideoCover != nil {
			if cover, ok := media.VideoCover.AsNotEmpty(); ok && cover != nil {
				out.VideoCover = cover.AsInput()
			}
		}
		if media.VideoTimestamp != 0 {
			out.VideoTimestamp = media.VideoTimestamp
		}

		return out
	}

	return nil
}
