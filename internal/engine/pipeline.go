package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"my-go-server/internal/engine/processor"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

var ErrUnsupportedCloneMode = errors.New("unsupported clone mode")

// ProcessMessage is the engine pipeline entry for a single incoming Telegram message.
// It aggregates album messages (same GroupedID) to avoid album fragmentation.
func (m *TaskManager) ProcessMessage(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, task model.Task, msg *tg.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || msg == nil {
		return nil
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
	}

	allowedTypes := normalizeTypeSet(task.ContentTypes.Strings())
	contentType := m.DetectContentType(msg)
	if allowedTypes != nil {
		if _, ok := allowedTypes[contentType]; !ok {
			return nil
		}
	}

	if msg.GroupedID != 0 && msg.Media != nil && m.grouper != nil && task.ID != 0 {
		taskID := task.ID
		groupedID := msg.GroupedID
		m.grouper.Add(taskID, groupedID, msg, func(batch []*tg.Message) {
			if err := m.processAlbumBatch(ctx, api, peer, task, batch, allowedTypes); err != nil && global.Logger != nil {
				global.Logger.Error(
					"process album batch failed",
					zap.Uint("task_id", taskID),
					zap.Int64("grouped_id", groupedID),
					zap.Error(err),
				)
			}
		})
		return nil
	}

	return m.processSingleMessage(ctx, api, peer, task, msg)
}

func (m *TaskManager) processSingleMessage(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, task model.Task, msg *tg.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if msg == nil {
		return nil
	}

	msgToSend := msg
	if procs := m.processors(); procs.Text != nil && procs.Text.Enabled() {
		if out, changed := procs.Text.Process(msg.Message); changed {
			cp := *msg
			cp.Message = out
			cp.Entities = nil
			msgToSend = &cp
		}
	}

	if msg.Media == nil {
		return m.SendText(ctx, api, peer, msgToSend)
	}

	switch task.CloneMode {
	case 2:
		return m.SendMedia(ctx, api, msgToSend, task, peer)
	case 3:
		localPath, _, cleanup, err := m.DownloadFile(ctx, api, msgToSend, task.ID)
		if err != nil {
			return err
		}
		if cleanup != nil {
			defer func() { _ = cleanup() }()
		}

		uploadPath := localPath
		var thumb tg.InputFileClass

		procs := m.processors()
		switch media := msgToSend.Media.(type) {
		case *tg.MessageMediaPhoto:
			if procs.Image != nil && procs.Image.Enabled() {
				if outPath, c, changed, err := procs.Image.ProcessPath(ctx, localPath); err != nil {
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

					if ok, err := procs.Video.ExtractCover(ctx, localPath, coverPath); err != nil {
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
							if err := processor.ModifyFileMD5(coverUploadPath); err != nil {
								return err
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
				if outPath, c, changed, err := procs.Image.ProcessPath(ctx, localPath); err != nil {
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

		if task.ChangeMD5 {
			if err := processor.ModifyFileMD5(uploadPath); err != nil {
				return err
			}
		}
		inputFile, err := m.UploadFile(ctx, api, uploadPath)
		if err != nil {
			return err
		}
		inputMedia, err := m.WrapUploadedMedia(ctx, api, inputFile, msgToSend)
		if err != nil {
			return err
		}
		if inputMedia == nil {
			return ErrUnsupportedMedia
		}
		if thumb != nil {
			if doc, ok := inputMedia.(*tg.InputMediaUploadedDocument); ok {
				doc.Thumb = thumb
			}
		}

		rid, err := randomID()
		if err != nil {
			return err
		}

		req := &tg.MessagesSendMediaRequest{
			Peer:     peer,
			Media:    inputMedia,
			Message:  msgToSend.Message,
			RandomID: rid,
		}
		if len(msgToSend.Entities) > 0 {
			req.Entities = msgToSend.Entities
		}

		_, err = api.MessagesSendMedia(ctx, req)
		return err
	default:
		return ErrUnsupportedCloneMode
	}
}

func (m *TaskManager) processAlbumBatch(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, task model.Task, msgs []*tg.Message, allowedTypes map[string]struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Filter nil/unsupported/filtered-out messages.
	filtered := make([]*tg.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.Media == nil {
			continue
		}
		if allowedTypes != nil {
			ct := m.DetectContentType(msg)
			if _, ok := allowedTypes[ct]; !ok {
				continue
			}
		}
		filtered = append(filtered, msg)
	}

	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return m.processSingleMessage(ctx, api, peer, task, filtered[0])
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	// Caption/entities only on the first item.
	if procs := m.processors(); procs.Text != nil && procs.Text.Enabled() && len(filtered) > 0 {
		first := filtered[0]
		if first != nil {
			if out, changed := procs.Text.Process(first.Message); changed {
				cp := *first
				cp.Message = out
				cp.Entities = nil
				copied := make([]*tg.Message, len(filtered))
				copy(copied, filtered)
				copied[0] = &cp
				filtered = copied
			}
		}
	}

	switch task.CloneMode {
	case 2:
		return m.SendAlbum(ctx, api, filtered, task, peer)
	case 3:
		return m.SendUploadedAlbum(ctx, api, filtered, task, peer)
	default:
		return ErrUnsupportedCloneMode
	}
}
