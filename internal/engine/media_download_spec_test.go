package engine

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestBuildMediaDownloadSpecIncludesDocumentDC(t *testing.T) {
	msg := &tg.Message{
		Media: &tg.MessageMediaDocument{
			Document: &tg.Document{
				ID:            101,
				AccessHash:    202,
				FileReference: []byte{1, 2, 3},
				MimeType:      "video/mp4",
				Size:          4096,
				DCID:          4,
				Attributes: []tg.DocumentAttributeClass{
					&tg.DocumentAttributeFilename{FileName: "clip.mp4"},
				},
			},
		},
	}

	spec, err := buildMediaDownloadSpec(msg)
	if err != nil {
		t.Fatalf("buildMediaDownloadSpec returned error: %v", err)
	}
	if spec.dcID != 4 {
		t.Fatalf("dcID = %d, want 4", spec.dcID)
	}
	if spec.baseName != "clip.mp4" {
		t.Fatalf("baseName = %q, want clip.mp4", spec.baseName)
	}
}

func TestBuildMediaDownloadSpecIncludesPhotoDC(t *testing.T) {
	msg := &tg.Message{
		Media: &tg.MessageMediaPhoto{
			Photo: &tg.Photo{
				ID:            303,
				AccessHash:    404,
				FileReference: []byte{4, 5, 6},
				DCID:          5,
				Sizes: []tg.PhotoSizeClass{
					&tg.PhotoSize{Type: "m", W: 320, H: 240, Size: 2048},
				},
			},
		},
	}

	spec, err := buildMediaDownloadSpec(msg)
	if err != nil {
		t.Fatalf("buildMediaDownloadSpec returned error: %v", err)
	}
	if spec.dcID != 5 {
		t.Fatalf("dcID = %d, want 5", spec.dcID)
	}
	if spec.size != 2048 {
		t.Fatalf("size = %d, want 2048", spec.size)
	}
}
