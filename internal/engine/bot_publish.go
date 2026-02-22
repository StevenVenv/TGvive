package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"my-go-server/internal/engine/processor"
	wm "my-go-server/internal/engine/watermark"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

func resolveBotChatID(targetURL string) (string, error) {
	s := strings.TrimSpace(targetURL)
	if s == "" {
		return "", errors.New("target_url is empty")
	}

	// Numeric chat id (e.g. -100xxxx).
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return s, nil
	}

	// @username
	if strings.HasPrefix(s, "@") {
		u := strings.TrimSpace(s[1:])
		if u == "" {
			return "", errors.New("invalid @username")
		}
		return "@" + u, nil
	}

	// Bare username.
	if isBotUsernameLike(s) {
		return "@" + s, nil
	}

	// t.me links.
	if strings.Contains(s, "t.me/") || strings.Contains(s, "telegram.me/") {
		// url.Parse without scheme treats the input as path.
		if !strings.Contains(s, "://") && (strings.HasPrefix(s, "t.me/") || strings.HasPrefix(s, "telegram.me/")) {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err == nil && u != nil {
			path := strings.Trim(strings.TrimSpace(u.Path), "/")
			if path == "" {
				return "", errors.New("invalid t.me url")
			}
			parts := strings.Split(path, "/")
			if len(parts) == 1 {
				p := strings.TrimSpace(parts[0])
				if strings.HasPrefix(p, "@") {
					p = strings.TrimSpace(strings.TrimPrefix(p, "@"))
				}
				if isBotUsernameLike(p) {
					return "@" + p, nil
				}
			}
			if len(parts) >= 2 && parts[0] == "c" {
				return "", errors.New("t.me/c/... is not a bot chat id")
			}
			return "", errors.New("unsupported t.me url path: " + path)
		}
	}

	return "", errors.New("unsupported bot target format")
}

func isBotUsernameLike(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}

type botAPIMessage struct {
	MessageID int `json:"message_id"`
}

func botHTTPClient() *http.Client {
	return &http.Client{Timeout: 6 * time.Minute}
}

func botPostForm[T any](ctx context.Context, bot global.StoredBot, method string, values url.Values) (T, error) {
	var zero T
	if strings.TrimSpace(bot.Token) == "" {
		return zero, errors.New("bot token is empty")
	}

	u := botAPIURL(bot.APIBase, bot.Token, method)
	body := strings.NewReader(values.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := botHTTPClient().Do(req)
	if err != nil {
		return zero, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return zero, err
	}

	var parsed tgBotAPIResp[T]
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return zero, fmt.Errorf("bot api invalid json (http=%d): %w", res.StatusCode, err)
	}
	if !parsed.OK {
		desc := strings.TrimSpace(parsed.Description)
		if desc == "" {
			desc = "bot api returned ok=false"
		}
		return zero, fmt.Errorf("bot api error (code=%d http=%d): %s", parsed.ErrorCode, res.StatusCode, desc)
	}
	return parsed.Result, nil
}

type botMultipartFile struct {
	FieldName string
	FileName  string
	Path      string
}

