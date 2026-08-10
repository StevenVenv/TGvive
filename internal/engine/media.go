package engine

import (
	"context"
	"crypto/md5"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"my-go-server/internal/engine/processor"
	wm "my-go-server/internal/engine/watermark"
	"my-go-server/internal/global"
	"my-go-server/internal/model"
	"my-go-server/pkg/retry"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

var ErrUnsupportedMedia = errors.New("unsupported media")
var ErrMediaDownload = errors.New("media download failed")

const tmpMediaRoot = "./tmp/tgmedia"

const telegramMediaDownloadAttempts = 5

type countingWriterAt struct {
	dst     io.WriterAt
	onWrite func(n int)
}

func (c countingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	n, err := c.dst.WriteAt(p, off)
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
	return n, err
}

type mediaKind uint8

const (
	mediaKindUnknown mediaKind = iota
	mediaKindPhoto
	mediaKindDocument
)

type mediaMeta struct {
	Kind mediaKind

	Spoiler    bool
	TTLSeconds int

	MimeType   string
	Attributes []tg.DocumentAttributeClass
	Filename   string
}

const maxMD5DetailLogSize = 512 * 1024 * 1024 // 512MB

func randIntRange(min, max int) (int, error) {
	if min <= 0 || max < min {
		return 0, errors.New("invalid random range")
	}
	n := int64(max - min + 1)
	v, err := crand.Int(crand.Reader, big.NewInt(n))
	if err != nil {
		return 0, err
	}
	return min + int(v.Int64()), nil
}

func modifyFileMD5WithDetailLog(ctx context.Context, path string, label string, msgID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("empty file path")
	}

	beforeSize := int64(0)
	if fi, err := os.Stat(path); err == nil && fi != nil {
		beforeSize = fi.Size()
	}

	fmtSize := func(n int64) string {
		if n <= 0 {
			return fmt.Sprintf("%dB", n)
		}
		return fmt.Sprintf("%dB(%.1fMB)", n, float64(n)/1024.0/1024.0)
	}

	beforeMD5 := ""
	afterMD5 := ""
	md5Skipped := beforeSize > maxMD5DetailLogSize

	var h = md5.New()
	if !md5Skipped {
		if f, err := os.Open(path); err == nil && f != nil {
			if _, err := io.Copy(h, f); err == nil {
				beforeMD5 = hex.EncodeToString(h.Sum(nil))
			}
			_ = f.Close()
		}
	}

	if err := processor.ModifyFileMD5(path); err != nil {
		return err
	}

	afterSize := int64(0)
	if fi, err := os.Stat(path); err == nil && fi != nil {
		afterSize = fi.Size()
	}
	delta := afterSize - beforeSize

	if beforeMD5 != "" && delta > 0 && delta <= 1024 {
		if f, err := os.Open(path); err == nil && f != nil {
			if _, err := f.Seek(-delta, io.SeekEnd); err == nil {
				tail := make([]byte, delta)
				if _, err := io.ReadFull(f, tail); err == nil {
					_, _ = h.Write(tail)
					afterMD5 = hex.EncodeToString(h.Sum(nil))
				}
			}
			_ = f.Close()
		}
	}

	base := filepath.Base(path)
	if beforeMD5 != "" && afterMD5 != "" {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s md5=%s -> %s (size=%s -> %s, +%dB, msg_id=%d)", label, base, beforeMD5, afterMD5, fmtSize(beforeSize), fmtSize(afterSize), delta, msgID))
		return nil
	}

	if md5Skipped {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s (跳过MD5: 文件过大) size=%s -> %s (+%dB, msg_id=%d)", label, base, fmtSize(beforeSize), fmtSize(afterSize), delta, msgID))
		return nil
	}
	recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s size=%s -> %s (+%dB, msg_id=%d)", label, base, fmtSize(beforeSize), fmtSize(afterSize), delta, msgID))
	return nil
}

func modifyBytesMD5WithDetailLog(ctx context.Context, in []byte, label string, name string, msgID int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "bytes"
	}

	beforeSize := int64(len(in))
	fmtSize := func(n int64) string {
		if n <= 0 {
			return fmt.Sprintf("%dB", n)
		}
		return fmt.Sprintf("%dB(%.1fMB)", n, float64(n)/1024.0/1024.0)
	}

	beforeMD5 := ""
	afterMD5 := ""
	md5Skipped := beforeSize > maxMD5DetailLogSize

	if !md5Skipped && len(in) > 0 {
		sum := md5.Sum(in)
		beforeMD5 = hex.EncodeToString(sum[:])
	}

	nBytes, err := randIntRange(8, 32)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, nBytes)
	if _, err := crand.Read(buf); err != nil {
		return nil, err
	}
	tail := hex.EncodeToString(buf) // 16..64 bytes

	out := make([]byte, 0, len(in)+len(tail))
	out = append(out, in...)
	out = append(out, tail...)

	afterSize := int64(len(out))
	delta := afterSize - beforeSize

	if !md5Skipped && len(out) > 0 {
		sum := md5.Sum(out)
		afterMD5 = hex.EncodeToString(sum[:])
	}

	if beforeMD5 != "" && afterMD5 != "" {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s md5=%s -> %s (size=%s -> %s, +%dB, msg_id=%d)", label, name, beforeMD5, afterMD5, fmtSize(beforeSize), fmtSize(afterSize), delta, msgID))
		return out, nil
	}

	if md5Skipped {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s (跳过MD5: 文件过大) size=%s -> %s (+%dB, msg_id=%d)", label, name, fmtSize(beforeSize), fmtSize(afterSize), delta, msgID))
		return out, nil
	}
	recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: %s size=%s -> %s (+%dB, msg_id=%d)", label, name, fmtSize(beforeSize), fmtSize(afterSize), delta, msgID))
	return out, nil
}

