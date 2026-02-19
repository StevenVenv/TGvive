package localdb

import (
	"errors"
	"strings"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

type LightPayload struct {
	SenderType string `json:"sender_type"` // user|channel|unknown
	SenderID   int64  `json:"sender_id"`

	MediaType string `json:"media_type"` // text|image|video|audio|file|other
	Text      string `json:"text,omitempty"`

	MediaBytes []byte `json:"media_bytes,omitempty"` // TL-encoded tg.InputMediaClass
}

func WashMessage(msg *tg.Message, detectType func(*tg.Message) string) (*LightPayload, error) {
	if msg == nil {
		return nil, nil
	}

	p := &LightPayload{
		SenderType: "unknown",
		Text:       msg.Message,
	}

	if from, ok := msg.GetFromID(); ok && from != nil {
		switch v := from.(type) {
		case *tg.PeerUser:
			if v != nil {
				p.SenderType = "user"
				p.SenderID = v.UserID
			}
		case *tg.PeerChannel:
			if v != nil {
				p.SenderType = "channel"
				p.SenderID = v.ChannelID
			}
		}
	}

	mediaType := "other"
	if detectType != nil {
		mediaType = strings.ToLower(strings.TrimSpace(detectType(msg)))
	}
	if mediaType == "" {
		mediaType = "other"
	}
	p.MediaType = mediaType

	// Encode media reference (best-effort).
	if msg.Media != nil {
		inputMedia, err := convertMessageMediaToInput(msg.Media)
		if err != nil {
			if errors.Is(err, ErrUnsupportedMedia) {
				return p, nil
			}
			return nil, err
		}
		if inputMedia != nil {
			b := new(bin.Buffer)
			if err := inputMedia.Encode(b); err != nil {
				return nil, err
			}
			p.MediaBytes = append([]byte(nil), b.Buf...)
		}
	}

	return p, nil
}

var ErrUnsupportedMedia = errors.New("unsupported media")

func convertMessageMediaToInput(m tg.MessageMediaClass) (tg.InputMediaClass, error) {
	switch v := m.(type) {
	case *tg.MessageMediaPhoto:
		if v == nil || v.Photo == nil {
			return nil, ErrUnsupportedMedia
		}
		photo, ok := v.Photo.AsNotEmpty()
		if !ok {
			return nil, ErrUnsupportedMedia
		}
		return &tg.InputMediaPhoto{
			Spoiler:    v.Spoiler,
			ID:         photo.AsInput(),
			TTLSeconds: v.TTLSeconds,
		}, nil
	case *tg.MessageMediaDocument:
		if v == nil || v.Document == nil {
			return nil, ErrUnsupportedMedia
		}
		doc, ok := v.Document.AsNotEmpty()
		if !ok {
			return nil, ErrUnsupportedMedia
		}
		out := &tg.InputMediaDocument{
			Spoiler:    v.Spoiler,
			ID:         doc.AsInput(),
			TTLSeconds: v.TTLSeconds,
		}
		if v.VideoCover != nil {
			if cover, ok := v.VideoCover.AsNotEmpty(); ok {
				out.VideoCover = cover.AsInput()
			}
		}
		if v.VideoTimestamp != 0 {
			out.VideoTimestamp = v.VideoTimestamp
		}
		return out, nil
	default:
		return nil, ErrUnsupportedMedia
	}
}