func botPostMultipart[T any](ctx context.Context, bot global.StoredBot, method string, fields map[string]string, files []botMultipartFile) (T, error) {
	var zero T
	if strings.TrimSpace(bot.Token) == "" {
		return zero, errors.New("bot token is empty")
	}

	u := botAPIURL(bot.APIBase, bot.Token, method)
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	writeErr := make(chan error, 1)
	go func() {
		defer func() {
			_ = mw.Close()
			_ = pw.Close()
		}()

		for k, v := range fields {
			if strings.TrimSpace(k) == "" {
				continue
			}
			if err := mw.WriteField(k, v); err != nil {
				_ = pw.CloseWithError(err)
				writeErr <- err
				return
			}
		}

		for _, f := range files {
			if strings.TrimSpace(f.FieldName) == "" || strings.TrimSpace(f.Path) == "" {
				continue
			}
			fn := strings.TrimSpace(f.FileName)
			if fn == "" {
				fn = filepath.Base(f.Path)
			}
			part, err := mw.CreateFormFile(f.FieldName, fn)
			if err != nil {
				_ = pw.CloseWithError(err)
				writeErr <- err
				return
			}
			fd, err := os.Open(f.Path)
			if err != nil {
				_ = pw.CloseWithError(err)
				writeErr <- err
				return
			}
			_, err = io.Copy(part, fd)
			_ = fd.Close()
			if err != nil {
				_ = pw.CloseWithError(err)
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, pr)
	if err != nil {
		_ = pr.Close()
		_ = pw.CloseWithError(err)
		return zero, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	res, err := botHTTPClient().Do(req)
	if err != nil {
		_ = pr.Close()
		_ = pw.CloseWithError(err)
		return zero, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		_ = pr.Close()
		_ = pw.CloseWithError(err)
		return zero, err
	}

	// Ensure writer goroutine completed (surface errors).
	if werr := <-writeErr; werr != nil {
		return zero, werr
	}

	var parsed tgBotAPIResp[T]
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return zero, fmt.Errorf("bot api invalid json (http=%d): %w", res.StatusCode, err)
	}
	if !parsed.OK {
		desc := strings.TrimSpace(parsed.Description)
		if desc == "" {
			desc = "bot api returned ok=false"
		}
		return zero, fmt.Errorf("bot api error (code=%d http=%d): %s", parsed.ErrorCode, res.StatusCode, desc)
	}
	return parsed.Result, nil
}

func (m *TaskManager) botSendTextFromTGMessage(ctx context.Context, msg *tg.Message, task model.Task, pub *taskPublisherRuntime) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pub == nil || pub.Kind != publisherKindBot {
		return errors.New("publisher is not bot")
	}
	if msg == nil {
		return nil
	}
	text := strings.TrimSpace(msg.Message)
	if text == "" {
		return nil
	}

	replyID := 0
	if rt, ok := buildKeepReplyInput(ctx, task, msg).(*tg.InputReplyToMessage); ok && rt != nil {
		replyID = rt.ReplyToMsgID
	}

	vals := url.Values{}
	vals.Set("chat_id", pub.ChatID)
	vals.Set("text", text)
	if replyID > 0 {
		vals.Set("reply_to_message_id", strconv.Itoa(replyID))
		vals.Set("allow_sending_without_reply", "true")
	}

	out, err := botPostForm[botAPIMessage](ctx, pub.Bot, "sendMessage", vals)
	if err != nil {
		return err
	}
	if msg.ID > 0 && out.MessageID > 0 {
		storeMsgMapping(task, msg.ID, out.MessageID)
	}
	return nil
}

func isAudioDocument(media *tg.MessageMediaDocument) bool {
	if media == nil || media.Document == nil {
		return false
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok || doc == nil {
		return false
	}
	for _, attr := range doc.Attributes {
		if _, ok := attr.(*tg.DocumentAttributeAudio); ok {
			return true
		}
	}
	mt := strings.ToLower(strings.TrimSpace(doc.MimeType))
	return strings.HasPrefix(mt, "audio/")
}

func botSingleMethodForMessage(msg *tg.Message) (method string, fileField string, mediaType string) {
	if msg == nil || msg.Media == nil {
		return "", "", ""
	}
	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		_ = media
		return "sendPhoto", "photo", "photo"
	case *tg.MessageMediaDocument:
		if media.Voice {
			return "sendVoice", "voice", "audio"
		}
		if isVideoDocument(media) {
			return "sendVideo", "video", "video"
		}
		if isAudioDocument(media) {
			return "sendAudio", "audio", "audio"
		}
		return "sendDocument", "document", "document"
	default:
		return "", "", ""
	}
}

func (m *TaskManager) botSendUploadedMediaFromTGMessage(ctx context.Context, crawlerAPI *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, pub *taskPublisherRuntime) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pub == nil || pub.Kind != publisherKindBot {
		return errors.New("publisher is not bot")
	}
	if crawlerAPI == nil {
		return errors.New("crawler tg api is nil")
	}
	if msg == nil || msg.Media == nil {
		return nil
	}

	method, fileField, _ := botSingleMethodForMessage(msg)
	if method == "" || fileField == "" {
		return m.botSendTextFromTGMessage(ctx, msg, task, pub)
	}

	localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, crawlerAPI, sourcePeer, msg, task.ID)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer func() { _ = cleanup() }()
	}

	uploadPath := localPath
	cleanups := []func() error(nil)
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
	wmCandidate := wmEnabled && isWatermarkableImageMessage(msg)

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

	uploadFileName := filepath.Base(uploadPath)
	if task.ChangeMD5 && task.RandomFilename {
		if d, ok := msg.Media.(*tg.MessageMediaDocument); ok && d != nil && d.Document != nil {
			if doc, ok := d.Document.AsNotEmpty(); ok && doc != nil {
				orig, _ := findDocumentFilename(doc.Attributes)
				if rnd, err := randomizeFilename(orig, doc.MimeType); err == nil && strings.TrimSpace(rnd) != "" {
					uploadFileName = sanitizeFilename(rnd)
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
						return err
					}
					outBytes = updated
				}
				dir := filepath.Dir(uploadPath)
				outPath := filepath.Join(dir, wmName)
				if err := os.WriteFile(outPath, outBytes, 0o600); err == nil {
					cleanups = append(cleanups, func() error { return os.Remove(outPath) })
					uploadPath = outPath
					uploadFileName = wmName
				}
			}
		}
	}

	if task.ChangeMD5 {
		if err := modifyFileMD5WithDetailLog(ctx, uploadPath, "修改MD5", msg.ID); err != nil {
			return err
		}
	}

	caption := msg.Message
	if out, truncated := sanitizeMediaCaptionText(caption); truncated {
		caption = out
	}

	replyID := 0
	if rt, ok := buildKeepReplyInput(ctx, task, msg).(*tg.InputReplyToMessage); ok && rt != nil {
		replyID = rt.ReplyToMsgID
	}

	fields := map[string]string{
		"chat_id": pub.ChatID,
	}
	if strings.TrimSpace(caption) != "" {
		fields["caption"] = caption
	}
	if replyID > 0 {
		fields["reply_to_message_id"] = strconv.Itoa(replyID)
		fields["allow_sending_without_reply"] = "true"
	}

	out, err := botPostMultipart[botAPIMessage](ctx, pub.Bot, method, fields, []botMultipartFile{
		{FieldName: fileField, FileName: uploadFileName, Path: uploadPath},
	})
	if err != nil {
		return err
	}
	if msg.ID > 0 && out.MessageID > 0 {
		storeMsgMapping(task, msg.ID, out.MessageID)
	}
	return nil
}

