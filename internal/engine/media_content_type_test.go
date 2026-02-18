package engine

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestDetectContentType_WebPageTypeVideo(t *testing.T) {
	m := &TaskManager{}

	msg := &tg.Message{
		Message: "https://example.com/video",
		Media: &tg.MessageMediaWebPage{
			Webpage: &tg.WebPage{Type: "video"},
		},
	}

	if got := m.DetectContentType(msg); got != "video" {
		t.Fatalf("expected content type %q, got %q", "video", got)
	}
}

func TestDetectContentType_WebPageTypePhoto(t *testing.T) {
	m := &TaskManager{}

	msg := &tg.Message{
		Message: "https://example.com/photo",
		Media: &tg.MessageMediaWebPage{
			Webpage: &tg.WebPage{Type: "photo"},
		},
	}

	if got := m.DetectContentType(msg); got != "image" {
		t.Fatalf("expected content type %q, got %q", "image", got)
	}
}

func TestDetectContentType_WebPageDocumentVideo(t *testing.T) {
	m := &TaskManager{}

	doc := &tg.Document{
		ID:       1,
		MimeType: "video/mp4",
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeVideo{},
		},
	}

	msg := &tg.Message{
		Message: "https://example.com/x",
		Media: &tg.MessageMediaWebPage{
			Webpage: &tg.WebPage{Document: doc},
		},
	}

	if got := m.DetectContentType(msg); got != "video" {
		t.Fatalf("expected content type %q, got %q", "video", got)
	}
}

func TestDetectContentType_DocumentFilenameWmv(t *testing.T) {
	m := &TaskManager{}

	msg := &tg.Message{
		Media: &tg.MessageMediaDocument{
			Document: &tg.Document{
				ID:       2,
				MimeType: "application/octet-stream",
				Attributes: []tg.DocumentAttributeClass{
					&tg.DocumentAttributeFilename{FileName: "clip.wmv"},
				},
			},
		},
	}

	if got := m.DetectContentType(msg); got != "video" {
		t.Fatalf("expected content type %q, got %q", "video", got)
	}
}

func TestQuotaSendableCount_UnsupportedMediaWithText(t *testing.T) {
	m := &TaskManager{}

	msg := &tg.Message{
		Message: "https://example.com/video",
		Media: &tg.MessageMediaWebPage{
			Webpage: &tg.WebPage{Type: "video"},
		},
	}

	allowed := map[string]struct{}{"video": {}}
	if got := quotaSendableCount(m, msg, allowed); got != 1 {
		t.Fatalf("expected sendable count %d, got %d", 1, got)
	}
}
