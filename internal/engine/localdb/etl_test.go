package localdb

import (
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func TestWashMessage_TextOnly(t *testing.T) {
	msg := &tg.Message{Message: "hello"}
	msg.SetFromID(&tg.PeerUser{UserID: 123})

	p, err := WashMessage(msg, func(*tg.Message) string { return "text" })
	if err != nil {
		t.Fatalf("WashMessage error: %v", err)
	}
	if p == nil {
		t.Fatalf("expected payload")
	}
	if p.SenderType != "user" || p.SenderID != 123 {
		t.Fatalf("sender mismatch: %+v", p)
	}
	if p.MediaType != "text" {
		t.Fatalf("media_type mismatch: got=%q", p.MediaType)
	}
	if p.Text != "hello" {
		t.Fatalf("text mismatch: got=%q", p.Text)
	}
	if len(p.MediaBytes) != 0 {
		t.Fatalf("expected no media_bytes for text-only")
	}
}

func TestWashMessage_DocumentMediaBytesDecodable(t *testing.T) {
	doc := &tg.Document{
		ID:            1,
		AccessHash:    2,
		FileReference: []byte{1, 2, 3},
		MimeType:      "application/octet-stream",
		Size:          10,
	}
	msg := &tg.Message{Message: "cap"}
	msg.SetFromID(&tg.PeerChannel{ChannelID: 999})
	msg.Media = &tg.MessageMediaDocument{Document: doc}

	p, err := WashMessage(msg, func(*tg.Message) string { return "file" })
	if err != nil {
		t.Fatalf("WashMessage error: %v", err)
	}
	if p == nil {
		t.Fatalf("expected payload")
	}
	if p.SenderType != "channel" || p.SenderID != 999 {
		t.Fatalf("sender mismatch: %+v", p)
	}
	if p.MediaType != "file" {
		t.Fatalf("media_type mismatch: got=%q", p.MediaType)
	}
	if len(p.MediaBytes) == 0 {
		t.Fatalf("expected media_bytes")
	}

	im, err := tg.DecodeInputMedia(&bin.Buffer{Buf: p.MediaBytes})
	if err != nil || im == nil {
		t.Fatalf("DecodeInputMedia failed: %v", err)
	}
	if _, ok := im.(*tg.InputMediaDocument); !ok {
		t.Fatalf("expected InputMediaDocument, got %T", im)
	}
}

func TestWashMessage_FallbackSenderFromPeerID(t *testing.T) {
	msg := &tg.Message{Message: "hi", PeerID: &tg.PeerChannel{ChannelID: 777}}

	p, err := WashMessage(msg, func(*tg.Message) string { return "text" })
	if err != nil {
		t.Fatalf("WashMessage error: %v", err)
	}
	if p == nil {
		t.Fatalf("expected payload")
	}
	if p.SenderType != "channel" || p.SenderID != 777 {
		t.Fatalf("sender mismatch: %+v", p)
	}
}
