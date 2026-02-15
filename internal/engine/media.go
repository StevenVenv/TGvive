package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"my-go-server/internal/model"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

var ErrUnsupportedMedia = errors.New("unsupported media")

const tmpMediaRoot = "./tmp/tgmedia"

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
		if media.Video || media.Round {
			return "video"
		}
		if media.Voice {
			return "audio"
		}

		if media.Document != nil {
			if doc, ok := media.Document.AsNotEmpty(); ok {
				for _, attr := range doc.Attributes {
					switch attr.(type) {
					case *tg.DocumentAttributeVideo, *tg.DocumentAttributeAnimated:
						return "video"
					case *tg.DocumentAttributeAudio:
						return "audio"
					}
				}
			}
		}

		return "file"
	default:
		return "other"
	}
}

// SendText sends a plain message (CloneMode=2 helper).
func (m *TaskManager) SendText(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, msg *tg.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
	}
	if msg == nil || strings.TrimSpace(msg.Message) == "" {
		return nil
	}

	rid, err := randomID()
	if err != nil {
		return err
	}

	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  msg.Message,
		RandomID: rid,
	}
	if len(msg.Entities) > 0 {
		req.Entities = msg.Entities
	}

	_, err = api.MessagesSendMessage(ctx, req)
	return err
}

// SendMedia sends a single media message using InputMediaPhoto/InputMediaDocument (CloneMode=2).
func (m *TaskManager) SendMedia(ctx context.Context, api *tg.Client, msg *tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_ = task
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
	}
	if msg == nil || msg.Media == nil {
		return nil
	}

	media, err := convertMessageMediaToInput(msg.Media)
	if err != nil {
		return err
	}

	rid, err := randomID()
	if err != nil {
		return err
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  msg.Message,
		RandomID: rid,
	}
	if len(msg.Entities) > 0 {
		req.Entities = msg.Entities
	}

	_, err = api.MessagesSendMedia(ctx, req)
	return err
}

// SendAlbum sends grouped media (CloneMode=2).
// Only the first item keeps caption/entities.
func (m *TaskManager) SendAlbum(ctx context.Context, api *tg.Client, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_ = task
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
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
		return nil
	case 1:
		return m.SendMedia(ctx, api, mediaMsgs[0], task, peer)
	}

	multi := make([]tg.InputSingleMedia, 0, len(mediaMsgs))
	for i, msg := range mediaMsgs {
		inputMedia, err := convertMessageMediaToInput(msg.Media)
		if err != nil {
			continue
		}
		rid, err := randomID()
		if err != nil {
			return err
		}

		item := tg.InputSingleMedia{
			Media:    inputMedia,
			RandomID: rid,
		}
		if i == 0 {
			item.Message = msg.Message
			if len(msg.Entities) > 0 {
				item.Entities = msg.Entities
			}
		}
		multi = append(multi, item)
	}

	if len(multi) == 0 {
		return nil
	}
	if len(multi) == 1 {
		return m.SendMedia(ctx, api, mediaMsgs[0], task, peer)
	}

	_, err := api.MessagesSendMultiMedia(ctx, &tg.MessagesSendMultiMediaRequest{
		Peer:       peer,
		MultiMedia: multi,
	})
	return err
}

// SendUploadedMedia sends a single media message by downloading to local disk and uploading back (CloneMode=3).
func (m *TaskManager) SendUploadedMedia(ctx context.Context, api *tg.Client, msg *tg.Message, task model.Task, peer tg.InputPeerClass) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
	}
	if msg == nil || msg.Media == nil {
		return nil
	}

	localPath, meta, cleanup, err := downloadMessageMedia(ctx, api, msg, task.ID)
	if err != nil {
		return err
	}

	inputFile, err := uploader.NewUploader(api).WithThreads(4).FromPath(ctx, localPath)
	if err != nil {
		return fmt.Errorf("upload file %q: %w", localPath, err)
	}

	uploaded := uploadAsInputMediaUploaded(meta, inputFile)
	rid, err := randomID()
	if err != nil {
		return err
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    uploaded,
		Message:  msg.Message,
		RandomID: rid,
	}
	if len(msg.Entities) > 0 {
		req.Entities = msg.Entities
	}

	if _, err := api.MessagesSendMedia(ctx, req); err != nil {
		return fmt.Errorf("send uploaded media failed (path=%q): %w", localPath, err)
	}

	_ = cleanup()
	return nil
}

