package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"my-go-server/internal/engine/processor"
	wm "my-go-server/internal/engine/watermark"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// SendUploadedMediaSeparated downloads media using downloadAPI (crawler) and uploads/sends using sendAPI (publisher).
func (m *TaskManager) SendUploadedMediaSeparated(ctx context.Context, downloadAPI *tg.Client, sendAPI *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_, err := m.sendUploadedMediaUpdatesSeparated(ctx, downloadAPI, sendAPI, sourcePeer, msg, task, peer)
	return err
}

func (m *TaskManager) sendUploadedMediaUpdatesSeparated(ctx context.Context, downloadAPI *tg.Client, sendAPI *tg.Client, sourcePeer tg.InputPeerClass, msg *tg.Message, task model.Task, peer tg.InputPeerClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if downloadAPI == nil {
		return nil, errors.New("download tg api is nil")
	}
	if sendAPI == nil {
		return nil, errors.New("publisher tg api is nil")
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

	localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, downloadAPI, sourcePeer, msg, task.ID)
	if err != nil {
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
						if inputThumb, err := m.UploadFile(ctx, sendAPI, coverUploadPath); err != nil {
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
				if inputFile, uerr := uploadBytes(ctx, sendAPI, wmName, outBytes); uerr == nil && inputFile != nil {
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

					upd, err := sendAPI.MessagesSendMedia(ctx, req)
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
	inputFile, err := m.UploadFile(ctx, sendAPI, uploadPath)
	if err != nil {
		return nil, fmt.Errorf("upload file %q: %w", uploadPath, err)
	}

	uploaded, err := m.WrapUploadedMedia(ctx, sendAPI, inputFile, msg, task.ChangeMD5 && task.RandomFilename)
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

	upd, err := sendAPI.MessagesSendMedia(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send uploaded media failed (path=%q): %w", uploadPath, err)
	}
	storeMsgMapping(task, msg.ID, minPositiveInt(extractSentMsgIDs(upd)))

	return upd, nil
}

// SendUploadedAlbumSeparated downloads album media using downloadAPI and uploads/sends using sendAPI.
func (m *TaskManager) SendUploadedAlbumSeparated(ctx context.Context, downloadAPI *tg.Client, sendAPI *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) error {
	_, err := m.sendUploadedAlbumUpdatesSeparated(ctx, downloadAPI, sendAPI, sourcePeer, msgs, task, peer)
	return err
}

func (m *TaskManager) sendUploadedAlbumUpdatesSeparated(ctx context.Context, downloadAPI *tg.Client, sendAPI *tg.Client, sourcePeer tg.InputPeerClass, msgs []*tg.Message, task model.Task, peer tg.InputPeerClass) (tg.UpdatesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if downloadAPI == nil {
		return nil, errors.New("download tg api is nil")
	}
	if sendAPI == nil {
		return nil, errors.New("publisher tg api is nil")
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
		return m.sendUploadedMediaUpdatesSeparated(ctx, downloadAPI, sendAPI, sourcePeer, mediaMsgs[0], task, peer)
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

		localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, downloadAPI, sourcePeer, msg, task.ID)
		if err != nil {
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
							if inputThumb, err := m.UploadFile(ctx, sendAPI, coverUploadPath); err != nil {
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
					if inputFile, uerr := uploadBytes(ctx, sendAPI, wmName, outBytes); uerr == nil && inputFile != nil {
						recordTaskDetailFromCtx(ctx, fmt.Sprintf("上传完成: %s (%.1fMB, msg_id=%d)", wmName, float64(len(outBytes))/1024.0/1024.0, msg.ID))
						spoiler, ttl := messageSpoilerTTL(msg)
						uploaded := &tg.InputMediaUploadedPhoto{
							File:       inputFile,
							Spoiler:    spoiler,
							TTLSeconds: ttl,
						}
						if thumb != nil {
							_ = thumb
						}
						inputMedia, err := uploadMediaForAlbum(ctx, sendAPI, peer, uploaded)
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
		inputFile, err := m.UploadFile(ctx, sendAPI, uploadPath)
		if err != nil {
			return nil, fmt.Errorf("upload file %q: %w", uploadPath, err)
		}

		uploaded, err := m.WrapUploadedMedia(ctx, sendAPI, inputFile, msg, task.ChangeMD5 && task.RandomFilename)
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
		inputMedia, err := uploadMediaForAlbum(ctx, sendAPI, peer, uploaded)
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
		return m.sendUploadedMediaUpdatesSeparated(ctx, downloadAPI, sendAPI, sourcePeer, mediaMsgs[0], task, peer)
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

	upd, err := sendAPI.MessagesSendMultiMedia(ctx, &tg.MessagesSendMultiMediaRequest{
		Peer:       peer,
		ReplyTo:    replyTo,
		MultiMedia: ups,
	})
	if err != nil {
		return nil, fmt.Errorf("send uploaded album failed (paths=%v): %w", localPaths, err)
	}
	storeMsgMappingsInOrder(task, mediaMsgs, extractSentMsgIDs(upd))

	return upd, nil
}