type botInputMedia struct {
	Type    string `json:"type"`
	Media   string `json:"media"`
	Caption string `json:"caption,omitempty"`
}

func (m *TaskManager) botSendUploadedAlbumFromTGMessages(ctx context.Context, crawlerAPI *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, pub *taskPublisherRuntime) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pub == nil || pub.Kind != publisherKindBot {
		return errors.New("publisher is not bot")
	}
	if crawlerAPI == nil {
		return errors.New("crawler tg api is nil")
	}
	if len(msgs) == 0 {
		return nil
	}

	// Bot API limit: 10 items per media group.
	const maxGroup = 10

	// Choose reply carrier for keep-reply.
	replyCarrier := msgs[0]
	for _, mm := range msgs {
		if extractReplyToSourceMsgID(mm) > 0 {
			replyCarrier = mm
			break
		}
	}
	replyID := 0
	if rt, ok := buildKeepReplyInput(ctx, task, replyCarrier).(*tg.InputReplyToMessage); ok && rt != nil {
		replyID = rt.ReplyToMsgID
	}

	for start := 0; start < len(msgs); start += maxGroup {
		end := start + maxGroup
		if end > len(msgs) {
			end = len(msgs)
		}
		chunk := msgs[start:end]

		inputMedia := make([]botInputMedia, 0, len(chunk))
		sentSrcMsgs := make([]*tg.Message, 0, len(chunk))
		files := make([]botMultipartFile, 0, len(chunk))
		cleanups := []func() error(nil)
		cleanUp := func() {
			for _, fn := range cleanups {
				if fn != nil {
					_ = fn()
				}
			}
		}

		for i, msg := range chunk {
			if msg == nil || msg.Media == nil {
				continue
			}
			_, _, mediaType := botSingleMethodForMessage(msg)
			if mediaType == "" {
				continue
			}

			localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, crawlerAPI, sourcePeer, msg, task.ID)
			if err != nil {
				cleanUp()
				return err
			}
			if cleanup != nil {
				cleanups = append(cleanups, cleanup)
			}

			uploadPath := localPath
			uploadFileName := filepath.Base(uploadPath)

			enableMediaEdit := task.CloneMode == 3 && task.EnableMediaEdit
			procs := m.processors()
			wmRule, wmEnabled := watermarkRuleForTask(task)
			wmCandidate := wmEnabled && isWatermarkableImageMessage(msg)

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

			if task.ChangeMD5 && task.RandomFilename {
				if d, ok := msg.Media.(*tg.MessageMediaDocument); ok && d != nil && d.Document != nil {
					if doc, ok := d.Document.AsNotEmpty(); ok && doc != nil {
						orig, _ := findDocumentFilename(doc.Attributes)
						if rnd, err := randomizeFilename(orig, doc.MimeType); err == nil && strings.TrimSpace(rnd) != "" {
							uploadFileName = sanitizeFilename(rnd)
						}
					}
				}
			}

			if wmCandidate {
				recordTaskDetailFromCtx(ctx, fmt.Sprintf("应用水印: %s", filepath.Base(uploadPath)))
				if b, rerr := os.ReadFile(uploadPath); rerr == nil {
					if outBytes, werr := wm.ApplyWatermark(b, wmRule); werr == nil {
						wmName := fmt.Sprintf("wm_%d_%d.jpg", msg.ID, i)
						if task.ChangeMD5 {
							updated, err := modifyBytesMD5WithDetailLog(ctx, outBytes, "修改MD5(水印)", wmName, msg.ID)
							if err != nil {
								cleanUp()
								return err
							}
							outBytes = updated
						}
						dir := filepath.Dir(uploadPath)
						outPath := filepath.Join(dir, wmName)
						if err := os.WriteFile(outPath, outBytes, 0o600); err == nil {
							cleanups = append(cleanups, func() error { return os.Remove(outPath) })
							uploadPath = outPath
							uploadFileName = wmName
						}
					}
				}
			}

			if task.ChangeMD5 {
				if err := modifyFileMD5WithDetailLog(ctx, uploadPath, "修改MD5", msg.ID); err != nil {
					cleanUp()
					return err
				}
			}

			attachName := fmt.Sprintf("file%d", i)
			files = append(files, botMultipartFile{
				FieldName: attachName,
				FileName:  uploadFileName,
				Path:      uploadPath,
			})
			im := botInputMedia{
				Type:  mediaType,
				Media: "attach://" + attachName,
			}
			inputMedia = append(inputMedia, im)
			sentSrcMsgs = append(sentSrcMsgs, msg)
		}

		if len(inputMedia) == 0 {
			cleanUp()
			continue
		}

		// Caption only on first item of first chunk.
		if start == 0 && len(inputMedia) > 0 && len(sentSrcMsgs) > 0 {
			caption := sentSrcMsgs[0].Message
			if out, truncated := sanitizeMediaCaptionText(caption); truncated {
				caption = out
			}
			inputMedia[0].Caption = caption
		}

		mediaJSON, err := json.Marshal(inputMedia)
		if err != nil {
			cleanUp()
			return err
		}

		fields := map[string]string{
			"chat_id": pub.ChatID,
			"media":   string(mediaJSON),
		}
		if replyID > 0 && start == 0 {
			fields["reply_to_message_id"] = strconv.Itoa(replyID)
			fields["allow_sending_without_reply"] = "true"
		}

		out, err := botPostMultipart[[]botAPIMessage](ctx, pub.Bot, "sendMediaGroup", fields, files)
		cleanUp()
		if err != nil {
			return err
		}
		sentIDs := make([]int, 0, len(out))
		for _, m2 := range out {
			if m2.MessageID > 0 {
				sentIDs = append(sentIDs, m2.MessageID)
			}
		}
		if len(sentIDs) > 0 {
			storeMsgMappingsInOrder(task, sentSrcMsgs, sentIDs)
		}
	}

	return nil
}