// SendUploadedAlbum sends grouped media by downloading and re-uploading (CloneMode=3).
// Only the first item keeps caption/entities.
func (m *TaskManager) SendUploadedAlbum(ctx context.Context, api *tg.Client, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
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
		return nil
	case 1:
		return m.SendUploadedMedia(ctx, api, mediaMsgs[0], task, peer)
	}

	ups := make([]tg.InputSingleMedia, 0, len(mediaMsgs))
	cleanups := make([]func() error, 0, len(mediaMsgs))
	localPaths := make([]string, 0, len(mediaMsgs))

	for _, msg := range mediaMsgs {
		localPath, meta, cleanup, err := downloadMessageMedia(ctx, api, msg, task.ID)
		if err != nil {
			return err
		}

		inputFile, err := uploader.NewUploader(api).WithThreads(4).FromPath(ctx, localPath)
		if err != nil {
			return fmt.Errorf("upload file %q: %w", localPath, err)
		}

		uploaded := uploadAsInputMediaUploaded(meta, inputFile)
		inputMedia, err := uploadMediaForAlbum(ctx, api, peer, uploaded)
		if err != nil {
			return fmt.Errorf("upload media for album failed (path=%q): %w", localPath, err)
		}

		rid, err := randomID()
		if err != nil {
			return err
		}

		ups = append(ups, tg.InputSingleMedia{
			Media:    inputMedia,
			RandomID: rid,
		})
		cleanups = append(cleanups, cleanup)
		localPaths = append(localPaths, localPath)
	}

	if len(ups) == 0 {
		return nil
	}
	if len(ups) == 1 {
		return m.SendUploadedMedia(ctx, api, mediaMsgs[0], task, peer)
	}

	// Caption/entities only on the first item.
	ups[0].Message = mediaMsgs[0].Message
	if len(mediaMsgs[0].Entities) > 0 {
		ups[0].Entities = mediaMsgs[0].Entities
	}

	if _, err := api.MessagesSendMultiMedia(ctx, &tg.MessagesSendMultiMediaRequest{
		Peer:       peer,
		MultiMedia: ups,
	}); err != nil {
		return fmt.Errorf("send uploaded album failed (paths=%v): %w", localPaths, err)
	}

	for _, fn := range cleanups {
		_ = fn()
	}
	return nil
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

	var (
		loc      tg.InputFileLocationClass
		baseName string
	)

	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		if media.Photo == nil {
			return "", mediaMeta{}, nil, ErrUnsupportedMedia
		}
		photo, ok := media.Photo.AsNotEmpty()
		if !ok {
			return "", mediaMeta{}, nil, ErrUnsupportedMedia
		}

		thumb, ok := bestPhotoThumbType(photo)
		if !ok {
			return "", mediaMeta{}, nil, ErrUnsupportedMedia
		}
		loc = &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumb,
		}

		baseName = fmt.Sprintf("photo_%d.jpg", photo.ID)
		meta = mediaMeta{
			Kind:       mediaKindPhoto,
			Spoiler:    media.Spoiler,
			TTLSeconds: media.TTLSeconds,
		}
	case *tg.MessageMediaDocument:
		if media.Document == nil {
			return "", mediaMeta{}, nil, ErrUnsupportedMedia
		}
		doc, ok := media.Document.AsNotEmpty()
		if !ok {
			return "", mediaMeta{}, nil, ErrUnsupportedMedia
		}
		loc = doc.AsInputDocumentFileLocation()

		if name, ok := findDocumentFilename(doc.Attributes); ok {
			baseName = sanitizeFilename(name)
		}
		if baseName == "" {
			baseName = fmt.Sprintf("doc_%d.bin", doc.ID)
		}

		attrs := make([]tg.DocumentAttributeClass, len(doc.Attributes))
		copy(attrs, doc.Attributes)

		meta = mediaMeta{
			Kind:       mediaKindDocument,
			Spoiler:    media.Spoiler,
			TTLSeconds: media.TTLSeconds,
			MimeType:   strings.TrimSpace(doc.MimeType),
			Attributes: attrs,
			Filename:   baseName,
		}
	default:
		return "", mediaMeta{}, nil, ErrUnsupportedMedia
	}

	f, path, err := createUniqueFile(dir, sanitizeFilename(baseName))
	if err != nil {
		return "", mediaMeta{}, nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	d := downloader.NewDownloader()
	if _, err := d.Download(api, loc).
		WithThreads(4).
		WithVerify(true).
		Parallel(ctx, f); err != nil {
		_ = os.Remove(path)
		return "", mediaMeta{}, nil, fmt.Errorf("download media to %q: %w", path, err)
	}

	return path, meta, func() error { return os.Remove(path) }, nil
}

func uploadAsInputMediaUploaded(meta mediaMeta, inputFile tg.InputFileClass) tg.InputMediaClass {
	switch meta.Kind {
	case mediaKindPhoto:
		return &tg.InputMediaUploadedPhoto{
			Spoiler:    meta.Spoiler,
			File:       inputFile,
			TTLSeconds: meta.TTLSeconds,
		}
	case mediaKindDocument:
		attrs := meta.Attributes
		if !hasFilenameAttr(attrs) {
			name := meta.Filename
			if name == "" {
				name = "file.bin"
			}
			attrs = append(attrs, &tg.DocumentAttributeFilename{FileName: sanitizeFilename(name)})
		}
		mime := strings.TrimSpace(meta.MimeType)
		if mime == "" {
			mime = "application/octet-stream"
		}
		return &tg.InputMediaUploadedDocument{
			Spoiler:    meta.Spoiler,
			File:       inputFile,
			MimeType:   mime,
			Attributes: attrs,
			TTLSeconds: meta.TTLSeconds,
		}
	default:
		return &tg.InputMediaUploadedDocument{
			File:     inputFile,
			MimeType: "application/octet-stream",
		}
	}
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

func bestPhotoThumbType(photo *tg.Photo) (thumb string, ok bool) {
	if photo == nil || len(photo.Sizes) == 0 {
		return "", false
	}

	bestType := ""
	bestArea := -1
	for _, s := range photo.Sizes {
		switch v := s.(type) {
		case *tg.PhotoSize:
			area := v.W * v.H
			if area > bestArea {
				bestArea = area
				bestType = v.Type
			}
		case *tg.PhotoCachedSize:
			area := v.W * v.H
			if area > bestArea {
				bestArea = area
				bestType = v.Type
			}
		case *tg.PhotoSizeProgressive:
			area := v.W * v.H
			if area > bestArea {
				bestArea = area
				bestType = v.Type
			}
		}
	}

	if bestType != "" {
		return bestType, true
	}

	for _, s := range photo.Sizes {
		switch s.(type) {
		case *tg.PhotoSizeEmpty, *tg.PhotoPathSize:
			continue
		default:
			if t := strings.TrimSpace(s.GetType()); t != "" {
				return t, true
			}
		}
	}
	return "", false
}

func randomID() (int64, error) {
	return crypto.RandInt64(crypto.DefaultRand())
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