func detectDocumentContentType(doc *tg.Document) string {
	if doc == nil {
		return "file"
	}

	for _, attr := range doc.Attributes {
		switch attr.(type) {
		case *tg.DocumentAttributeVideo, *tg.DocumentAttributeAnimated:
			return "video"
		case *tg.DocumentAttributeAudio:
			return "audio"
		}
	}

	mt := strings.ToLower(strings.TrimSpace(doc.MimeType))
	if strings.HasPrefix(mt, "image/") {
		return "image"
	}
	if strings.HasPrefix(mt, "video/") {
		return "video"
	}
	if strings.HasPrefix(mt, "audio/") {
		return "audio"
	}

	if name, ok := findDocumentFilename(doc.Attributes); ok {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mp4", ".mov", ".mkv", ".webm", ".avi", ".flv", ".m4v", ".3gp", ".3g2", ".wmv", ".mpeg", ".mpg", ".m2ts", ".mts", ".ts", ".vob", ".ogv", ".f4v", ".rm", ".rmvb":
			return "video"
		case ".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".opus", ".wma", ".amr":
			return "audio"
		}
	}

	return "file"
}

func detectWebPageContentType(msg *tg.Message, media *tg.MessageMediaWebPage) string {
	if msg == nil || media == nil {
		return "other"
	}

	switch wp := media.Webpage.(type) {
	case *tg.WebPage:
		if wp.Document != nil {
			if doc, ok := wp.Document.AsNotEmpty(); ok && doc != nil {
				if ct := detectDocumentContentType(doc); ct != "file" {
					return ct
				}
			}
		}

		switch strings.ToLower(strings.TrimSpace(wp.Type)) {
		case "video", "gif":
			return "video"
		case "photo":
			return "image"
		case "document":
			return "file"
		}

		et := strings.ToLower(strings.TrimSpace(wp.EmbedType))
		if strings.HasPrefix(et, "video/") {
			return "video"
		}
		if strings.HasPrefix(et, "image/") {
			return "image"
		}
		if strings.HasPrefix(et, "audio/") {
			return "audio"
		}

		if wp.Photo != nil {
			return "image"
		}
	}

	if strings.TrimSpace(msg.Message) != "" {
		return "text"
	}
	return "other"
}

// DetectContentType detects message content type for filtering.
// Returns: text|image|video|audio|file|other
func (m *TaskManager) DetectContentType(msg *tg.Message) string {
	if msg == nil {
		return "other"
	}
	if msg.Media == nil {
		if strings.TrimSpace(msg.Message) == "" {
			return "other"
		}
		return "text"
	}

	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		return "image"
	case *tg.MessageMediaDocument:
		if media.Voice {
			return "audio"
		}
		if media.Video || media.Round {
			return "video"
		}

		if media.Document != nil {
			if doc, ok := media.Document.AsNotEmpty(); ok && doc != nil {
				return detectDocumentContentType(doc)
			}
		}
		return "file"
	case *tg.MessageMediaWebPage:
		return detectWebPageContentType(msg, media)
	default:
		return "other"
	}
}

// SendText sends a plain message (CloneMode=2 helper).
func (m *TaskManager) SendText(ctx context.Context, api *tg.Client, msg *tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_, err := m.SendTextResult(ctx, api, msg, task, peer)
	return err
}

// SendMedia sends a single media message using InputMediaPhoto/InputMediaDocument (CloneMode=2).
func (m *TaskManager) SendMedia(ctx context.Context, api *tg.Client, msg *tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_, err := m.SendMediaResult(ctx, api, msg, task, peer)
	return err
}

// SendAlbum sends grouped media (CloneMode=2).
// Only the first item keeps caption/entities.
func (m *TaskManager) SendAlbum(ctx context.Context, api *tg.Client, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_, err := m.SendAlbumResult(ctx, api, msgs, task, peer)
	return err
}

// SendUploadedMedia sends a single media message by downloading to local disk and uploading back (CloneMode=3).
func (m *TaskManager) SendUploadedMedia(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_, err := m.sendUploadedMediaUpdates(ctx, api, sourcePeer, msg, task, peer)
	return err
}

