package engine

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"my-go-server/internal/model"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

func isWatermarkableImageMessage(msg *tg.Message) bool {
	if msg == nil || msg.Media == nil {
		return false
	}
	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		return true
	case *tg.MessageMediaDocument:
		return isImageDocument(media) && !isStickerDocument(media)
	default:
		return false
	}
}

func messageSpoilerTTL(msg *tg.Message) (spoiler bool, ttl int) {
	if msg == nil || msg.Media == nil {
		return false, 0
	}
	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		return media.Spoiler, media.TTLSeconds
	case *tg.MessageMediaDocument:
		return media.Spoiler, media.TTLSeconds
	default:
		return false, 0
	}
}

func refreshMessagesByIDs(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, ids []int) (map[int]*tg.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if len(ids) == 0 {
		return nil, nil
	}

	uniq := make(map[int]struct{}, len(ids))
	list := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := uniq[id]; ok {
			continue
		}
		uniq[id] = struct{}{}
		list = append(list, id)
	}
	if len(list) == 0 {
		return nil, nil
	}
	sort.Ints(list)

	out := make(map[int]*tg.Message, len(list))
	const chunkSize = 100
	for start := 0; start < len(list); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + chunkSize
		if end > len(list) {
			end = len(list)
		}
		chunk := list[start:end]
		input := make([]tg.InputMessageClass, 0, len(chunk))
		for _, id := range chunk {
			input = append(input, &tg.InputMessageID{ID: id})
		}

		var (
			r   tg.MessagesMessagesClass
			err error
		)
		if ch, ok := sourcePeer.(*tg.InputPeerChannel); ok && ch != nil {
			r, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
				Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
				ID:      input,
			})
		} else {
			r, err = api.MessagesGetMessages(ctx, input)
		}
		if err != nil {
			return nil, err
		}

		msgs := extractTGMessages(r)
		for _, m := range msgs {
			if m == nil || m.ID <= 0 {
				continue
			}
			out[m.ID] = m
		}
	}

	return out, nil
}

func downloadMessageMediaBytes(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgID int) ([]byte, *tg.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if api == nil {
		return nil, nil, errors.New("tg api is nil")
	}
	if msgID <= 0 {
		return nil, nil, errors.New("msg_id is required")
	}

	msg, err := refreshMessageForDownload(ctx, api, sourcePeer, msgID)
	if err != nil {
		return nil, nil, err
	}
	if msg == nil || msg.Media == nil {
		return nil, nil, errors.New("message has no media")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		spec, err := buildMediaDownloadSpec(msg)
		if err != nil {
			return nil, nil, err
		}

		locs := []tg.InputFileLocationClass{spec.loc}
		if spec.meta.Kind == mediaKindPhoto && len(spec.photoThumb) > 1 {
			if photoLoc, ok := spec.loc.(*tg.InputPhotoFileLocation); ok && photoLoc != nil {
				for _, t := range spec.photoThumb[1:] {
					cp := *photoLoc
					cp.ThumbSize = t
					locs = append(locs, &cp)
				}
			}
		}

		dl := downloader.NewDownloader()
		for _, loc := range locs {
			var buf bytes.Buffer
			_, err := dl.Download(api, loc).
				WithThreads(2).
				WithVerify(true).
				Stream(ctx, &buf)
			if err == nil {
				if buf.Len() == 0 {
					return nil, nil, errors.New("download returned empty bytes")
				}
				return buf.Bytes(), msg, nil
			}

			lastErr = err
			if attempt == 0 && sourcePeer != nil && isFileLocationRefreshable(err) {
				if refreshed, rerr := refreshMessageForDownload(ctx, api, sourcePeer, msgID); rerr == nil && refreshed != nil {
					msg = refreshed
					break // rebuild spec+locs
				}
			}
		}
	}

	if lastErr != nil {
		return nil, nil, lastErr
	}
	return nil, nil, errors.New("download failed")
}

func uploadBytes(ctx context.Context, api *tg.Client, name string, b []byte) (tg.InputFileClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	progress := &uploadByteProgress{}
	return uploader.NewUploader(api).
		WithThreads(4).
		WithProgress(progress).
		FromBytes(ctx, name, b)
}

func watermarkRuleForTask(task model.Task) (model.WatermarkRule, bool) {
	st := ResolveRuntimeStrategy(task)
	rule, enabled, _ := resolveWatermarkRule(task, st)
	return rule, enabled
}
