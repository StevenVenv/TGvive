package engine

import (
	"context"
	"errors"
	"os"

	"my-go-server/internal/model"

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

func isWatermarkableVideoMessage(msg *tg.Message) bool {
	if msg == nil || msg.Media == nil {
		return false
	}
	switch media := msg.Media.(type) {
	case *tg.MessageMediaDocument:
		return isVideoDocument(media)
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

	localPath, _, cleanup, err := downloadMessageMediaWithPeer(ctx, api, sourcePeer, msg, 0)
	if err != nil {
		return nil, nil, err
	}
	if cleanup != nil {
		defer func() { _ = cleanup() }()
	}
	b, err := os.ReadFile(localPath)
	if err != nil {
		return nil, nil, err
	}
	if len(b) == 0 {
		return nil, nil, errors.New("download returned empty bytes")
	}
	return b, msg, nil
}

func uploadBytes(ctx context.Context, api *tg.Client, name string, b []byte) (tg.InputFileClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	threads := bestTelegramTransferThreads(int64(len(b)))
	uploadAPI, _ := mediaUploadClient(ctx, api, threads)
	progress := &uploadByteProgress{}
	return newTelegramMediaUploader(uploadAPI).
		WithThreads(threads).
		WithProgress(progress).
		FromBytes(ctx, name, b)
}

func watermarkRuleForTask(task model.Task) (model.WatermarkRule, bool) {
	st := ResolveRuntimeStrategy(task)
	rule, enabled, _ := resolveWatermarkRule(st)
	return rule, enabled
}

func videoWatermarkRuleForTask(task model.Task) (model.VideoWatermarkRule, bool) {
	if task.CloneMode != 3 {
		return model.VideoWatermarkRule{}, false
	}
	st := ResolveRuntimeStrategy(task)
	rule, enabled, _ := resolveVideoWatermarkRule(st)
	return rule, enabled
}
