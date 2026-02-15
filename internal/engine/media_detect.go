package engine

import (
	"strings"

	"github.com/gotd/td/tg"
)

func isVideoDocument(media *tg.MessageMediaDocument) bool {
	if media == nil {
		return false
	}
	if media.Video || media.Round {
		return true
	}
	if media.Document == nil {
		return false
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok || doc == nil {
		return false
	}
	for _, attr := range doc.Attributes {
		switch attr.(type) {
		case *tg.DocumentAttributeVideo, *tg.DocumentAttributeAnimated:
			return true
		}
	}
	return false
}

func isImageDocument(media *tg.MessageMediaDocument) bool {
	if media == nil || media.Document == nil {
		return false
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok || doc == nil {
		return false
	}
	mt := strings.ToLower(strings.TrimSpace(doc.MimeType))
	return strings.HasPrefix(mt, "image/")
}

func isStickerDocument(media *tg.MessageMediaDocument) bool {
	if media == nil || media.Document == nil {
		return false
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok || doc == nil {
		return false
	}
	for _, attr := range doc.Attributes {
		if _, ok := attr.(*tg.DocumentAttributeSticker); ok {
			return true
		}
	}
	return false
}

func documentHasThumbs(media *tg.MessageMediaDocument) bool {
	if media == nil || media.Document == nil {
		return false
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok || doc == nil {
		return false
	}
	return len(doc.Thumbs) > 0
}