func (m *TaskManager) sendUploadedMediaUpdates(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}
	if msg == nil || msg.Media == nil {
		return nil, nil
	}

	replyTo := buildKeepReplyInput(ctx, task, msg)

	enableMediaEdit := task.CloneMode == 3 && task.EnableMediaEdit
	procs := m.processors()

	wmRule, wmEnabled := watermarkRuleForTask(task)
	wmCandidate := wmEnabled && isWatermarkableImageMessage(msg)
	vidRule, vidEnabled := videoWatermarkRuleForTask(task)
	vidWmCandidate := vidEnabled && isWatermarkableVideoMessage(msg)

	localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, api, sourcePeer, msg, task.ID)
	if err != nil {
		if errors.Is(err, ErrMediaDownload) && isFileLocationRefreshable(err) {
			if upd, serr := sendMediaUpdates(ctx, api, peer, msg, replyTo); serr == nil {
				global.BroadcastLog(fmt.Sprintf("[WARN] Media download failed, fallback to send by reference (msg_id=%d)", msg.ID))
				storeMsgMapping(task, msg.ID, minPositiveInt(extractSentMsgIDs(upd)))
				return upd, nil
			}
		}
		return nil, err
	}
	if cleanup != nil {
		defer func() { _ = cleanup() }()
	}

	uploadPath := localPath
	var thumb tg.InputFileClass

	if vidWmCandidate {
		if procs.Video != nil && procs.Video.Enabled() {
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("应用视频水印: %s", filepath.Base(uploadPath)))
			if outPath, c, changed, err := procs.Video.WatermarkPath(ctx, uploadPath, vidRule); err != nil {
				if global.Logger != nil {
					global.Logger.Warn("video watermark failed, skipped", zap.Error(err))
				}
			} else if changed {
				uploadPath = outPath
				if c != nil {
					defer func() { _ = c() }()
				}
			}
		} else if global.Logger != nil {
			global.Logger.Warn("video watermark skipped (video processor disabled)")
		}
	}

	if enableMediaEdit {
		switch media := msg.Media.(type) {
		case *tg.MessageMediaPhoto:
			if procs.Image != nil && procs.Image.Enabled() {
				proc := procs.Image.ProcessPath
				if wmCandidate {
					proc = procs.Image.ProcessPathNoWatermark
				}
				if outPath, c, changed, err := proc(ctx, localPath); err != nil {
					if err != processor.ErrUnsupportedImage && global.Logger != nil {
						global.Logger.Warn("image processor failed, skipped", zap.Error(err))
					}
				} else if changed {
					uploadPath = outPath
					if c != nil {
						defer func() { _ = c() }()
					}
				}
			}
		case *tg.MessageMediaDocument:
			if isVideoDocument(media) && procs.Video != nil && procs.Video.Enabled() && !documentHasThumbs(media) {
				dir := filepath.Dir(localPath)
				f, err := os.CreateTemp(dir, "tgcover_*.jpg")
				if err == nil {
					coverPath := f.Name()
					_ = f.Close()
					defer func() { _ = os.Remove(coverPath) }()

					if ok, err := procs.Video.ExtractCover(ctx, uploadPath, coverPath); err != nil {
						if global.Logger != nil {
							global.Logger.Warn("extract video cover failed, skipped", zap.Error(err))
						}
					} else if ok {
						coverUploadPath := coverPath
						if procs.CoverImage != nil && procs.CoverImage.Enabled() {
							if outPath, c, changed, err := procs.CoverImage.ProcessPath(ctx, coverPath); err != nil {
								if err != processor.ErrUnsupportedImage && global.Logger != nil {
									global.Logger.Warn("cover image processor failed, skipped", zap.Error(err))
								}
							} else if changed {
								coverUploadPath = outPath
								if c != nil {
									defer func() { _ = c() }()
								}
							}
						}

						if task.ChangeMD5 {
							if err := modifyFileMD5WithDetailLog(ctx, coverUploadPath, "修改MD5(封面)", msg.ID); err != nil {
								return nil, err
							}
						}
						if inputThumb, err := m.UploadFile(ctx, api, coverUploadPath); err != nil {
							if global.Logger != nil {
								global.Logger.Warn("upload video cover failed, skipped", zap.Error(err))
							}
						} else {
							thumb = inputThumb
						}
					}
				}
			}

			if isImageDocument(media) && !isStickerDocument(media) && procs.Image != nil && procs.Image.Enabled() {
				proc := procs.Image.ProcessPath
				if wmCandidate {
					proc = procs.Image.ProcessPathNoWatermark
				}
				if outPath, c, changed, err := proc(ctx, localPath); err != nil {
					if err != processor.ErrUnsupportedImage && global.Logger != nil {
						global.Logger.Warn("image processor failed, skipped", zap.Error(err))
					}
				} else if changed {
					uploadPath = outPath
					if c != nil {
						defer func() { _ = c() }()
					}
				}
			}
		}
	}

	if wmCandidate {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("应用水印: %s", filepath.Base(uploadPath)))
		if b, rerr := os.ReadFile(uploadPath); rerr == nil {
			if outBytes, werr := wm.ApplyWatermark(b, wmRule); werr == nil {
				wmName := fmt.Sprintf("wm_%d.jpg", msg.ID)
				if task.ChangeMD5 {
					updated, err := modifyBytesMD5WithDetailLog(ctx, outBytes, "修改MD5(水印)", wmName, msg.ID)
					if err != nil {
						return nil, err
					}
					outBytes = updated
				}
				if inputFile, uerr := uploadBytes(ctx, api, wmName, outBytes); uerr == nil && inputFile != nil {
					recordTaskDetailFromCtx(ctx, fmt.Sprintf("上传完成: %s (%.1fMB, msg_id=%d)", wmName, float64(len(outBytes))/1024.0/1024.0, msg.ID))
					spoiler, ttl := messageSpoilerTTL(msg)
					uploaded := &tg.InputMediaUploadedPhoto{
						File:       inputFile,
						Spoiler:    spoiler,
						TTLSeconds: ttl,
					}
					rid, err := randomID()
					if err != nil {
						return nil, err
					}

					caption := msg.Message
					entities := msg.Entities
					if out, truncated := sanitizeMediaCaptionText(caption); truncated {
						caption = out
						entities = nil
					}

					req := &tg.MessagesSendMediaRequest{
						Peer:     peer,
						ReplyTo:  replyTo,
						Media:    uploaded,
						Message:  caption,
						RandomID: rid,
					}
					if len(entities) > 0 {
						req.Entities = entities
					}

					upd, err := sendTelegramUpdatesWithRetry(ctx, "发送水印媒体", sendSubjectMsgID(msg.ID), func(callCtx context.Context) (tg.UpdatesClass, error) {
						return api.MessagesSendMedia(callCtx, req)
					})
					if err == nil {
						recordTaskDetailFromCtx(ctx, "水印发送完成")
						storeMsgMapping(task, msg.ID, minPositiveInt(extractSentMsgIDs(upd)))
						return upd, nil
					}
				}
			}
		}
	}

	if task.ChangeMD5 {
		if err := modifyFileMD5WithDetailLog(ctx, uploadPath, "修改MD5", msg.ID); err != nil {
			return nil, err
		}
	}
	inputFile, err := m.UploadFile(ctx, api, uploadPath)
	if err != nil {
		return nil, fmt.Errorf("upload file %q: %w", uploadPath, err)
	}

	uploaded, err := m.WrapUploadedMedia(ctx, api, inputFile, msg, task.ChangeMD5 && task.RandomFilename, uploadPath)
	if err != nil {
		return nil, err
	}
	if uploaded == nil {
		return nil, ErrUnsupportedMedia
	}
	if thumb != nil {
		if doc, ok := uploaded.(*tg.InputMediaUploadedDocument); ok {
			doc.Thumb = thumb
		}
	}
	rid, err := randomID()
	if err != nil {
		return nil, err
	}

	caption := msg.Message
	entities := msg.Entities
	if out, truncated := sanitizeMediaCaptionText(caption); truncated {
		caption = out
		entities = nil
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		ReplyTo:  replyTo,
		Media:    uploaded,
		Message:  caption,
		RandomID: rid,
	}
	if len(entities) > 0 {
		req.Entities = entities
	}

	upd, err := sendTelegramUpdatesWithRetry(ctx, "发送上传媒体", sendSubjectMsgID(msg.ID), func(callCtx context.Context) (tg.UpdatesClass, error) {
		return api.MessagesSendMedia(callCtx, req)
	})
	if err != nil {
		return nil, fmt.Errorf("send uploaded media failed (path=%q): %w", uploadPath, err)
	}
	storeMsgMapping(task, msg.ID, minPositiveInt(extractSentMsgIDs(upd)))

	return upd, nil
}

