package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"my-go-server/internal/engine/localdb"
	wm "my-go-server/internal/engine/watermark"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

const (
	commentSendDelayMin = 300 * time.Millisecond
	commentSendDelayMax = 1 * time.Second

	// commentScanBatchSize limits each DB scan to avoid loading huge rows into memory.
	commentScanBatchSize = 50
)

// sendPendingCommentsForRoot sends all unforwarded comments for a given source discussion root,
// in ascending msg id order, and marks them as forwarded in the task localdb.
// Best-effort: errors are logged and the caller continues its main workflow.
// FloodWait will stop processing remaining comments for this root (skip).
func (m *TaskManager) sendPendingCommentsForRoot(
	ctx context.Context,
	api *tg.Client,
	task model.Task,
	cfg *commentPipelineConfig,
	sourceRootID int,
	targetRootID int,
) {
	if err := ctx.Err(); err != nil {
		return
	}
	if m == nil || api == nil || cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return
	}
	if cfg.SourceLinkedPeer == nil || cfg.TargetLinkedPeer == nil {
		return
	}
	if sourceRootID <= 0 || targetRootID <= 0 {
		return
	}

	cfg.sendMu.Lock()
	defer cfg.sendMu.Unlock()

	db := cfg.LocalDB

	wmRule, wmEnabled := watermarkRuleForTask(task)

	sendWatermarkedImage := func(commentMsgID int, payload localdb.LightPayload) error {
		if commentMsgID <= 0 {
			return errors.New("comment_msg_id is required")
		}
		srcBytes, fullMsg, err := downloadMessageMediaBytes(ctx, api, cfg.SourceLinkedPeer, commentMsgID)
		if err != nil {
			return err
		}
		if fullMsg == nil || !isWatermarkableImageMessage(fullMsg) {
			return errors.New("message is not watermarkable")
		}

		outBytes, err := wm.ApplyWatermark(srcBytes, wmRule)
		if err != nil {
			return err
		}
		inputFile, err := uploadBytes(ctx, api, "wm.jpg", outBytes)
		if err != nil {
			return err
		}

		spoiler, ttl := messageSpoilerTTL(fullMsg)
		rid, err := randomID()
		if err != nil {
			return err
		}

		replyTo := &tg.InputReplyToMessage{ReplyToMsgID: targetRootID}
		caption := payload.Text
		if out, truncated := sanitizeMediaCaptionText(caption); truncated {
			caption = out
		}
		_, err = api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
			Peer:    cfg.TargetLinkedPeer,
			ReplyTo: replyTo,
			Media: &tg.InputMediaUploadedPhoto{
				File:       inputFile,
				Spoiler:    spoiler,
				TTLSeconds: ttl,
			},
			Message:  caption,
			RandomID: rid,
		})
		return err
	}

	sendOne := func(item localdb.LocalComment, payload localdb.LightPayload) error {
		if wmEnabled && strings.EqualFold(payload.MediaType, "image") {
			if err := sendWatermarkedImage(item.CommentMsgID, payload); err == nil {
				return nil
			} else if _, ok := tgerr.AsFloodWait(err); ok {
				return err
			}
		}

		sendErr := m.sendLightPayloadAsComment(ctx, api, cfg, payload, targetRootID)
		if sendErr != nil {
			// Upload fallback: refetch original message and send with fallback.
			if fullMsg, rerr := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, item.CommentMsgID); rerr == nil && fullMsg != nil {
				sendErr = m.sendCommentWithFallback(ctx, api, cfg.SourceLinkedPeer, cfg.TargetLinkedPeer, fullMsg, targetRootID, task)
			}
		}
		return sendErr
	}

	markForwarded := func(ids []int) {
		if len(ids) == 0 {
			return
		}
		_ = db.Model(&localdb.LocalComment{}).
			Where("source_post_id = ? AND comment_msg_id IN ?", sourceRootID, ids).
			Update("is_forwarded", true).Error
	}

	cursor := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		var batch []localdb.LocalComment
		q := db.Where("source_post_id = ? AND is_forwarded = ?", sourceRootID, false)
		if cursor > 0 {
			q = q.Where("comment_msg_id > ?", cursor)
		}
		if err := q.Order("comment_msg_id ASC").Limit(commentScanBatchSize).Find(&batch).Error; err != nil {
			if global.Logger != nil {
				global.Logger.Warn(
					"scan pending comments failed",
					zap.Uint("task_id", task.ID),
					zap.Int("source_root_id", sourceRootID),
					zap.Error(err),
				)
			}
			return
		}
		if len(batch) == 0 {
			return
		}

		// Avoid splitting a grouped album across batches: if the tail item belongs to an album,
		// fetch the remaining items of that album in additional queries.
		if tail := batch[len(batch)-1]; tail.GroupedID != 0 && tail.CommentMsgID > 0 {
			gid := tail.GroupedID
			last := tail.CommentMsgID
			for {
				var extra []localdb.LocalComment
				if err := db.
					Where("source_post_id = ? AND is_forwarded = ? AND grouped_id = ? AND comment_msg_id > ?", sourceRootID, false, gid, last).
					Order("comment_msg_id ASC").
					Limit(commentScanBatchSize).
					Find(&extra).Error; err != nil {
					break
				}
				if len(extra) == 0 {
					break
				}
				batch = append(batch, extra...)
				last = extra[len(extra)-1].CommentMsgID
				if last <= 0 {
					break
				}
			}
		}

		for i := 0; i < len(batch); {
			if err := ctx.Err(); err != nil {
				return
			}

			item := batch[i]
			if item.CommentMsgID <= 0 || item.SourcePostID != sourceRootID || item.IsForwarded {
				i++
				continue
			}

			// Grouped media (album) support.
			if item.GroupedID != 0 {
				gid := item.GroupedID
				j := i
				group := make([]localdb.LocalComment, 0, 8)
				for j < len(batch) {
					it := batch[j]
					if it.CommentMsgID <= 0 || it.SourcePostID != sourceRootID || it.IsForwarded || it.GroupedID != gid {
						break
					}
					group = append(group, it)
					j++
				}

				// Try to preserve album grouping when possible.
				if len(group) >= 2 {
					payloads := make([]localdb.LightPayload, 0, len(group))
					ids := make([]int, 0, len(group))
					okAlbum := true
					needWM := false
					for _, it := range group {
						ids = append(ids, it.CommentMsgID)
						var p localdb.LightPayload
						if len(it.LightPayload) > 0 {
							if err := json.Unmarshal(it.LightPayload, &p); err != nil {
								okAlbum = false
								break
							}
						}
						if strings.EqualFold(p.MediaType, "image") {
							needWM = needWM || wmEnabled
						} else if len(p.MediaBytes) == 0 {
							okAlbum = false
							break
						}
						payloads = append(payloads, p)
					}

					if okAlbum {
						// Watermark path: upload watermarked photos for image items.
						if needWM {
							replyTo := &tg.InputReplyToMessage{ReplyToMsgID: targetRootID}
							multi := make([]tg.InputSingleMedia, 0, len(group))
							sendErr := error(nil)
							for idx, it := range group {
								p := payloads[idx]

								im := tg.InputMediaClass(nil)
								if wmEnabled && strings.EqualFold(p.MediaType, "image") {
									// Best-effort watermark; if it fails, fallback to per-item send.
									srcBytes, fullMsg, err := downloadMessageMediaBytes(ctx, api, cfg.SourceLinkedPeer, it.CommentMsgID)
									if err != nil {
										sendErr = err
										break
									}
									if fullMsg == nil || !isWatermarkableImageMessage(fullMsg) {
										sendErr = errors.New("message is not watermarkable")
										break
									}
									outBytes, err := wm.ApplyWatermark(srcBytes, wmRule)
									if err != nil {
										sendErr = err
										break
									}
									inputFile, err := uploadBytes(ctx, api, "wm.jpg", outBytes)
									if err != nil {
										sendErr = err
										break
									}
									spoiler, ttl := messageSpoilerTTL(fullMsg)
									im = &tg.InputMediaUploadedPhoto{
										File:       inputFile,
										Spoiler:    spoiler,
										TTLSeconds: ttl,
									}
								} else if len(p.MediaBytes) > 0 {
									decoded, err := tg.DecodeInputMedia(&bin.Buffer{Buf: p.MediaBytes})
									if err != nil || decoded == nil {
										if err == nil {
											err = errors.New("decode input media returned nil")
										}
										sendErr = err
										break
									}
									im = decoded
								} else {
									sendErr = errors.New("album item missing media")
									break
								}

								rid, err := randomID()
								if err != nil {
									sendErr = err
									break
								}
								item := tg.InputSingleMedia{
									Media:    im,
									RandomID: rid,
								}
								if idx == 0 {
									caption := p.Text
									if out, truncated := sanitizeMediaCaptionText(caption); truncated {
										caption = out
									}
									item.Message = caption
								}
								multi = append(multi, item)
							}

							if sendErr == nil && len(multi) == len(group) {
								_, sendErr = api.MessagesSendMultiMedia(ctx, &tg.MessagesSendMultiMediaRequest{
									Peer:       cfg.TargetLinkedPeer,
									ReplyTo:    replyTo,
									MultiMedia: multi,
								})
							}

							if sendErr == nil {
								markForwarded(ids)
								sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
								i = j
								continue
							} else if d, ok := tgerr.AsFloodWait(sendErr); ok {
								if global.Logger != nil {
									global.Logger.Warn(
										"comment floodwait, skipping remaining comments for trunk",
										zap.Uint("task_id", task.ID),
										zap.Int("source_root_id", sourceRootID),
										zap.Duration("wait", d),
										zap.Error(sendErr),
									)
								}
								return
							}
						} else {
							// Reference path.
							if err := m.sendLightPayloadAlbumAsComment(ctx, api, cfg, payloads, targetRootID); err == nil {
								markForwarded(ids)
								sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
								i = j
								continue
							} else if d, ok := tgerr.AsFloodWait(err); ok {
								if global.Logger != nil {
									global.Logger.Warn(
										"comment floodwait, skipping remaining comments for trunk",
										zap.Uint("task_id", task.ID),
										zap.Int("source_root_id", sourceRootID),
										zap.Duration("wait", d),
										zap.Error(err),
									)
								}
								return
							}
						}
					}
				}

				// Fallback: send each item individually (still in order).
				for _, it := range group {
					if err := ctx.Err(); err != nil {
						return
					}

					var payload localdb.LightPayload
					if len(it.LightPayload) > 0 {
						if err := json.Unmarshal(it.LightPayload, &payload); err != nil {
							if global.Logger != nil {
								global.Logger.Warn(
									"decode light payload failed",
									zap.Uint("task_id", task.ID),
									zap.Int("msg_id", it.CommentMsgID),
									zap.Error(err),
								)
							}
							continue
						}
					}

					sendErr := sendOne(it, payload)
					if sendErr != nil {
						if d, ok := tgerr.AsFloodWait(sendErr); ok {
							if global.Logger != nil {
								global.Logger.Warn(
									"comment floodwait, skipping remaining comments for trunk",
									zap.Uint("task_id", task.ID),
									zap.Int("source_root_id", sourceRootID),
									zap.Duration("wait", d),
									zap.Error(sendErr),
								)
							}
							return
						}
						if global.Logger != nil {
							global.Logger.Warn(
								"send comment failed",
								zap.Uint("task_id", task.ID),
								zap.Int("msg_id", it.CommentMsgID),
								zap.Int64("grouped_id", gid),
								zap.Error(sendErr),
							)
						}
						continue
					}

					markForwarded([]int{it.CommentMsgID})
					sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
				}

				i = j
				continue
			}

			// Single message.
			var payload localdb.LightPayload
			if len(item.LightPayload) > 0 {
				if err := json.Unmarshal(item.LightPayload, &payload); err != nil {
					if global.Logger != nil {
						global.Logger.Warn(
							"decode light payload failed",
							zap.Uint("task_id", task.ID),
							zap.Int("msg_id", item.CommentMsgID),
							zap.Error(err),
						)
					}
					i++
					continue
				}
			}

			sendErr := sendOne(item, payload)
			if sendErr != nil {
				if d, ok := tgerr.AsFloodWait(sendErr); ok {
					if global.Logger != nil {
						global.Logger.Warn(
							"comment floodwait, skipping remaining comments for trunk",
							zap.Uint("task_id", task.ID),
							zap.Int("source_root_id", sourceRootID),
							zap.Duration("wait", d),
							zap.Error(sendErr),
						)
					}
					return
				}
				if global.Logger != nil {
					global.Logger.Warn(
						"send comment failed",
						zap.Uint("task_id", task.ID),
						zap.Int("msg_id", item.CommentMsgID),
						zap.Error(sendErr),
					)
				}
				i++
				continue
			}

			markForwarded([]int{item.CommentMsgID})
			sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
			i++
		}

		if last := batch[len(batch)-1].CommentMsgID; last > 0 {
			cursor = last
		} else {
			return
		}
	}
}
