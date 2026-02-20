package engine

import (
	"bytes"
	"context"
	"errors"

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
	rule, enabled, _ := resolveWatermarkRule(st)
	return rule, enabled
}