// SendUploadedAlbum sends grouped media by downloading and re-uploading (CloneMode=3).
// Only the first item keeps caption/entities.
func (m *TaskManager) SendUploadedAlbum(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_, err := m.sendUploadedAlbumUpdates(ctx, api, sourcePeer, msgs, task, peer)
	return err
}

func (m *TaskManager) sendUploadedAlbumUpdates(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}

	var mediaMsgs []*tg.Message
	for _, msg := range msgs {
		if msg == nil || msg.Media == nil {
			continue
		}
		if _, err := convertMessageMediaToInput(msg.Media); err != nil {
			continue
		}
		mediaMsgs = append(mediaMsgs, msg)
	}

	switch len(mediaMsgs) {
	case 0:
		return nil, nil
	case 1:
		return m.sendUploadedMediaUpdates(ctx, api, sourcePeer, mediaMsgs[0], task, peer)
	}

	sort.Slice(mediaMsgs, func(i, j int) bool {
		return mediaMsgs[i].ID < mediaMsgs[j].ID
	})

	replyCarrier := mediaMsgs[0]
	for _, mm := range mediaMsgs {
		if extractReplyToSourceMsgID(mm) > 0 {
			replyCarrier = mm
			break
		}
	}
	replyTo := buildKeepReplyInput(ctx, task, replyCarrier)

	ups := make([]tg.InputSingleMedia, 0, len(mediaMsgs))
	cleanups := make([]func() error, 0, len(mediaMsgs))
	localPaths := make([]string, 0, len(mediaMsgs))

	defer func() {
		for _, fn := range cleanups {
			if fn != nil {
				_ = fn()
			}
		}
	}()

	enableMediaEdit := task.CloneMode == 3 && task.EnableMediaEdit
	procs := m.processors()
	wmRule, wmEnabled := watermarkRuleForTask(task)
	vidRule, vidEnabled := videoWatermarkRuleForTask(task)

	for _, msg := range mediaMsgs {
		wmCandidate := wmEnabled && isWatermarkableImageMessage(msg)
		vidWmCandidate := vidEnabled && isWatermarkableVideoMessage(msg)

		localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, api, sourcePeer, msg, task.ID)
		if err != nil {
			if errors.Is(err, ErrMediaDownload) && isFileLocationRefreshable(err) {
				if upd, serr := sendAlbumUpdates(ctx, api, peer, mediaMsgs, replyTo); serr == nil {
					global.BroadcastLog(fmt.Sprintf("[WARN] Album download failed, fallback to send by reference (grouped_id=%d)", msg.GroupedID))
					storeMsgMappingsInOrder(task, mediaMsgs, extractSentMsgIDs(upd))
					return upd, nil
				}
			}
			return nil, err
		}
		cleanups = append(cleanups, cleanup)
		localPaths = append(localPaths, localPath)

		uploadPath := localPath
		var thumb tg.InputFileClass

		if vidWmCandidate {
			if procs.Video != nil && procs.Video.Enabled() {
				recordTaskDetailFromCtx(ctx, fmt.Sprintf("应用视频水印: %s", filepath.Base(uploadPath)))
				if outPath, c, changed, err := procs.Video.WatermarkPath(ctx, uploadPath, vidRule); err != nil {
					if global.Logger != nil {
						global.Logger.Warn("video watermark failed, skipped", zap.Error(err))
					}
				} else if changed {
					uploadPath = outPath
					if c != nil {
						cleanups = append(cleanups, c)
					}
				}
			} else if global.Logger != nil {
				global.Logger.Warn("video watermark skipped (video processor disabled)")
			}
		}

		if enableMediaEdit {
			switch media := msg.Media.(type) {
			case *tg.MessageMediaPhoto:
				if procs.Image != nil && procs.Image.Enabled() {
					proc := procs.Image.ProcessPath
					if wmCandidate {
						proc = procs.Image.ProcessPathNoWatermark
					}
					if outPath, c, changed, err := proc(ctx, localPath); err != nil {
						if err != processor.ErrUnsupportedImage && global.Logger != nil {
							global.Logger.Warn("image processor failed, skipped", zap.Error(err))
						}
					} else if changed {
						uploadPath = outPath
						if c != nil {
							cleanups = append(cleanups, c)
						}
					}
				}
			case *tg.MessageMediaDocument:
				if isVideoDocument(media) && procs.Video != nil && procs.Video.Enabled() && !documentHasThumbs(media) {
					dir := filepath.Dir(localPath)
					f, err := os.CreateTemp(dir, "tgcover_*.jpg")
					if err == nil {
						coverPath := f.Name()
						_ = f.Close()
						cleanups = append(cleanups, func() error { return os.Remove(coverPath) })

						if ok, err := procs.Video.ExtractCover(ctx, uploadPath, coverPath); err != nil {
							if global.Logger != nil {
								global.Logger.Warn("extract video cover failed, skipped", zap.Error(err))
							}
						} else if ok {
							coverUploadPath := coverPath
							if procs.CoverImage != nil && procs.CoverImage.Enabled() {
								if outPath, c, changed, err := procs.CoverImage.ProcessPath(ctx, coverPath); err != nil {
									if err != processor.ErrUnsupportedImage && global.Logger != nil {
										global.Logger.Warn("cover image processor failed, skipped", zap.Error(err))
									}
								} else if changed {
									coverUploadPath = outPath
									if c != nil {
										cleanups = append(cleanups, c)
									}
								}
							}

							if task.ChangeMD5 {
								if err := modifyFileMD5WithDetailLog(ctx, coverUploadPath, "修改MD5(封面)", msg.ID); err != nil {
									return nil, err
								}
							}
							if inputThumb, err := m.UploadFile(ctx, api, coverUploadPath); err != nil {
								if global.Logger != nil {
									global.Logger.Warn("upload video cover failed, skipped", zap.Error(err))
								}
							} else {
								thumb = inputThumb
							}
						}
					}
				}

				if isImageDocument(media) && !isStickerDocument(media) && procs.Image != nil && procs.Image.Enabled() {
					proc := procs.Image.ProcessPath
					if wmCandidate {
						proc = procs.Image.ProcessPathNoWatermark
					}
					if outPath, c, changed, err := proc(ctx, localPath); err != nil {
						if err != processor.ErrUnsupportedImage && global.Logger != nil {
							global.Logger.Warn("image processor failed, skipped", zap.Error(err))
						}
					} else if changed {
						uploadPath = outPath
						if c != nil {
							cleanups = append(cleanups, c)
						}
					}
				}
			}
		}

		if wmCandidate {
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("应用水印: %s", filepath.Base(uploadPath)))
			if b, rerr := os.ReadFile(uploadPath); rerr == nil {
				if outBytes, werr := wm.ApplyWatermark(b, wmRule); werr == nil {
					wmName := fmt.Sprintf("wm_%d.jpg", msg.ID)
					if task.ChangeMD5 {
						updated, err := modifyBytesMD5WithDetailLog(ctx, outBytes, "修改MD5(水印)", wmName, msg.ID)
						if err != nil {
							return nil, err
						}
						outBytes = updated
					}
					if inputFile, uerr := uploadBytes(ctx, api, wmName, outBytes); uerr == nil && inputFile != nil {
						recordTaskDetailFromCtx(ctx, fmt.Sprintf("上传完成: %s (%.1fMB, msg_id=%d)", wmName, float64(len(outBytes))/1024.0/1024.0, msg.ID))
						spoiler, ttl := messageSpoilerTTL(msg)
						uploaded := &tg.InputMediaUploadedPhoto{
							File:       inputFile,
							Spoiler:    spoiler,
							TTLSeconds: ttl,
						}
						if thumb != nil {
							// keep best-effort thumb (not applicable to uploaded photo)
							_ = thumb
						}
						inputMedia, err := uploadMediaForAlbum(ctx, api, peer, uploaded)
						if err == nil && inputMedia != nil {
							rid, err := randomID()
							if err != nil {
								return nil, err
							}
							ups = append(ups, tg.InputSingleMedia{
								Media:    inputMedia,
								RandomID: rid,
							})
							continue
						}
					}
				}
			}
		}

		if task.ChangeMD5 {
			if err := modifyFileMD5WithDetailLog(ctx, uploadPath, "修改MD5", msg.ID); err != nil {
				return nil, err
			}
		}
		inputFile, err := m.UploadFile(ctx, api, uploadPath)
		if err != nil {
			return nil, fmt.Errorf("upload file %q: %w", uploadPath, err)
		}

		uploaded, err := m.WrapUploadedMedia(ctx, api, inputFile, msg, task.ChangeMD5 && task.RandomFilename, uploadPath)
		if err != nil {
			return nil, err
		}
		if uploaded == nil {
			return nil, ErrUnsupportedMedia
		}
		if thumb != nil {
			if doc, ok := uploaded.(*tg.InputMediaUploadedDocument); ok {
				doc.Thumb = thumb
			}
		}
		inputMedia, err := uploadMediaForAlbum(ctx, api, peer, uploaded)
		if err != nil {
			return nil, fmt.Errorf("upload media for album failed (path=%q): %w", uploadPath, err)
		}

		rid, err := randomID()
		if err != nil {
			return nil, err
		}

		ups = append(ups, tg.InputSingleMedia{
			Media:    inputMedia,
			RandomID: rid,
		})
	}

	if len(ups) == 0 {
		return nil, nil
	}
	if len(ups) == 1 {
		return m.sendUploadedMediaUpdates(ctx, api, sourcePeer, mediaMsgs[0], task, peer)
	}

	// Caption/entities only on the first item.
	caption := mediaMsgs[0].Message
	entities := mediaMsgs[0].Entities
	if out, truncated := sanitizeMediaCaptionText(caption); truncated {
		caption = out
		entities = nil
	}
	ups[0].Message = caption
	if len(entities) > 0 {
		ups[0].Entities = entities
	}

	req := &tg.MessagesSendMultiMediaRequest{
		Peer:       peer,
		ReplyTo:    replyTo,
		MultiMedia: ups,
	}
	upd, err := sendTelegramUpdatesWithRetry(ctx, "发送上传专辑", sendSubjectAlbum(mediaMsgs[0].GroupedID, len(mediaMsgs)), func(callCtx context.Context) (tg.UpdatesClass, error) {
		return api.MessagesSendMultiMedia(callCtx, req)
	})
	if err != nil {
		return nil, fmt.Errorf("send uploaded album failed (paths=%v): %w", localPaths, err)
	}
	storeMsgMappingsInOrder(task, mediaMsgs, extractSentMsgIDs(upd))

	return upd, nil
}

