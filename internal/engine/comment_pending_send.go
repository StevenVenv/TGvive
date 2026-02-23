package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// sendPendingCommentsForRoot sends all pending comments for a given source discussion root, in ascending msg id order.
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
	if sourceRootID <= 0 || targetRootID <= 0 {
		return
	}
	if cfg.SourceLinkedPeer == nil {
		return
	}

	pub := m.getPublisher(task.ID)
	useBot := pub != nil && pub.isBot()
	botChatID := ""
	botPub := (*taskPublisherRuntime)(nil)
	if useBot {
		if cfg.TargetLinkedChatID == 0 {
			return
		}
		botPub = pub
		if cfg.TargetLinkedChatID < 0 {
			botChatID = fmt.Sprintf("%d", cfg.TargetLinkedChatID)
		} else {
			botChatID = fmt.Sprintf("-100%d", cfg.TargetLinkedChatID)
		}
		if strings.TrimSpace(botChatID) == "" {
			return
		}
	} else {
		if cfg.TargetLinkedPeer == nil {
			return
		}
	}

	cfg.sendMu.Lock()
	defer cfg.sendMu.Unlock()

	db := cfg.LocalDB

	wmRule, wmEnabled := watermarkRuleForTask(task)

	st := ResolveRuntimeStrategy(task)
	rule, enabled, _ := resolveCommentRule(task, st)
	if !enabled {
		return
	}

	trustedSet := make(map[int64]struct{}, len(rule.TrustedUserIDs))
	for _, id := range rule.TrustedUserIDs {
		if id > 0 {
			trustedSet[id] = struct{}{}
		}
	}

	typeList := normalizeRuntimeTypeList(rule.AllowedTypes)
	allowedSet := map[string]struct{}(nil)
	if len(typeList) > 0 {
		allowedSet = normalizeTypeSet(typeList)
	}
	blockLower := lowerKeywordList(rule.BlockKeywords)
	kw := (*keywordPolicy)(nil)
	if cfg != nil {
		kw = cfg.Keyword
	}

	normalizePayloadMeta := func(p *localdb.LightPayload) bool {
		if p == nil {
			return false
		}
		filterMode := strings.ToLower(strings.TrimSpace(rule.FilterMode))
		allowAllIdentity := filterMode == "all"
		if filterMode == "whitelist" {
			filterMode = "owner_only"
		}

		allowAnonymous := rule.AllowAnonymous
		if !allowAllIdentity {
			if filterMode != "owner_only" && filterMode != "owner_or_linked" {
				filterMode = "owner_only"
			}
			if filterMode == "owner_or_linked" {
				allowAnonymous = true
			}
		}

		senderType := strings.ToLower(strings.TrimSpace(p.SenderType))
		senderID := p.SenderID
		if senderType == "" || senderType == "unknown" {
			// Backward compatibility: old/corrupted payload may miss sender info.
			// Treat it as "send as linked discussion group" identity.
			if cfg.SourceLinkedChatID != 0 {
				senderType = "channel"
				senderID = cfg.SourceLinkedChatID
			}
		}

		if !allowAllIdentity {
			switch senderType {
			case "channel":
				if senderID != 0 && (senderID == cfg.SourceChannelID || senderID == cfg.SourceLinkedChatID) {
					// Channel itself or linked discussion group identity.
					break
				}
				return false
			case "user":
				if allowAnonymous && senderID == groupAnonymousBotID {
					break
				}
				if trustedSet != nil {
					if _, ok := trustedSet[senderID]; ok {
						break
					}
				}
				return false
			default:
				return false
			}
		}

		if allowedSet != nil {
			ct := strings.ToLower(strings.TrimSpace(p.MediaType))
			if ct == "" {
				ct = "other"
			}
			if _, ok := allowedSet[ct]; !ok {
				return false
			}
		}

		return true
	}

	applyTextPolicy := func(p *localdb.LightPayload) bool {
		if p == nil {
			return false
		}

		// Keyword profile (block/allow/replace).
		if kw != nil {
			if kw.shouldSkip(p.Text) {
				return false
			}
			if out, changed := kw.replaceText(p.Text); changed {
				p.Text = out
			}
		}

		if hitBlockKeywords(p.Text, blockLower) {
			return false
		}
		return true
	}

	normalizePayload := func(p *localdb.LightPayload) bool {
		if !normalizePayloadMeta(p) {
			return false
		}
		return applyTextPolicy(p)
	}

	pendingBefore := int64(0)
	_ = db.Model(&localdb.CommentQueue{}).
		Where("reply_to_root_id = ? AND status = ?", sourceRootID, localdb.CommentStatusPending).
		Count(&pendingBefore).Error
	if pendingBefore <= 0 {
		return
	}

	publisher := "account"
	if useBot {
		publisher = "bot"
	}

	attempted := 0
	sentOK := 0
	markedFailed := 0
	sendErrCnt := 0

	defer func() {
		if ctx.Err() != nil {
			return
		}
		if attempted == 0 && sentOK == 0 && markedFailed == 0 && sendErrCnt == 0 {
			return
		}
		pendingAfter := int64(0)
		_ = db.Model(&localdb.CommentQueue{}).
			Where("reply_to_root_id = ? AND status = ?", sourceRootID, localdb.CommentStatusPending).
			Count(&pendingAfter).Error
		recordTaskDetailFromCtx(
			ctx,
			fmt.Sprintf(
				"评论发送(%s): source_root=%d target_root=%d pending=%d attempt=%d ok=%d failed=%d send_err=%d left=%d",
				publisher,
				sourceRootID,
				targetRootID,
				pendingBefore,
				attempted,
				sentOK,
				markedFailed,
				sendErrCnt,
				pendingAfter,
			),
		)
	}()

	markFailed := func(ids []int) {
		if len(ids) == 0 {
			return
		}
		markedFailed += len(ids)
		_ = db.Model(&localdb.CommentQueue{}).
			Where("reply_to_root_id = ? AND msg_id IN ? AND status = ?", sourceRootID, ids, localdb.CommentStatusPending).
			Update("status", localdb.CommentStatusFailed).Error
	}

	markSuccessWithCAS := func(item localdb.CommentQueue, targetMsgID int) (casOK bool) {
		if item.MsgID <= 0 || targetMsgID <= 0 {
			return false
		}

		res := db.Model(&localdb.CommentQueue{}).
			Where("msg_id = ? AND reply_to_root_id = ? AND status = ? AND updated_at = ?", item.MsgID, sourceRootID, localdb.CommentStatusPending, item.UpdatedAt).
			Updates(map[string]any{
				"status":        localdb.CommentStatusSuccess,
				"target_msg_id": targetMsgID,
			})
		if res.Error != nil {
			return false
		}
		if res.RowsAffected == 1 {
			return true
		}

		// Payload changed while sending (or timestamp precision mismatch). Write success without clobbering payload.
		_ = db.Model(&localdb.CommentQueue{}).
			Where("msg_id = ? AND reply_to_root_id = ? AND status = ?", item.MsgID, sourceRootID, localdb.CommentStatusPending).
			Updates(map[string]any{
				"status":        localdb.CommentStatusSuccess,
				"target_msg_id": targetMsgID,
			}).Error
		return false
	}

	resolveReplyToTargetMsgID := func(payload localdb.LightPayload) int {
		srcReplyID := payload.ReplyToMsgID
		if srcReplyID <= 0 || srcReplyID == sourceRootID {
			return targetRootID
		}

		var parent localdb.CommentQueue
		if err := db.
			Select("target_msg_id", "grouped_id").
			Where(
				"msg_id = ? AND reply_to_root_id = ? AND status = ?",
				srcReplyID,
				sourceRootID,
				localdb.CommentStatusSuccess,
			).
			First(&parent).Error; err != nil {
			return targetRootID
		}

		if parent.GroupedID != 0 {
			var first localdb.CommentQueue
			if err := db.
				Select("target_msg_id").
				Where(
					"reply_to_root_id = ? AND grouped_id = ? AND status = ? AND target_msg_id > 0",
					sourceRootID,
					parent.GroupedID,
					localdb.CommentStatusSuccess,
				).
				Order("msg_id ASC").
				First(&first).Error; err == nil && first.TargetMsgID > 0 {
				return first.TargetMsgID
			}
		}

		if parent.TargetMsgID > 0 {
			return parent.TargetMsgID
		}
		return targetRootID
	}

	sendWatermarkedImage := func(sourceCommentMsgID int, payload localdb.LightPayload, replyToMsgID int) (int, error) {
		if sourceCommentMsgID <= 0 {
			return 0, errors.New("source_comment_msg_id is required")
		}
		if replyToMsgID <= 0 {
			return 0, errors.New("reply_to_msg_id is required")
		}
		srcBytes, fullMsg, err := downloadMessageMediaBytes(ctx, api, cfg.SourceLinkedPeer, sourceCommentMsgID)
		if err != nil {
			return 0, err
		}
		if fullMsg == nil || !isWatermarkableImageMessage(fullMsg) {
			return 0, errors.New("message is not watermarkable")
		}

		outBytes, err := wm.ApplyWatermark(srcBytes, wmRule)
		if err != nil {
			return 0, err
		}
		inputFile, err := uploadBytes(ctx, api, "wm.jpg", outBytes)
		if err != nil {
			return 0, err
		}

		spoiler, ttl := messageSpoilerTTL(fullMsg)
		rid, err := randomID()
		if err != nil {
			return 0, err
		}

		replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToMsgID}
		caption := payload.Text
		if out, truncated := sanitizeMediaCaptionText(caption); truncated {
			caption = out
		}
		upd, err := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
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
		return minPositiveInt(extractSentMsgIDs(upd)), err
	}

	sendOne := func(item localdb.CommentQueue, payload localdb.LightPayload) (int, error) {
		replyToMsgID := resolveReplyToTargetMsgID(payload)

		// Bot publisher: send comment to target linked chat using Bot API.
		if useBot && botPub != nil {
			// Fast path: text-only (or caption-only when media unsupported).
			if len(payload.MediaBytes) == 0 && (payload.MediaType == "" || strings.EqualFold(payload.MediaType, "text")) {
				return botSendText(ctx, botPub.Bot, botChatID, payload.Text, replyToMsgID)
			}

			fullMsg, rerr := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, item.MsgID)
			if rerr != nil {
				return 0, rerr
			}
			if fullMsg == nil {
				return botSendText(ctx, botPub.Bot, botChatID, payload.Text, replyToMsgID)
			}
			if fullMsg.Media == nil {
				return botSendText(ctx, botPub.Bot, botChatID, payload.Text, replyToMsgID)
			}

			pubCopy := *botPub
			pubCopy.ChatID = botChatID
			msgToSend := fullMsg
			if payload.Text != fullMsg.Message {
				cp := *fullMsg
				cp.Message = payload.Text
				cp.Entities = nil
				msgToSend = &cp
			}
			return m.botSendUploadedMediaFromTGMessageResult(ctx, api, cfg.SourceLinkedPeer, msgToSend, task, &pubCopy, replyToMsgID)
		}

		// MTProto publisher.
		if wmEnabled && strings.EqualFold(payload.MediaType, "image") {
			if id, err := sendWatermarkedImage(item.MsgID, payload, replyToMsgID); err == nil {
				return id, nil
			} else if _, ok := tgerr.AsFloodWait(err); ok {
				return 0, err
			}
		}

		ids, sendErr := m.sendLightPayloadAsComment(ctx, api, cfg, payload, replyToMsgID)
		if sendErr != nil {
			// Upload fallback: refetch original message and send with fallback.
			if fullMsg, rerr := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, item.MsgID); rerr == nil && fullMsg != nil {
				ids, sendErr = m.sendCommentWithFallback(ctx, api, cfg.SourceLinkedPeer, cfg.TargetLinkedPeer, fullMsg, replyToMsgID, task)
			}
		}
		return minPositiveInt(ids), sendErr
	}

	cursor := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		var batch []localdb.CommentQueue
		q := db.Where("reply_to_root_id = ? AND status = ?", sourceRootID, localdb.CommentStatusPending)
		if cursor > 0 {
			q = q.Where("msg_id > ?", cursor)
		}
		if err := q.Order("msg_id ASC").Limit(commentScanBatchSize).Find(&batch).Error; err != nil {
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
		if tail := batch[len(batch)-1]; tail.GroupedID != 0 && tail.MsgID > 0 {
			gid := tail.GroupedID
			last := tail.MsgID
			for {
				var extra []localdb.CommentQueue
				if err := db.
					Where("reply_to_root_id = ? AND status = ? AND grouped_id = ? AND msg_id > ?", sourceRootID, localdb.CommentStatusPending, gid, last).
					Order("msg_id ASC").
					Limit(commentScanBatchSize).
					Find(&extra).Error; err != nil {
					break
				}
				if len(extra) == 0 {
					break
				}
				batch = append(batch, extra...)
				last = extra[len(extra)-1].MsgID
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
			if item.MsgID <= 0 || item.ReplyToRootID != sourceRootID || item.Status != localdb.CommentStatusPending {
				i++
				continue
			}

			// Grouped media (album) support.
			if item.GroupedID != 0 {
				gid := item.GroupedID
				j := i
				group := make([]localdb.CommentQueue, 0, 8)
				for j < len(batch) {
					it := batch[j]
					if it.MsgID <= 0 || it.ReplyToRootID != sourceRootID || it.Status != localdb.CommentStatusPending || it.GroupedID != gid {
						break
					}
					group = append(group, it)
					j++
				}

				type albumItem struct {
					rec     localdb.CommentQueue
					payload localdb.LightPayload
				}

				filteredIDs := make([]int, 0, 4)
				sendItems := make([]albumItem, 0, len(group))
				needWM := false
				okAlbum := true

				for _, it := range group {
					var p localdb.LightPayload
					if len(it.Payload) > 0 {
						if err := json.Unmarshal(it.Payload, &p); err != nil {
							filteredIDs = append(filteredIDs, it.MsgID)
							okAlbum = false
							continue
						}
					}

					if !normalizePayloadMeta(&p) {
						filteredIDs = append(filteredIDs, it.MsgID)
						continue
					}

					if wmEnabled && strings.EqualFold(p.MediaType, "image") {
						needWM = true
					} else if len(p.MediaBytes) == 0 {
						okAlbum = false
					}

					sendItems = append(sendItems, albumItem{rec: it, payload: p})
				}

				if len(filteredIDs) > 0 {
					markFailed(filteredIDs)
				}
				if len(sendItems) == 0 {
					i = j
					continue
				}

				// Apply keyword policies on the album caption only.
				captionIdx := 0
				for idx, it := range sendItems {
					if strings.TrimSpace(it.payload.Text) != "" {
						captionIdx = idx
						break
					}
				}
				if !applyTextPolicy(&sendItems[captionIdx].payload) {
					toFail := make([]int, 0, len(sendItems))
					for _, it := range sendItems {
						toFail = append(toFail, it.rec.MsgID)
					}
					markFailed(toFail)
					i = j
					continue
				}
				captionText := sendItems[captionIdx].payload.Text
				for idx := range sendItems {
					if idx == 0 {
						sendItems[0].payload.Text = captionText
					} else {
						sendItems[idx].payload.Text = ""
					}
				}

				replyCarrier := sendItems[0].payload
				for _, it := range sendItems {
					if it.payload.ReplyToMsgID > 0 && it.payload.ReplyToMsgID != sourceRootID {
						replyCarrier = it.payload
						break
					}
				}
				replyToMsgID := resolveReplyToTargetMsgID(replyCarrier)

				attempted += len(sendItems)

				// Try preserve album grouping when possible.
				if okAlbum && len(sendItems) >= 2 {
					targetIDs := []int(nil)
					sendErr := error(nil)

					if useBot && botPub != nil {
						msgsToSend := make([]*tg.Message, 0, len(sendItems))
						for _, it := range sendItems {
							fullMsg, err := refreshMessageForDownload(ctx, api, cfg.SourceLinkedPeer, it.rec.MsgID)
							if err != nil {
								sendErr = err
								break
							}
							if fullMsg == nil || fullMsg.Media == nil {
								sendErr = errors.New("album item missing media")
								break
							}
							method, _, _ := botSingleMethodForMessage(fullMsg)
							if method == "" {
								sendErr = errors.New("album item unsupported by bot api")
								break
							}
							msgsToSend = append(msgsToSend, fullMsg)
						}

						if sendErr == nil && len(msgsToSend) == len(sendItems) {
							pubCopy := *botPub
							pubCopy.ChatID = botChatID

							// Use normalized caption (keyword replace) for albums.
							toSend := msgsToSend
							if len(toSend) > 0 && toSend[0] != nil && sendItems[0].payload.Text != toSend[0].Message {
								cp := *toSend[0]
								cp.Message = sendItems[0].payload.Text
								cp.Entities = nil
								copied := make([]*tg.Message, len(toSend))
								copy(copied, toSend)
								copied[0] = &cp
								toSend = copied
							}

							ids, err := m.botSendUploadedAlbumFromTGMessagesResult(ctx, api, cfg.SourceLinkedPeer, toSend, task, &pubCopy, replyToMsgID)
							if err == nil {
								targetIDs = ids
							}
							sendErr = err
						}
					} else if needWM {
						replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToMsgID}
						multi := make([]tg.InputSingleMedia, 0, len(sendItems))

						for idx, it := range sendItems {
							p := it.payload

							im := tg.InputMediaClass(nil)
							if wmEnabled && strings.EqualFold(p.MediaType, "image") {
								// Best-effort watermark; if it fails, fallback to per-item send.
								srcBytes, fullMsg, err := downloadMessageMediaBytes(ctx, api, cfg.SourceLinkedPeer, it.rec.MsgID)
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

						if sendErr == nil && len(multi) == len(sendItems) {
							upd, err := api.MessagesSendMultiMedia(ctx, &tg.MessagesSendMultiMediaRequest{
								Peer:       cfg.TargetLinkedPeer,
								ReplyTo:    replyTo,
								MultiMedia: multi,
							})
							targetIDs = extractSentMsgIDs(upd)
							sendErr = err
						}
					} else {
						payloads := make([]localdb.LightPayload, 0, len(sendItems))
						for _, it := range sendItems {
							payloads = append(payloads, it.payload)
						}
						targetIDs, sendErr = m.sendLightPayloadAlbumAsComment(ctx, api, cfg, payloads, replyToMsgID)
					}

					if sendErr == nil {
						sentOK += len(sendItems)
						if len(targetIDs) == len(sendItems) {
							for idx, it := range sendItems {
								casOK := markSuccessWithCAS(it.rec, targetIDs[idx])
								if !casOK && !useBot {
									m.submitCommentEdit(ctx, api, task.ID, cfg, it.rec.MsgID, targetIDs[idx])
								}
							}
						} else {
							// Sent but cannot map ids reliably; mark success without mapping to avoid duplicates.
							for _, it := range sendItems {
								_ = db.Model(&localdb.CommentQueue{}).
									Where("msg_id = ? AND reply_to_root_id = ? AND status = ?", it.rec.MsgID, sourceRootID, localdb.CommentStatusPending).
									Update("status", localdb.CommentStatusSuccess).Error
							}
						}

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
				}

				// Fallback: send each item individually (still in order).
				for _, it := range sendItems {
					if err := ctx.Err(); err != nil {
						return
					}

					targetID, sendErr := sendOne(it.rec, it.payload)
					if sendErr != nil {
						sendErrCnt++
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
								zap.Int("msg_id", it.rec.MsgID),
								zap.Int64("grouped_id", gid),
								zap.Error(sendErr),
							)
						}
						continue
					}

					if targetID > 0 {
						sentOK++
						casOK := markSuccessWithCAS(it.rec, targetID)
						if !casOK && !useBot {
							m.submitCommentEdit(ctx, api, task.ID, cfg, it.rec.MsgID, targetID)
						}
						sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
					} else {
						markFailed([]int{it.rec.MsgID})
					}
				}

				i = j
				continue
			}

			// Single message.
			var payload localdb.LightPayload
			if len(item.Payload) > 0 {
				if err := json.Unmarshal(item.Payload, &payload); err != nil {
					markFailed([]int{item.MsgID})
					i++
					continue
				}
			}

			if !normalizePayload(&payload) {
				markFailed([]int{item.MsgID})
				i++
				continue
			}

			attempted++
			targetID, sendErr := sendOne(item, payload)
			if sendErr != nil {
				sendErrCnt++
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
						zap.Int("msg_id", item.MsgID),
						zap.Error(sendErr),
					)
				}
				i++
				continue
			}

			if targetID > 0 {
				sentOK++
				casOK := markSuccessWithCAS(item, targetID)
				if !casOK && !useBot {
					m.submitCommentEdit(ctx, api, task.ID, cfg, item.MsgID, targetID)
				}
				sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
			} else {
				markFailed([]int{item.MsgID})
			}
			i++
		}

		if last := batch[len(batch)-1].MsgID; last > 0 {
			cursor = last
		} else {
			return
		}
	}
}