func convertMessageMediaToInput(m tg.MessageMediaClass) (tg.InputMediaClass, error) {
	switch v := m.(type) {
	case *tg.MessageMediaPhoto:
		if v.Photo == nil {
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
		if v.Document == nil {
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

func downloadMessageMedia(ctx context.Context, api *tg.Client, msg *tg.Message, taskID uint) (localPath string, meta mediaMeta, cleanup func() error, err error) {
	return downloadMessageMediaWithPeer(ctx, api, nil, msg, taskID)
}

type mediaDownloadSpec struct {
	loc        tg.InputFileLocationClass
	baseName   string
	size       int64
	dcID       int
	meta       mediaMeta
	photoThumb []string
}

func buildMediaDownloadSpec(msg *tg.Message) (mediaDownloadSpec, error) {
	if msg == nil || msg.Media == nil {
		return mediaDownloadSpec{}, ErrUnsupportedMedia
	}

	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		if media.Photo == nil {
			return mediaDownloadSpec{}, ErrUnsupportedMedia
		}
		photo, ok := media.Photo.AsNotEmpty()
		if !ok {
			return mediaDownloadSpec{}, ErrUnsupportedMedia
		}

		thumbs := photoThumbCandidates(photo)
		if len(thumbs) == 0 {
			return mediaDownloadSpec{}, ErrUnsupportedMedia
		}
		thumb := thumbs[0]
		loc := &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumb,
		}

		return mediaDownloadSpec{
			loc:        loc,
			baseName:   fmt.Sprintf("photo_%d.jpg", photo.ID),
			size:       photoDownloadSize(photo, thumb),
			dcID:       photo.DCID,
			photoThumb: thumbs,
			meta: mediaMeta{
				Kind:       mediaKindPhoto,
				Spoiler:    media.Spoiler,
				TTLSeconds: media.TTLSeconds,
			},
		}, nil

	case *tg.MessageMediaDocument:
		if media.Document == nil {
			return mediaDownloadSpec{}, ErrUnsupportedMedia
		}
		doc, ok := media.Document.AsNotEmpty()
		if !ok {
			return mediaDownloadSpec{}, ErrUnsupportedMedia
		}

		baseName := ""
		if name, ok := findDocumentFilename(doc.Attributes); ok {
			baseName = sanitizeFilename(name)
		}
		if baseName == "" {
			baseName = fmt.Sprintf("doc_%d.bin", doc.ID)
		}

		attrs := make([]tg.DocumentAttributeClass, len(doc.Attributes))
		copy(attrs, doc.Attributes)

		return mediaDownloadSpec{
			loc:      doc.AsInputDocumentFileLocation(""),
			baseName: baseName,
			size:     doc.Size,
			dcID:     doc.DCID,
			meta: mediaMeta{
				Kind:       mediaKindDocument,
				Spoiler:    media.Spoiler,
				TTLSeconds: media.TTLSeconds,
				MimeType:   strings.TrimSpace(doc.MimeType),
				Attributes: attrs,
				Filename:   baseName,
			},
		}, nil
	default:
		return mediaDownloadSpec{}, ErrUnsupportedMedia
	}
}

func isFileLocationRefreshable(err error) bool {
	if err == nil {
		return false
	}
	return tgerr.Is(err, "LOCATION_INVALID") ||
		tgerr.Is(err, "FILE_REFERENCE_EXPIRED") ||
		tgerr.Is(err, "FILE_REFERENCE_EMPTY") ||
		tgerr.Is(err, "FILE_REFERENCE_INVALID")
}

type messageMediaKey struct {
	kind mediaKind
	id   int64
}

func getMessageMediaKey(msg *tg.Message) (messageMediaKey, bool) {
	if msg == nil || msg.Media == nil {
		return messageMediaKey{}, false
	}

	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		if media.Photo == nil {
			return messageMediaKey{}, false
		}
		photo, ok := media.Photo.AsNotEmpty()
		if !ok || photo == nil {
			return messageMediaKey{}, false
		}
		return messageMediaKey{kind: mediaKindPhoto, id: photo.ID}, true
	case *tg.MessageMediaDocument:
		if media.Document == nil {
			return messageMediaKey{}, false
		}
		doc, ok := media.Document.AsNotEmpty()
		if !ok || doc == nil {
			return messageMediaKey{}, false
		}
		return messageMediaKey{kind: mediaKindDocument, id: doc.ID}, true
	default:
		return messageMediaKey{}, false
	}
}

func refreshMessageForDownload(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msgID int) (*tg.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if msgID <= 0 {
		return nil, errors.New("message id is required")
	}

	// First try: direct getMessages (channels.getMessages for channels).
	{
		var r tg.MessagesMessagesClass
		var err error

		if ch, ok := sourcePeer.(*tg.InputPeerChannel); ok && ch != nil {
			r, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
				Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
				ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}},
			})
		} else {
			r, err = api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}})
		}
		if err == nil {
			msgs := extractTGMessages(r)
			if len(msgs) > 0 && msgs[0] != nil {
				return msgs[0], nil
			}
		}
	}

	// Fallback: refresh via getHistory (offset_id = msgID+1, limit=1 -> likely returns msgID).
	if sourcePeer != nil {
		r, err := getHistoryWithFloodWait(ctx, api, &tg.MessagesGetHistoryRequest{
			Peer:     sourcePeer,
			OffsetID: msgID + 1,
			Limit:    1,
		})
		if err != nil {
			return nil, err
		}
		msgs := extractTGMessages(r)
		for _, m := range msgs {
			if m != nil && m.ID == msgID {
				return m, nil
			}
		}
	}

	return nil, errors.New("refresh message returned empty result")
}

func refreshAlbumMessageForDownload(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, groupedID int64, target messageMediaKey, aroundMsgID int) (*tg.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if sourcePeer == nil {
		return nil, errors.New("source peer is nil")
	}
	if groupedID == 0 {
		return nil, errors.New("grouped id is required")
	}
	if aroundMsgID <= 0 {
		return nil, errors.New("around message id is required")
	}

	const window = 48
	limit := window*2 + 8
	if limit > 128 {
		limit = 128
	}
	reqs := []*tg.MessagesGetHistoryRequest{
		{
			Peer:      sourcePeer,
			OffsetID:  aroundMsgID,
			AddOffset: -window,
			Limit:     limit,
		},
		{
			Peer:      sourcePeer,
			OffsetID:  aroundMsgID + 1,
			AddOffset: -window,
			Limit:     limit,
		},
		{
			Peer:     sourcePeer,
			OffsetID: aroundMsgID + 1,
			Limit:    limit,
		},
	}

	for _, req := range reqs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r, err := getHistoryWithFloodWait(ctx, api, req)
		if err != nil {
			continue
		}
		msgs := extractTGMessages(r)
		var ids []tg.InputMessageClass
		for _, m := range msgs {
			if m == nil || m.Media == nil || m.GroupedID != groupedID {
				continue
			}
			ids = append(ids, &tg.InputMessageID{ID: m.ID})
			key, ok := getMessageMediaKey(m)
			if !ok {
				continue
			}
			if key.kind == target.kind && key.id == target.id {
				return m, nil
			}
		}

		// Second pass: bulk refresh album messages by IDs via getMessages to ensure newest file_reference.
		if len(ids) == 0 {
			continue
		}
		if ch, ok := sourcePeer.(*tg.InputPeerChannel); ok && ch != nil {
			r, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
				Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
				ID:      ids,
			})
		} else {
			r, err = api.MessagesGetMessages(ctx, ids)
		}
		if err != nil {
			continue
		}
		refreshed := extractTGMessages(r)
		for _, m := range refreshed {
			if m == nil || m.Media == nil || m.GroupedID != groupedID {
				continue
			}
			key, ok := getMessageMediaKey(m)
			if !ok {
				continue
			}
			if key.kind == target.kind && key.id == target.id {
				return m, nil
			}
		}
	}

	return nil, errors.New("album refresh returned empty result")
}

func downloadMessageMediaWithPeer(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, taskID uint) (localPath string, meta mediaMeta, cleanup func() error, err error) {
	if err := ctx.Err(); err != nil {
		return "", mediaMeta{}, nil, err
	}
	if api == nil {
		return "", mediaMeta{}, nil, errors.New("tg api is nil")
	}
	if msg == nil || msg.Media == nil {
		return "", mediaMeta{}, nil, ErrUnsupportedMedia
	}

	dir := filepath.Join(tmpMediaRoot, fmt.Sprintf("task_%d", taskID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", mediaMeta{}, nil, err
	}

	downloadAction := func(meta mediaMeta) string {
		if meta.Kind == mediaKindPhoto {
			return "下载图片"
		}
		if meta.Kind != mediaKindDocument {
			return "下载媒体"
		}

		for _, a := range meta.Attributes {
			switch a.(type) {
			case *tg.DocumentAttributeVideo, *tg.DocumentAttributeAnimated:
				return "下载视频"
			case *tg.DocumentAttributeAudio:
				return "下载音频"
			}
		}

		mt := strings.ToLower(strings.TrimSpace(meta.MimeType))
		switch {
		case strings.HasPrefix(mt, "video/"):
			return "下载视频"
		case strings.HasPrefix(mt, "image/"):
			return "下载图片"
		case strings.HasPrefix(mt, "audio/"):
			return "下载音频"
		default:
			return "下载文件"
		}
	}

	downloadOnce := func(cur *tg.Message, afterRefresh bool) (string, mediaMeta, func() error, error) {
		spec, err := buildMediaDownloadSpec(cur)
		if err != nil {
			return "", mediaMeta{}, nil, err
		}

		act := downloadAction(spec.meta)
		if cur != nil && cur.ID > 0 {
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s: msg_id=%d", act, cur.ID))
		} else {
			recordTaskDetailFromCtx(ctx, act)
		}
		threads := bestTelegramTransferThreads(spec.size)

		f, path, err := createUniqueFile(dir, sanitizeFilename(spec.baseName))
		if err != nil {
			return "", mediaMeta{}, nil, err
		}
		defer func() { _ = f.Close() }()

		resetFile := func() error {
			if err := f.Truncate(0); err != nil {
				return err
			}
			_, err := f.Seek(0, 0)
			return err
		}

		doDownload := func(loc tg.InputFileLocationClass, verify bool, downloadAPI *tg.Client) error {
			d := newTelegramMediaDownloader()
			_, err := d.Download(downloadAPI, loc).
				WithThreads(threads).
				WithVerify(verify).
				Parallel(ctx, countingWriterAt{
					dst: f,
					onWrite: func(n int) {
						global.AddDownloadBytes(uint64(n))
					},
				})
			return err
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

		var lastErr error
		var downloadDC int
		started := time.Now()
		for idx, loc := range locs {
			if idx > 0 {
				if err := resetFile(); err != nil {
					lastErr = err
					break
				}
			}

			var err error
			for attempt := 1; attempt <= telegramMediaDownloadAttempts; attempt++ {
				if attempt > 1 {
					if rerr := resetFile(); rerr != nil {
						err = rerr
						break
					}
				}

				downloadAPI, usedDC := mediaDownloadClient(ctx, api, spec.dcID, threads)
				downloadDC = usedDC
				err = doDownload(loc, telegramDownloadVerify, downloadAPI)
				if err != nil && telegramDownloadVerify && strings.Contains(err.Error(), "get hashes") {
					// Some media locations may fail on upload.getFileHashes (verify path) but still be downloadable.
					// If that happens, fallback to no-verify download once to improve success rate.
					if rerr := resetFile(); rerr == nil {
						err = doDownload(loc, false, downloadAPI)
					}
				}
				if err == nil {
					break
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !retry.IsRetryableNetErr(err) || attempt >= telegramMediaDownloadAttempts {
					break
				}

				if usedDC > 0 {
					invalidateMediaClient(ctx, api, usedDC)
				}
				wait := retry.WithJitter(retry.Backoff(attempt, 2*time.Second, 15*time.Second), 0.2)
				recordTaskDetailFromCtx(ctx, fmt.Sprintf("下载重试: %s attempt=%d/%d wait=%.1fs %s err=%v", spec.baseName, attempt+1, telegramMediaDownloadAttempts, wait.Seconds(), transferDCLabel(usedDC), err))
				if serr := retry.Sleep(ctx, wait); serr != nil {
					err = serr
					break
				}
			}

			if err == nil {
				lastErr = nil
				break
			}
			lastErr = err
		}
		if lastErr != nil {
			if spec.meta.Kind == mediaKindPhoto && len(spec.photoThumb) > 1 {
				lastErr = fmt.Errorf("download photo failed (thumbs=%v): %w", spec.photoThumb, lastErr)
			}
			_ = os.Remove(path)
			if afterRefresh {
				return "", mediaMeta{}, nil, fmt.Errorf("%w: download media to %q after refresh: %w", ErrMediaDownload, path, lastErr)
			}
			return "", mediaMeta{}, nil, fmt.Errorf("%w: download media to %q: %w", ErrMediaDownload, path, lastErr)
		}

		if fi, err := f.Stat(); err == nil && fi != nil {
			if sz := fi.Size(); sz > 0 {
				stats := formatTransferStats(sz, time.Since(started))
				dcLabel := transferDCLabel(downloadDC)
				global.BroadcastLog(fmt.Sprintf("Downloaded %s (%s, %s, threads=%d)", filepath.Base(path), stats, dcLabel, threads))
				recordTaskDetailFromCtx(ctx, fmt.Sprintf("%s完成: %s (%s, %s, threads=%d)", act, filepath.Base(path), stats, dcLabel, threads))
			}
		}

		return path, spec.meta, func() error { return os.Remove(path) }, nil
	}

	path, meta, cleanup, err := downloadOnce(msg, false)
	if err == nil {
		return path, meta, cleanup, nil
	}

	if sourcePeer == nil || !isFileLocationRefreshable(err) || msg.ID <= 0 {
		return "", mediaMeta{}, nil, err
	}

	global.BroadcastLog(fmt.Sprintf("[WARN] Media LOCATION_INVALID, refreshing file reference then retry (msg_id=%d)", msg.ID))
	recordTaskDetailFromCtx(ctx, fmt.Sprintf("文件引用失效，刷新后重试: msg_id=%d", msg.ID))
	refreshed, rerr := refreshMessageForDownload(ctx, api, sourcePeer, msg.ID)
	if rerr != nil || refreshed == nil {
		return "", mediaMeta{}, nil, err
	}
	if refreshed.Media == nil {
		return "", mediaMeta{}, nil, err
	}

	path, meta, cleanup, err = downloadOnce(refreshed, true)
	if err == nil {
		return path, meta, cleanup, nil
	}

	// Album special-case: refresh the whole grouped media window and match by concrete media ID.
	if msg.GroupedID != 0 && isFileLocationRefreshable(err) {
		if key, ok := getMessageMediaKey(msg); ok {
			global.BroadcastLog(fmt.Sprintf("[WARN] Media LOCATION_INVALID after refresh, refreshing album window then retry (msg_id=%d, grouped_id=%d)", msg.ID, msg.GroupedID))
			recordTaskDetailFromCtx(ctx, fmt.Sprintf("文件引用失效，刷新专辑窗口后重试: msg_id=%d grouped_id=%d", msg.ID, msg.GroupedID))
			if m2, aerr := refreshAlbumMessageForDownload(ctx, api, sourcePeer, msg.GroupedID, key, msg.ID); aerr == nil && m2 != nil && m2.Media != nil {
				return downloadOnce(m2, true)
			}
		}
	}

	return "", mediaMeta{}, nil, err
}

func uploadMediaForAlbum(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, uploaded tg.InputMediaClass) (tg.InputMediaClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}

	m, err := api.MessagesUploadMedia(ctx, &tg.MessagesUploadMediaRequest{
		Peer:  peer,
		Media: uploaded,
	})
	if err != nil {
		return nil, err
	}

	return convertMessageMediaToInput(m)
}

func randomID() (int64, error) {
	return crypto.RandInt64(crypto.DefaultRand())
}

func photoThumbCandidates(photo *tg.Photo) []string {
	if photo == nil || len(photo.Sizes) == 0 {
		return nil
	}

	// Pick download-able size types, skipping stripped/path-only constructors.
	bestByType := make(map[string]int)
	for _, s := range photo.Sizes {
		if s == nil {
			continue
		}

		var (
			t    string
			area int
		)
		switch v := s.(type) {
		case *tg.PhotoSize:
			t = v.Type
			area = v.W * v.H
		case *tg.PhotoCachedSize:
			t = v.Type
			area = v.W * v.H
		case *tg.PhotoSizeProgressive:
			t = v.Type
			area = v.W * v.H
		case *tg.PhotoSizeEmpty, *tg.PhotoPathSize, *tg.PhotoStrippedSize:
			continue
		default:
			continue
		}

		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		if prev, ok := bestByType[t]; !ok || area > prev {
			bestByType[t] = area
		}
	}

	if len(bestByType) == 0 {
		return nil
	}

	type scored struct {
		t    string
		area int
	}
	scoredTypes := make([]scored, 0, len(bestByType))
	for t, area := range bestByType {
		scoredTypes = append(scoredTypes, scored{t: t, area: area})
	}
	sort.Slice(scoredTypes, func(i, j int) bool {
		if scoredTypes[i].area == scoredTypes[j].area {
			return scoredTypes[i].t < scoredTypes[j].t
		}
		return scoredTypes[i].area > scoredTypes[j].area
	})

	out := make([]string, 0, len(scoredTypes))
	for _, s := range scoredTypes {
		out = append(out, s.t)
	}
	return out
}

func photoDownloadSize(photo *tg.Photo, thumb string) int64 {
	if photo == nil || len(photo.Sizes) == 0 {
		return 0
	}

	thumb = strings.TrimSpace(thumb)
	var fallback int64
	for _, s := range photo.Sizes {
		if s == nil {
			continue
		}

		var (
			t    string
			size int64
		)
		switch v := s.(type) {
		case *tg.PhotoSize:
			t = v.Type
			size = int64(v.Size)
		case *tg.PhotoCachedSize:
			t = v.Type
			size = int64(len(v.Bytes))
		case *tg.PhotoSizeProgressive:
			t = v.Type
			for _, part := range v.Sizes {
				if int64(part) > size {
					size = int64(part)
				}
			}
		default:
			continue
		}

		if size > fallback {
			fallback = size
		}
		if thumb != "" && strings.TrimSpace(t) == thumb && size > 0 {
			return size
		}
	}

	return fallback
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// Normalize path separators and drop any directory component.
	name = strings.ReplaceAll(name, "\\", "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	const maxLen = 180
	var b strings.Builder
	b.Grow(len(name))

	prevUnderscore := false
	for _, r := range name {
		keep := (r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			r == '.' || r == '_' || r == '-'
		if keep {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}

	out := strings.Trim(b.String(), "._-")
	if out == "" {
		out = "file"
	}
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}

func findDocumentFilename(attrs []tg.DocumentAttributeClass) (string, bool) {
	for _, attr := range attrs {
		if v, ok := attr.(*tg.DocumentAttributeFilename); ok {
			name := strings.TrimSpace(v.FileName)
			if name != "" {
				return name, true
			}
			return "", false
		}
	}
	return "", false
}

func hasFilenameAttr(attrs []tg.DocumentAttributeClass) bool {
	_, ok := findDocumentFilename(attrs)
	return ok
}

func createUniqueFile(dir string, base string) (*os.File, string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "file"
	}

	name := base
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "file"
	}

	for i := 0; i < 1000; i++ {
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, path, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
		name = fmt.Sprintf("%s_%d%s", stem, i+1, ext)
	}

	return nil, "", fmt.Errorf("failed to create unique file in %q for %q", dir, base)
}
