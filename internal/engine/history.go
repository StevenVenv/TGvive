package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
	"my-go-server/pkg/retry"

	"github.com/gotd/td/pool"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"gorm.io/gorm"
)

const (
	defaultHistoryPageSize = 50

	// Throttling between history requests (helps avoid flood limits).
	defaultReqDelayMin = 400 * time.Millisecond
	defaultReqDelayMax = 900 * time.Millisecond

	// Throttling between sends (anti-detection; task can override).
	defaultMsgDelayMin = 250 * time.Millisecond
	defaultMsgDelayMax = 650 * time.Millisecond

	// Retry sending on transient errors (incl. FLOOD_WAIT).
	defaultProcessRetries = 3
	defaultRetryDelayMin  = 800 * time.Millisecond
	defaultRetryDelayMax  = 2500 * time.Millisecond

	// When history_max_id is missing (existing tasks before schema upgrade), we can only do best-effort 追更.
	// We'll scan a recent window to avoid re-sending the entire history.
	defaultCatchUpFallbackWindow = 500
)

// CloneHistory iterates source peer history and sends to target peer using current media pipeline.
// It supports:
//   - order: task.HistoryOrder (old->new / new->old)
//   - resume: task.HistoryCursor
//   - throttling: randomized request/message delays
func (m *TaskManager) CloneHistory(ctx context.Context, api *tg.Client, task model.Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if task.ID == 0 {
		return errors.New("task id is required")
	}

	sourcePeer, err := resolveInputPeer(ctx, api, task.SourceURL)
	if err != nil {
		return fmt.Errorf("resolve source peer %q: %w", task.SourceURL, err)
	}
	targetPeer, err := resolveInputPeer(ctx, api, task.TargetURL)
	if err != nil {
		return fmt.Errorf("resolve target peer %q: %w", task.TargetURL, err)
	}

	return m.CloneHistoryWithPeers(ctx, api, sourcePeer, targetPeer, task, nil, 0, nil)
}

func (m *TaskManager) CloneHistoryWithPeers(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, targetPeer tg.InputPeerClass, task model.Task, kw *keywordPolicy, runID uint64, commentCfg *commentPipelineConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if sourcePeer == nil {
		return errors.New("source peer is nil")
	}
	if targetPeer == nil && strings.TrimSpace(task.PublishType) != "bot" {
		return errors.New("target peer is nil")
	}
	if task.ID == 0 {
		return errors.New("task id is required")
	}

	order := task.HistoryOrder
	if order != model.HistoryOrderNewToOld {
		order = model.HistoryOrderOldToNew
	}

	bounds := parseHistoryBounds(task)
	allowedTypes := normalizeTypeSet(task.ContentTypes.Strings())

	pageSize := defaultHistoryPageSize
	if bounds.MaxMessages > 0 && bounds.MaxMessages < pageSize {
		pageSize = bounds.MaxMessages
	}
	if pageSize <= 0 {
		pageSize = 1
	}

	quota := newTaskQuota(task)

	if runID != 0 {
		if cnt, latestID, err := getRemoteHistoryCountAndLatestID(ctx, api, sourcePeer); err == nil {
			maxID := latestID
			if maxID <= 0 && cnt > 0 {
				maxID = cnt
			}
			if total := estimateHistoryRunTotal(order, bounds, task.HistoryCursor, maxID, task.HistoryMaxID); total > 0 {
				m.setStateTotal(task.ID, runID, total)
			}
		}
	}

	// 追更: when using new->old mode, a completed task typically ends with history_cursor=1 and would not
	// fetch new messages on restart. We use history_max_id as the boundary and catch up newest messages first.
	if order == model.HistoryOrderNewToOld && (task.HistoryMaxID > 0 || task.HistoryCursor > 0) {
		if err := m.catchUpNewMessagesNewToOld(ctx, api, sourcePeer, targetPeer, task, task.HistoryMaxID, bounds, allowedTypes, pageSize, kw, runID, quota, commentCfg); err != nil {
			return err
		}
	}

	cursor := task.HistoryCursor
	if order == model.HistoryOrderOldToNew {
		return m.cloneHistoryOldToNew(ctx, api, sourcePeer, targetPeer, task, cursor, bounds, allowedTypes, pageSize, kw, runID, quota, commentCfg)
	}
	return m.cloneHistoryNewToOld(ctx, api, sourcePeer, targetPeer, task, cursor, bounds, allowedTypes, pageSize, kw, runID, quota, commentCfg)
}

type historyBounds struct {
	MinID       int // inclusive, 0 = no bound
	MaxID       int // inclusive, 0 = no bound
	MaxMessages int // 0 = unlimited
}

func parseHistoryBounds(task model.Task) historyBounds {
	var b historyBounds
	switch task.ScopeType {
	case 2: // 最近 N 条
		ints := extractInts(task.ScopeValue, 1)
		if len(ints) == 1 && ints[0] > 0 {
			b.MaxMessages = ints[0]
		}
	case 4: // ID 范围
		ints := extractInts(task.ScopeValue, 2)
		if len(ints) >= 2 && ints[0] > 0 && ints[1] > 0 {
			start, end := ints[0], ints[1]
			if end < start {
				start, end = end, start
			}
			b.MinID = start
			b.MaxID = end
		}
	}
	return b
}

func recordTaskCounters(m *TaskManager, taskID uint, runID uint64, processedDelta int, successDelta int, failDelta int, rootDelta int, replyDelta int) {
	if m == nil || taskID == 0 || runID == 0 {
		return
	}
	if processedDelta == 0 && successDelta == 0 && failDelta == 0 && rootDelta == 0 && replyDelta == 0 {
		return
	}
	m.recordEx(taskID, runID, 0, processedDelta, successDelta, failDelta, rootDelta, replyDelta, "")
}

func getRemoteHistoryCountAndLatestID(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass) (count int, latestID int, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if api == nil {
		return 0, 0, errors.New("tg api is nil")
	}
	if sourcePeer == nil {
		return 0, 0, errors.New("source peer is nil")
	}

	r, err := getHistoryWithFloodWait(ctx, api, &tg.MessagesGetHistoryRequest{
		Peer:  sourcePeer,
		Limit: 1,
	})
	if err != nil {
		return 0, 0, err
	}

	switch v := r.(type) {
	case *tg.MessagesChannelMessages:
		count = v.Count
	case *tg.MessagesMessagesSlice:
		count = v.Count
	}

	msgs := extractTGMessages(r)
	for _, m2 := range msgs {
		if m2 != nil && m2.ID > latestID {
			latestID = m2.ID
		}
	}
	return count, latestID, nil
}

func estimateHistoryRunTotal(order int, bounds historyBounds, cursor int, maxID int, baselineMaxID int) int {
	if bounds.MaxMessages > 0 {
		return bounds.MaxMessages
	}
	if maxID <= 0 {
		return 0
	}

	minID := bounds.MinID
	if minID <= 0 {
		minID = 1
	}

	if bounds.MaxID > 0 && bounds.MaxID < maxID {
		maxID = bounds.MaxID
	}
	if maxID < minID {
		return 0
	}

	if order == model.HistoryOrderOldToNew {
		effCursor := cursor
		if bounds.MinID > 0 && effCursor < bounds.MinID-1 {
			effCursor = bounds.MinID - 1
		}
		if effCursor < 0 {
			effCursor = 0
		}
		if effCursor >= maxID {
			return 0
		}
		return maxID - effCursor
	}

	// new->old: we will process IDs in [minID, startCursor-1].
	startCursor := cursor
	if startCursor <= 0 {
		startCursor = maxID + 1
	}
	if bounds.MaxID > 0 && startCursor > bounds.MaxID+1 {
		startCursor = bounds.MaxID + 1
	}
	if startCursor <= minID {
		return 0
	}

	baseTotal := startCursor - minID
	catchUpExtra := 0
	if baselineMaxID > 0 && baselineMaxID < maxID {
		catchUpExtra = maxID - baselineMaxID
	}
	return baseTotal + catchUpExtra
}

func getLatestRemoteMessageID(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if api == nil {
		return 0, errors.New("tg api is nil")
	}
	if sourcePeer == nil {
		return 0, errors.New("source peer is nil")
	}

	r, err := getHistoryWithFloodWait(ctx, api, &tg.MessagesGetHistoryRequest{
		Peer:  sourcePeer,
		Limit: 1,
	})
	if err != nil {
		return 0, err
	}
	msgs := extractTGMessages(r)
	maxID := 0
	for _, m := range msgs {
		if m != nil && m.ID > maxID {
			maxID = m.ID
		}
	}
	return maxID, nil
}

func (m *TaskManager) catchUpNewMessagesNewToOld(
	ctx context.Context,
	api *tg.Client,
	sourcePeer tg.InputPeerClass,
	targetPeer tg.InputPeerClass,
	task model.Task,
	baselineMaxID int,
	bounds historyBounds,
	allowedTypes map[string]struct{},
	pageSize int,
	kw *keywordPolicy,
	runID uint64,
	quota *taskQuota,
	commentCfg *commentPipelineConfig,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || api == nil || sourcePeer == nil || task.ID == 0 {
		return nil
	}
	if targetPeer == nil && strings.TrimSpace(task.PublishType) != "bot" {
		return nil
	}

	latestRemoteID, err := getLatestRemoteMessageID(ctx, api, sourcePeer)
	if err != nil || latestRemoteID <= 0 {
		return err
	}

	sinceID := baselineMaxID
	fallback := false
	if sinceID <= 0 {
		fallback = true
		sinceID = latestRemoteID - defaultCatchUpFallbackWindow
		if sinceID < 0 {
			sinceID = 0
		}
	}

	// Keep legacy bounds.MaxID from limiting 追更: we want to continue to newest remote message.
	_ = bounds

	if latestRemoteID <= sinceID {
		return nil
	}

	if runID != 0 {
		if fallback {
			m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("追更: 历史最大ID缺失，按最近窗口回补 (from>%d to=%d)", sinceID, latestRemoteID))
		} else {
			m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("追更: 检测到新消息 (from>%d to=%d)", sinceID, latestRemoteID))
		}
	}

	reqDelayMin, reqDelayMax := defaultReqDelayMin, defaultReqDelayMax

	runtimeTask := task
	curAllowedTypes := allowedTypes
	curAllowFileSuffixes, curBlockFileSuffixes, _ := ResolveFileSuffixRules(task, nil)
	msgDelayMin, msgDelayMax := normalizeDelayRange(runtimeTask.DelayMinMs, runtimeTask.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)

	lastHotRefresh := time.Time{}
	refreshHot := func(now time.Time) {
		st := ResolveRuntimeStrategy(task)
		runtimeTask = MergeHotFieldsIntoTask(task, st)
		curAllowedTypes, _ = ResolveAllowedTypes(task, st)
		curAllowFileSuffixes, curBlockFileSuffixes, _ = ResolveFileSuffixRules(task, st)
		msgDelayMin, msgDelayMax = normalizeDelayRange(runtimeTask.DelayMinMs, runtimeTask.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
		if quota != nil {
			quota.UpdateConfig(runtimeTask.DailyLimit, runtimeTask.RunWindow)
		}
		lastHotRefresh = now
	}
	maybeRefreshHot := func() {
		now := time.Now()
		if lastHotRefresh.IsZero() || now.Sub(lastHotRefresh) >= time.Second {
			refreshHot(now)
		}
	}

	refreshHot(time.Now())

	cursor := sinceID
	includeCursorOnce := false
	if cursor <= 0 {
		cursor = 1
		includeCursorOnce = true
	}

	processed := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cursor >= latestRemoteID {
			return nil
		}

		limit := pageSize
		if limit <= 0 {
			limit = defaultHistoryPageSize
		}
		if limit > defaultHistoryPageSize {
			limit = defaultHistoryPageSize
		}

		// Gate by quota to avoid flood / daily limit violations (追更 is still "history clone").
		maybeRefreshHot()
		if err := m.waitForQuota(ctx, task.ID, runID, quota, 1); err != nil {
			return err
		}

		req := &tg.MessagesGetHistoryRequest{
			Peer:     sourcePeer,
			OffsetID: cursor,
			AddOffset: func() int {
				if includeCursorOnce {
					return -limit + 1
				}
				return -limit
			}(),
			Limit: limit,
			MinID: cursor,
			MaxID: latestRemoteID + 1,
		}
		allowEqual := includeCursorOnce
		includeCursorOnce = false

		r, err := getHistoryWithFloodWait(ctx, api, req)
		if err != nil {
			return err
		}
		msgs := extractTGMessages(r)
		if len(msgs) == 0 {
			return nil
		}

		sort.Slice(msgs, func(i, j int) bool {
			return msgs[i].ID < msgs[j].ID
		})

		advanced := false
		i := 0
		for i < len(msgs) {
			if err := ctx.Err(); err != nil {
				return err
			}
			maybeRefreshHot()

			msg := msgs[i]
			if msg == nil || msg.ID <= 0 {
				i++
				continue
			}
			if msg.ID > latestRemoteID {
				i++
				continue
			}
			if msg.ID < cursor || (!allowEqual && msg.ID == cursor) {
				i++
				continue
			}
			if msg.ID <= sinceID {
				i++
				continue
			}

			// Album grouping (by contiguous GroupedID).
			if msg.GroupedID != 0 && msg.Media != nil {
				gid := msg.GroupedID
				j := i
				var group []*tg.Message
				var maxInGroup int
				for j < len(msgs) {
					next := msgs[j]
					if next == nil || next.GroupedID != gid || next.Media == nil {
						break
					}
					if next.ID > latestRemoteID {
						j++
						continue
					}
					if next.ID < cursor || (!allowEqual && next.ID == cursor) {
						j++
						continue
					}
					if next.ID <= sinceID {
						j++
						continue
					}
					group = append(group, next)
					if next.ID > maxInGroup {
						maxInGroup = next.ID
					}
					j++
				}

				if len(group) > 0 {
					commentEnabled := commentCfg != nil && commentCfg.Enabled && commentCfg.LocalDB != nil
					sentIDs := []int(nil)
					minInGroup := 0
					if commentEnabled {
						for _, gm := range group {
							if gm == nil || gm.ID <= 0 {
								continue
							}
							if minInGroup == 0 || gm.ID < minInGroup {
								minInGroup = gm.ID
							}
						}
					}

					plan := PlanMediaGroup(m, group, curAllowedTypes, curAllowFileSuffixes, curBlockFileSuffixes)
					if plan.Skipped > 0 {
						global.AddFiltered(uint64(plan.Skipped))
					}
					if plan.Need > 0 && kw != nil {
						carrier := plan.Text
						if carrier == nil && len(plan.Media) > 0 {
							carrier = plan.Media[0]
						}
						if carrier != nil {
							if out, skip := applyKeywordPolicyToMessage(carrier, kw); skip {
								global.AddFiltered(uint64(plan.Need))
								cursor = maxInGroup
								if err := persistHistoryMaxID(task.ID, cursor); err != nil {
									return err
								}
								rootDelta, replyDelta := classifyRootReplyBatch(group)
								recordTaskCounters(m, task.ID, runID, len(group), 0, 0, rootDelta, replyDelta)
								processed += len(group)
								advanced = true
								sleepRandom(ctx, msgDelayMin, msgDelayMax)
								i = j
								continue
							} else if out != nil && out != carrier {
								if plan.Text != nil {
									plan.Text = out
								} else if len(plan.Media) > 0 {
									copied := make([]*tg.Message, len(plan.Media))
									copy(copied, plan.Media)
									copied[0] = out
									plan.Media = copied
								}
							}
						}
					}

					if plan.Need > 0 {
						if err := m.waitForQuota(ctx, task.ID, runID, quota, plan.Need); err != nil {
							return err
						}
						if err := processWithRetry(ctx, func() error {
							if plan.Text != nil {
								if commentEnabled {
									ids, serr := m.processSingleMessageResult(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Text)
									if serr != nil {
										return serr
									}
									sentIDs = ids
									return nil
								}
								return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Text)
							}
							if commentEnabled {
								ids, serr := m.processAlbumBatchResult(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Media)
								if serr != nil {
									return serr
								}
								sentIDs = ids
								return nil
							}
							return m.processAlbumBatch(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Media, nil)
						}); err != nil {
							global.AddFail(uint64(plan.Need))
							if errors.Is(err, ErrMediaDownload) {
								if runID != 0 {
									m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("[Error] 追更 Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
								} else {
									global.BroadcastLog(fmt.Sprintf("[Error] 追更 Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
								}
								cursor = maxInGroup
								if err := persistHistoryMaxID(task.ID, cursor); err != nil {
									return err
								}
								rootDelta, replyDelta := classifyRootReplyBatch(group)
								recordTaskCounters(m, task.ID, runID, len(group), 0, plan.Need, rootDelta, replyDelta)
								processed += len(group)
								advanced = true
								sleepRandom(ctx, msgDelayMin, msgDelayMax)
								i = j
								continue
							}
							return err
						}
						global.AddSuccess(uint64(plan.Need))
						if err := m.quotaAdd(ctx, task.ID, quota, plan.Need); err != nil {
							return err
						}

						if commentEnabled && minInGroup > 0 {
							targetID := minPositiveInt(sentIDs)
							if targetID > 0 {
								srcRoot, dstRoot := m.ProduceHistoryCommentsForTrunk(ctx, api, task, commentCfg, sourcePeer, targetPeer, minInGroup, targetID)
								if srcRoot > 0 && dstRoot > 0 {
									m.sendPendingCommentsForRoot(ctx, api, task, commentCfg, srcRoot, dstRoot)
								}
							}
						}
					}
					cursor = maxInGroup
					if err := persistHistoryMaxID(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReplyBatch(group)
					recordTaskCounters(m, task.ID, runID, len(group), plan.Need, 0, rootDelta, replyDelta)
					processed += len(group)
					advanced = true
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
				}

				i = j
				continue
			}

			ct := m.DetectContentType(msg)
			if curAllowedTypes != nil {
				if _, ok := curAllowedTypes[ct]; !ok {
					global.IncFiltered()
					cursor = msg.ID
					if err := persistHistoryMaxID(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					continue
				}
			}
			if ct == "file" && !fileSuffixAllowed(msg, curAllowFileSuffixes, curBlockFileSuffixes) {
				global.IncFiltered()
				cursor = msg.ID
				if err := persistHistoryMaxID(task.ID, cursor); err != nil {
					return err
				}
				rootDelta, replyDelta := classifyRootReply(msg)
				recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
				processed++
				advanced = true
				i++
				continue
			}

			msgToSend := msg
			if kw != nil {
				if out, skip := applyKeywordPolicyToMessage(msg, kw); skip {
					if msg.Media != nil || strings.TrimSpace(msg.Message) != "" {
						global.IncFiltered()
					}
					cursor = msg.ID
					if err := persistHistoryMaxID(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					continue
				} else if out != nil {
					msgToSend = out
				}
			}

			need := quotaSendableCount(m, msgToSend, curAllowedTypes)
			commentEnabled := commentCfg != nil && commentCfg.Enabled && commentCfg.LocalDB != nil
			sentIDs := []int(nil)
			if need > 0 {
				if err := m.waitForQuota(ctx, task.ID, runID, quota, need); err != nil {
					return err
				}
			}
			if err := processWithRetry(ctx, func() error {
				if commentEnabled {
					ids, serr := m.processSingleMessageResult(ctx, api, sourcePeer, targetPeer, runtimeTask, msgToSend)
					if serr != nil {
						return serr
					}
					sentIDs = ids
					return nil
				}
				return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, runtimeTask, msgToSend)
			}); err != nil {
				if need > 0 {
					global.AddFail(uint64(need))
				} else {
					global.IncFail()
				}
				if errors.Is(err, ErrMediaDownload) {
					if runID != 0 {
						m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("[Error] 追更 MsgID %d download failed: %v (skipping)", msg.ID, err))
					} else {
						global.BroadcastLog(fmt.Sprintf("[Error] 追更 MsgID %d download failed: %v (skipping)", msg.ID, err))
					}
					cursor = msg.ID
					if err := persistHistoryMaxID(task.ID, cursor); err != nil {
						return err
					}
					failDelta := need
					if failDelta <= 0 {
						failDelta = 1
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, failDelta, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
					continue
				}
				return err
			}
			if need > 0 {
				global.AddSuccess(uint64(need))
			}
			if need > 0 {
				if err := m.quotaAdd(ctx, task.ID, quota, need); err != nil {
					return err
				}
			}

			if commentEnabled {
				targetID := minPositiveInt(sentIDs)
				if targetID > 0 {
					srcRoot, dstRoot := m.ProduceHistoryCommentsForTrunk(ctx, api, task, commentCfg, sourcePeer, targetPeer, msg.ID, targetID)
					if srcRoot > 0 && dstRoot > 0 {
						m.sendPendingCommentsForRoot(ctx, api, task, commentCfg, srcRoot, dstRoot)
					}
				}
			}

			cursor = msg.ID
			if err := persistHistoryMaxID(task.ID, cursor); err != nil {
				return err
			}
			rootDelta, replyDelta := classifyRootReply(msg)
			recordTaskCounters(m, task.ID, runID, 1, need, 0, rootDelta, replyDelta)
			processed++
			advanced = true
			i++
			sleepRandom(ctx, msgDelayMin, msgDelayMax)
		}

		if !advanced {
			return nil
		}
		sleepRandom(ctx, reqDelayMin, reqDelayMax)

		// Extra safety: avoid infinite loops when Telegram returns overlapping windows.
		if processed > 10_000 {
			return nil
		}
	}
}

func (m *TaskManager) cloneHistoryOldToNew(
	ctx context.Context,
	api *tg.Client,
	sourcePeer tg.InputPeerClass,
	targetPeer tg.InputPeerClass,
	task model.Task,
	cursor int,
	bounds historyBounds,
	allowedTypes map[string]struct{},
	pageSize int,
	kw *keywordPolicy,
	runID uint64,
	quota *taskQuota,
	commentCfg *commentPipelineConfig,
) error {
	reqDelayMin, reqDelayMax := defaultReqDelayMin, defaultReqDelayMax

	runtimeTask := task
	curAllowedTypes := allowedTypes
	curAllowFileSuffixes, curBlockFileSuffixes, _ := ResolveFileSuffixRules(task, nil)
	msgDelayMin, msgDelayMax := normalizeDelayRange(runtimeTask.DelayMinMs, runtimeTask.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)

	lastHotRefresh := time.Time{}
	refreshHot := func(now time.Time) {
		st := ResolveRuntimeStrategy(task)
		runtimeTask = MergeHotFieldsIntoTask(task, st)
		curAllowedTypes, _ = ResolveAllowedTypes(task, st)
		curAllowFileSuffixes, curBlockFileSuffixes, _ = ResolveFileSuffixRules(task, st)
		msgDelayMin, msgDelayMax = normalizeDelayRange(runtimeTask.DelayMinMs, runtimeTask.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
		if quota != nil {
			quota.UpdateConfig(runtimeTask.DailyLimit, runtimeTask.RunWindow)
		}
		lastHotRefresh = now
	}
	maybeRefreshHot := func() {
		now := time.Now()
		if lastHotRefresh.IsZero() || now.Sub(lastHotRefresh) >= time.Second {
			refreshHot(now)
		}
	}

	refreshHot(time.Now())

	if bounds.MinID > 0 && cursor < bounds.MinID-1 {
		cursor = bounds.MinID - 1
	}
	includeCursorOnce := false
	if cursor <= 0 {
		// Start from the very beginning.
		cursor = 1
		includeCursorOnce = true
	}

	processed := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if bounds.MaxMessages > 0 && processed >= bounds.MaxMessages {
			return nil
		}
		if bounds.MaxID > 0 && cursor >= bounds.MaxID {
			return nil
		}

		maybeRefreshHot()
		if err := m.waitForQuota(ctx, task.ID, runID, quota, 1); err != nil {
			return err
		}

		limit := pageSize
		if bounds.MaxMessages > 0 {
			remain := bounds.MaxMessages - processed
			if remain < limit {
				limit = remain
			}
		}
		if limit <= 0 {
			return nil
		}

		req := &tg.MessagesGetHistoryRequest{
			Peer:     sourcePeer,
			OffsetID: cursor,
			// Telegram offset rules: negative add_offset loads messages newer than OffsetID.
			AddOffset: -limit,
			Limit:     limit,
		}

		allowEqual := false
		if includeCursorOnce {
			// Best-effort to include the first message (usually ID=1).
			req.AddOffset = -limit + 1
			req.MinID = 0
			allowEqual = true
			includeCursorOnce = false
		} else {
			req.MinID = cursor
		}
		if bounds.MaxID > 0 {
			req.MaxID = bounds.MaxID + 1
		}

		r, err := getHistoryWithFloodWait(ctx, api, req)
		if err != nil {
			return err
		}
		msgs := extractTGMessages(r)
		if len(msgs) == 0 {
			return nil
		}

		sort.Slice(msgs, func(i, j int) bool {
			return msgs[i].ID < msgs[j].ID
		})

		advanced := false
		i := 0
		for i < len(msgs) {
			if err := ctx.Err(); err != nil {
				return err
			}
			maybeRefreshHot()

			msg := msgs[i]
			if msg == nil || msg.ID <= 0 {
				i++
				continue
			}
			if bounds.MinID > 0 && msg.ID < bounds.MinID {
				i++
				continue
			}
			if bounds.MaxID > 0 && msg.ID > bounds.MaxID {
				i++
				continue
			}
			if msg.ID < cursor || (!allowEqual && msg.ID == cursor) {
				i++
				continue
			}

			// Album grouping (by contiguous GroupedID).
			if msg.GroupedID != 0 && msg.Media != nil {
				gid := msg.GroupedID
				j := i
				var group []*tg.Message
				var maxInGroup int
				for j < len(msgs) {
					next := msgs[j]
					if next == nil || next.GroupedID != gid || next.Media == nil {
						break
					}
					if next.ID < cursor || (!allowEqual && next.ID == cursor) {
						j++
						continue
					}
					if bounds.MinID > 0 && next.ID < bounds.MinID {
						j++
						continue
					}
					if bounds.MaxID > 0 && next.ID > bounds.MaxID {
						j++
						continue
					}
					group = append(group, next)
					if next.ID > maxInGroup {
						maxInGroup = next.ID
					}
					j++
				}

				// Even if filtered out by content types, we still advance cursor to avoid reprocessing.
				if len(group) > 0 {
					commentEnabled := commentCfg != nil && commentCfg.Enabled && commentCfg.LocalDB != nil
					sentIDs := []int(nil)
					minInGroup := 0
					if commentEnabled {
						for _, gm := range group {
							if gm == nil || gm.ID <= 0 {
								continue
							}
							if minInGroup == 0 || gm.ID < minInGroup {
								minInGroup = gm.ID
							}
						}
					}

					plan := PlanMediaGroup(m, group, curAllowedTypes, curAllowFileSuffixes, curBlockFileSuffixes)
					if plan.Skipped > 0 {
						global.AddFiltered(uint64(plan.Skipped))
					}
					if plan.Need > 0 && kw != nil {
						carrier := plan.Text
						if carrier == nil && len(plan.Media) > 0 {
							carrier = plan.Media[0]
						}
						if carrier != nil {
							if out, skip := applyKeywordPolicyToMessage(carrier, kw); skip {
								global.AddFiltered(uint64(plan.Need))
								cursor = maxInGroup
								if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
									return err
								}
								rootDelta, replyDelta := classifyRootReplyBatch(group)
								recordTaskCounters(m, task.ID, runID, len(group), 0, 0, rootDelta, replyDelta)
								processed += len(group)
								advanced = true
								sleepRandom(ctx, msgDelayMin, msgDelayMax)
								i = j
								continue
							} else if out != nil && out != carrier {
								if plan.Text != nil {
									plan.Text = out
								} else if len(plan.Media) > 0 {
									copied := make([]*tg.Message, len(plan.Media))
									copy(copied, plan.Media)
									copied[0] = out
									plan.Media = copied
								}
							}
						}
					}
					if plan.Need > 0 {
						if err := m.waitForQuota(ctx, task.ID, runID, quota, plan.Need); err != nil {
							return err
						}
						if err := processWithRetry(ctx, func() error {
							if plan.Text != nil {
								if commentEnabled {
									ids, serr := m.processSingleMessageResult(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Text)
									if serr != nil {
										return serr
									}
									sentIDs = ids
									return nil
								}
								return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Text)
							}
							if commentEnabled {
								ids, serr := m.processAlbumBatchResult(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Media)
								if serr != nil {
									return serr
								}
								sentIDs = ids
								return nil
							}
							return m.processAlbumBatch(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Media, nil)
						}); err != nil {
							global.AddFail(uint64(plan.Need))
							if errors.Is(err, ErrMediaDownload) {
								if runID != 0 {
									m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("[Error] Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
								} else {
									global.BroadcastLog(fmt.Sprintf("[Error] Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
								}
								cursor = maxInGroup
								if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
									return err
								}
								rootDelta, replyDelta := classifyRootReplyBatch(group)
								recordTaskCounters(m, task.ID, runID, len(group), 0, plan.Need, rootDelta, replyDelta)
								processed += len(group)
								advanced = true
								sleepRandom(ctx, msgDelayMin, msgDelayMax)
								i = j
								continue
							}
							return err
						}
						global.AddSuccess(uint64(plan.Need))
						if err := m.quotaAdd(ctx, task.ID, quota, plan.Need); err != nil {
							return err
						}

						if commentEnabled && minInGroup > 0 {
							targetID := minPositiveInt(sentIDs)
							if targetID > 0 {
								srcRoot, dstRoot := m.ProduceHistoryCommentsForTrunk(ctx, api, task, commentCfg, sourcePeer, targetPeer, minInGroup, targetID)
								if srcRoot > 0 && dstRoot > 0 {
									m.sendPendingCommentsForRoot(ctx, api, task, commentCfg, srcRoot, dstRoot)
								}
							}
						}
					}

					cursor = maxInGroup
					if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReplyBatch(group)
					recordTaskCounters(m, task.ID, runID, len(group), plan.Need, 0, rootDelta, replyDelta)
					processed += len(group)
					advanced = true
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
				}

				i = j
				continue
			}

			ct := m.DetectContentType(msg)
			if curAllowedTypes != nil {
				if _, ok := curAllowedTypes[ct]; !ok {
					global.IncFiltered()
					cursor = msg.ID
					if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					continue
				}
			}
			if ct == "file" && !fileSuffixAllowed(msg, curAllowFileSuffixes, curBlockFileSuffixes) {
				global.IncFiltered()
				cursor = msg.ID
				if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
					return err
				}
				rootDelta, replyDelta := classifyRootReply(msg)
				recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
				processed++
				advanced = true
				i++
				continue
			}

			msgToSend := msg
			if kw != nil {
				if out, skip := applyKeywordPolicyToMessage(msg, kw); skip {
					if msg.Media != nil || strings.TrimSpace(msg.Message) != "" {
						global.IncFiltered()
					}
					cursor = msg.ID
					if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					continue
				} else if out != nil {
					msgToSend = out
				}
			}

			need := quotaSendableCount(m, msgToSend, curAllowedTypes)
			commentEnabled := commentCfg != nil && commentCfg.Enabled && commentCfg.LocalDB != nil
			sentIDs := []int(nil)
			if need > 0 {
				if err := m.waitForQuota(ctx, task.ID, runID, quota, need); err != nil {
					return err
				}
			}
			if err := processWithRetry(ctx, func() error {
				if commentEnabled {
					ids, serr := m.processSingleMessageResult(ctx, api, sourcePeer, targetPeer, runtimeTask, msgToSend)
					if serr != nil {
						return serr
					}
					sentIDs = ids
					return nil
				}
				return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, runtimeTask, msgToSend)
			}); err != nil {
				if need > 0 {
					global.AddFail(uint64(need))
				} else {
					global.IncFail()
				}
				if errors.Is(err, ErrMediaDownload) {
					if runID != 0 {
						m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("[Error] MsgID %d download failed: %v (skipping)", msg.ID, err))
					} else {
						global.BroadcastLog(fmt.Sprintf("[Error] MsgID %d download failed: %v (skipping)", msg.ID, err))
					}
					cursor = msg.ID
					if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
						return err
					}
					failDelta := need
					if failDelta <= 0 {
						failDelta = 1
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, failDelta, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
					continue
				}
				return err
			}
			if need > 0 {
				global.AddSuccess(uint64(need))
			}
			if need > 0 {
				if err := m.quotaAdd(ctx, task.ID, quota, need); err != nil {
					return err
				}
			}

			if commentEnabled {
				targetID := minPositiveInt(sentIDs)
				if targetID > 0 {
					srcRoot, dstRoot := m.ProduceHistoryCommentsForTrunk(ctx, api, task, commentCfg, sourcePeer, targetPeer, msg.ID, targetID)
					if srcRoot > 0 && dstRoot > 0 {
						m.sendPendingCommentsForRoot(ctx, api, task, commentCfg, srcRoot, dstRoot)
					}
				}
			}

			cursor = msg.ID
			if err := persistHistoryCursorAndMax(task.ID, cursor); err != nil {
				return err
			}
			rootDelta, replyDelta := classifyRootReply(msg)
			recordTaskCounters(m, task.ID, runID, 1, need, 0, rootDelta, replyDelta)
			processed++
			advanced = true
			i++
			sleepRandom(ctx, msgDelayMin, msgDelayMax)
		}

		if !advanced {
			return nil
		}
		sleepRandom(ctx, reqDelayMin, reqDelayMax)
	}
}

func (m *TaskManager) cloneHistoryNewToOld(
	ctx context.Context,
	api *tg.Client,
	sourcePeer tg.InputPeerClass,
	targetPeer tg.InputPeerClass,
	task model.Task,
	cursor int,
	bounds historyBounds,
	allowedTypes map[string]struct{},
	pageSize int,
	kw *keywordPolicy,
	runID uint64,
	quota *taskQuota,
	commentCfg *commentPipelineConfig,
) error {
	reqDelayMin, reqDelayMax := defaultReqDelayMin, defaultReqDelayMax

	runtimeTask := task
	curAllowedTypes := allowedTypes
	curAllowFileSuffixes, curBlockFileSuffixes, _ := ResolveFileSuffixRules(task, nil)
	msgDelayMin, msgDelayMax := normalizeDelayRange(runtimeTask.DelayMinMs, runtimeTask.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)

	lastHotRefresh := time.Time{}
	refreshHot := func(now time.Time) {
		st := ResolveRuntimeStrategy(task)
		runtimeTask = MergeHotFieldsIntoTask(task, st)
		curAllowedTypes, _ = ResolveAllowedTypes(task, st)
		curAllowFileSuffixes, curBlockFileSuffixes, _ = ResolveFileSuffixRules(task, st)
		msgDelayMin, msgDelayMax = normalizeDelayRange(runtimeTask.DelayMinMs, runtimeTask.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
		if quota != nil {
			quota.UpdateConfig(runtimeTask.DailyLimit, runtimeTask.RunWindow)
		}
		lastHotRefresh = now
	}
	maybeRefreshHot := func() {
		now := time.Now()
		if lastHotRefresh.IsZero() || now.Sub(lastHotRefresh) >= time.Second {
			refreshHot(now)
		}
	}

	refreshHot(time.Now())

	if cursor <= 0 {
		if bounds.MaxID > 0 {
			// Include MaxID by setting offset_id to max+1.
			cursor = bounds.MaxID + 1
		} else {
			cursor = 0
		}
	}

	maxSeen := task.HistoryMaxID
	persistedMaxSeen := maxSeen
	processed := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if bounds.MaxMessages > 0 && processed >= bounds.MaxMessages {
			return nil
		}
		if bounds.MinID > 0 && cursor <= bounds.MinID {
			return nil
		}

		maybeRefreshHot()
		if err := m.waitForQuota(ctx, task.ID, runID, quota, 1); err != nil {
			return err
		}

		limit := pageSize
		if bounds.MaxMessages > 0 {
			remain := bounds.MaxMessages - processed
			if remain < limit {
				limit = remain
			}
		}
		if limit <= 0 {
			return nil
		}

		req := &tg.MessagesGetHistoryRequest{
			Peer:      sourcePeer,
			OffsetID:  cursor,
			AddOffset: 0,
			Limit:     limit,
		}
		if bounds.MaxID > 0 {
			req.MaxID = bounds.MaxID + 1
		}
		if bounds.MinID > 0 {
			req.MinID = bounds.MinID - 1
		}

		r, err := getHistoryWithFloodWait(ctx, api, req)
		if err != nil {
			return err
		}
		msgs := extractTGMessages(r)
		if len(msgs) == 0 {
			return nil
		}

		// Track max message ID ever seen for 追更 (new->old completion would otherwise leave history_cursor at 1).
		pageMax := 0
		for _, m2 := range msgs {
			if m2 != nil && m2.ID > pageMax {
				pageMax = m2.ID
			}
		}
		if pageMax > maxSeen {
			maxSeen = pageMax
		}
		if maxSeen > persistedMaxSeen {
			if err := persistHistoryMaxID(task.ID, maxSeen); err != nil {
				return err
			}
			persistedMaxSeen = maxSeen
		}

		// Ensure descending order.
		sort.Slice(msgs, func(i, j int) bool {
			return msgs[i].ID > msgs[j].ID
		})

		advanced := false
		i := 0
		for i < len(msgs) {
			if err := ctx.Err(); err != nil {
				return err
			}

			msg := msgs[i]
			if msg == nil || msg.ID <= 0 {
				i++
				continue
			}
			if cursor > 0 && msg.ID >= cursor {
				i++
				continue
			}
			if bounds.MinID > 0 && msg.ID < bounds.MinID {
				i++
				continue
			}
			if bounds.MaxID > 0 && msg.ID > bounds.MaxID {
				i++
				continue
			}

			if msg.GroupedID != 0 && msg.Media != nil {
				gid := msg.GroupedID
				j := i
				var group []*tg.Message
				minInGroup := msg.ID
				for j < len(msgs) {
					next := msgs[j]
					if next == nil || next.GroupedID != gid || next.Media == nil {
						break
					}
					if cursor > 0 && next.ID >= cursor {
						j++
						continue
					}
					if bounds.MinID > 0 && next.ID < bounds.MinID {
						j++
						continue
					}
					if bounds.MaxID > 0 && next.ID > bounds.MaxID {
						j++
						continue
					}
					group = append(group, next)
					if next.ID < minInGroup {
						minInGroup = next.ID
					}
					j++
				}

				if len(group) > 0 {
					commentEnabled := commentCfg != nil && commentCfg.Enabled && commentCfg.LocalDB != nil
					sentIDs := []int(nil)

					plan := PlanMediaGroup(m, group, curAllowedTypes, curAllowFileSuffixes, curBlockFileSuffixes)
					if plan.Skipped > 0 {
						global.AddFiltered(uint64(plan.Skipped))
					}
					if plan.Need > 0 && kw != nil {
						carrier := plan.Text
						if carrier == nil && len(plan.Media) > 0 {
							carrier = plan.Media[0]
						}
						if carrier != nil {
							if out, skip := applyKeywordPolicyToMessage(carrier, kw); skip {
								global.AddFiltered(uint64(plan.Need))
								cursor = minInGroup
								if err := persistHistoryCursor(task.ID, cursor); err != nil {
									return err
								}
								rootDelta, replyDelta := classifyRootReplyBatch(group)
								recordTaskCounters(m, task.ID, runID, len(group), 0, 0, rootDelta, replyDelta)
								processed += len(group)
								advanced = true
								sleepRandom(ctx, msgDelayMin, msgDelayMax)
								i = j
								continue
							} else if out != nil && out != carrier {
								if plan.Text != nil {
									plan.Text = out
								} else if len(plan.Media) > 0 {
									copied := make([]*tg.Message, len(plan.Media))
									copy(copied, plan.Media)
									copied[0] = out
									plan.Media = copied
								}
							}
						}
					}
					if plan.Need > 0 {
						if err := m.waitForQuota(ctx, task.ID, runID, quota, plan.Need); err != nil {
							return err
						}
						if err := processWithRetry(ctx, func() error {
							if plan.Text != nil {
								if commentEnabled {
									ids, serr := m.processSingleMessageResult(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Text)
									if serr != nil {
										return serr
									}
									sentIDs = ids
									return nil
								}
								return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Text)
							}
							if commentEnabled {
								ids, serr := m.processAlbumBatchResult(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Media)
								if serr != nil {
									return serr
								}
								sentIDs = ids
								return nil
							}
							return m.processAlbumBatch(ctx, api, sourcePeer, targetPeer, runtimeTask, plan.Media, nil)
						}); err != nil {
							global.AddFail(uint64(plan.Need))
							if errors.Is(err, ErrMediaDownload) {
								if runID != 0 {
									m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("[Error] Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
								} else {
									global.BroadcastLog(fmt.Sprintf("[Error] Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
								}
								cursor = minInGroup
								if err := persistHistoryCursor(task.ID, cursor); err != nil {
									return err
								}
								rootDelta, replyDelta := classifyRootReplyBatch(group)
								recordTaskCounters(m, task.ID, runID, len(group), 0, plan.Need, rootDelta, replyDelta)
								processed += len(group)
								advanced = true
								sleepRandom(ctx, msgDelayMin, msgDelayMax)
								i = j
								continue
							}
							return err
						}
						global.AddSuccess(uint64(plan.Need))
						if err := m.quotaAdd(ctx, task.ID, quota, plan.Need); err != nil {
							return err
						}

						if commentEnabled {
							targetID := minPositiveInt(sentIDs)
							if targetID > 0 {
								srcRoot, dstRoot := m.ProduceHistoryCommentsForTrunk(ctx, api, task, commentCfg, sourcePeer, targetPeer, minInGroup, targetID)
								if srcRoot > 0 && dstRoot > 0 {
									m.sendPendingCommentsForRoot(ctx, api, task, commentCfg, srcRoot, dstRoot)
								}
							}
						}
					}
					cursor = minInGroup
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReplyBatch(group)
					recordTaskCounters(m, task.ID, runID, len(group), plan.Need, 0, rootDelta, replyDelta)
					processed += len(group)
					advanced = true
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
				}
				i = j
				continue
			}

			ct := m.DetectContentType(msg)
			if curAllowedTypes != nil {
				if _, ok := curAllowedTypes[ct]; !ok {
					global.IncFiltered()
					cursor = msg.ID
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					continue
				}
			}
			if ct == "file" && !fileSuffixAllowed(msg, curAllowFileSuffixes, curBlockFileSuffixes) {
				global.IncFiltered()
				cursor = msg.ID
				if err := persistHistoryCursor(task.ID, cursor); err != nil {
					return err
				}
				rootDelta, replyDelta := classifyRootReply(msg)
				recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
				processed++
				advanced = true
				i++
				continue
			}

			msgToSend := msg
			if kw != nil {
				if out, skip := applyKeywordPolicyToMessage(msg, kw); skip {
					if msg.Media != nil || strings.TrimSpace(msg.Message) != "" {
						global.IncFiltered()
					}
					cursor = msg.ID
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, 0, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					continue
				} else if out != nil {
					msgToSend = out
				}
			}

			need := quotaSendableCount(m, msgToSend, curAllowedTypes)
			commentEnabled := commentCfg != nil && commentCfg.Enabled && commentCfg.LocalDB != nil
			sentIDs := []int(nil)
			if need > 0 {
				if err := m.waitForQuota(ctx, task.ID, runID, quota, need); err != nil {
					return err
				}
			}
			if err := processWithRetry(ctx, func() error {
				if commentEnabled {
					ids, serr := m.processSingleMessageResult(ctx, api, sourcePeer, targetPeer, runtimeTask, msgToSend)
					if serr != nil {
						return serr
					}
					sentIDs = ids
					return nil
				}
				return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, runtimeTask, msgToSend)
			}); err != nil {
				if need > 0 {
					global.AddFail(uint64(need))
				} else {
					global.IncFail()
				}
				if errors.Is(err, ErrMediaDownload) {
					if runID != 0 {
						m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("[Error] MsgID %d download failed: %v (skipping)", msg.ID, err))
					} else {
						global.BroadcastLog(fmt.Sprintf("[Error] MsgID %d download failed: %v (skipping)", msg.ID, err))
					}
					cursor = msg.ID
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					failDelta := need
					if failDelta <= 0 {
						failDelta = 1
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, task.ID, runID, 1, 0, failDelta, rootDelta, replyDelta)
					processed++
					advanced = true
					i++
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
					continue
				}
				return err
			}
			if need > 0 {
				global.AddSuccess(uint64(need))
			}
			if need > 0 {
				if err := m.quotaAdd(ctx, task.ID, quota, need); err != nil {
					return err
				}
			}

			if commentEnabled {
				targetID := minPositiveInt(sentIDs)
				if targetID > 0 {
					srcRoot, dstRoot := m.ProduceHistoryCommentsForTrunk(ctx, api, task, commentCfg, sourcePeer, targetPeer, msg.ID, targetID)
					if srcRoot > 0 && dstRoot > 0 {
						m.sendPendingCommentsForRoot(ctx, api, task, commentCfg, srcRoot, dstRoot)
					}
				}
			}

			cursor = msg.ID
			if err := persistHistoryCursor(task.ID, cursor); err != nil {
				return err
			}
			rootDelta, replyDelta := classifyRootReply(msg)
			recordTaskCounters(m, task.ID, runID, 1, need, 0, rootDelta, replyDelta)
			processed++
			advanced = true
			i++
			sleepRandom(ctx, msgDelayMin, msgDelayMax)
		}

		if !advanced {
			return nil
		}
		sleepRandom(ctx, reqDelayMin, reqDelayMax)
	}
}

func processWithRetry(ctx context.Context, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return nil
	}

	var last error
	for attempt := 1; attempt <= defaultProcessRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := fn()
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrUnsupportedMedia) {
			return nil
		}
		last = err

		// Telegram FLOOD_WAIT_X: gotd helper sleeps the required seconds.
		if ok, _ := tgerr.FloodWait(ctx, err); ok {
			continue
		}

		if attempt >= defaultProcessRetries {
			break
		}

		// Stop retrying on non-retryable RPC "business" errors (e.g. 400).
		if rpcErr, ok := tgerr.As(err); ok && rpcErr != nil {
			// 4xx except 420 (FLOOD_WAIT) is usually non-retryable.
			if rpcErr.Code >= 400 && rpcErr.Code < 500 && rpcErr.Code != 420 {
				break
			}
		}

		// Retry only on likely transient network/server errors.
		retryable := retry.IsRetryableNetErr(err)
		if !retryable {
			if rpcErr, ok := tgerr.As(err); ok && rpcErr != nil {
				if rpcErr.Code >= 500 || rpcErr.Code == 420 {
					retryable = true
				}
				if rpcErr.IsOneOf("RPC_CALL_FAIL", "TIMEOUT", "INTERNAL", "SERVER_ERROR", "SERVICE_UNAVAILABLE") {
					retryable = true
				}
			}
		}
		if !retryable {
			break
		}

		// Exponential backoff + jitter.
		wait := retry.WithJitter(retry.Backoff(attempt, 2*time.Second, 20*time.Second), 0.2)
		_ = retry.Sleep(ctx, wait)
	}

	return last
}

func extractTGMessages(r tg.MessagesMessagesClass) []*tg.Message {
	if r == nil {
		return nil
	}

	var messages tg.MessageClassArray
	switch v := r.(type) {
	case *tg.MessagesMessages:
		messages = v.Messages
	case *tg.MessagesMessagesSlice:
		messages = v.Messages
	case *tg.MessagesChannelMessages:
		messages = v.Messages
	default:
		return nil
	}

	out := make([]*tg.Message, 0, len(messages))
	for _, msg := range messages {
		m, ok := msg.(*tg.Message)
		if !ok || m == nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func getHistoryWithFloodWait(ctx context.Context, api *tg.Client, req *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if req == nil {
		return nil, errors.New("history request is nil")
	}

	for attempt := 0; attempt < 5; attempt++ {
		r, err := api.MessagesGetHistory(ctx, req)
		if err == nil {
			return r, nil
		}
		if ok, ferr := tgerr.FloodWait(ctx, err); ok && ferr != nil {
			continue
		}
		return nil, err
	}

	return nil, errors.New("history request retries exceeded")
}

func resolveInputPeer(ctx context.Context, api *tg.Client, raw string) (tg.InputPeerClass, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty peer")
	}

	// Private channels/supergroups have no username; allow resolving by:
	// - Bot API chat_id: -100123...
	// - private post link: https://t.me/c/<id>/<msg>
	// - tg://privatepost?channel=<id>&post=<msg>
	if ref, ok, err := parsePrivatePeerRef(raw); ok {
		if err != nil {
			return nil, err
		}
		return resolveChannelPeerByID(ctx, api, ref.ChannelID)
	}

	// Bot API basic group chat id is negative (no -100 prefix). Convert to MTProto chat_id.
	// (InputPeerChat doesn't require access_hash.)
	if strings.HasPrefix(raw, "-") {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id < 0 {
			return &tg.InputPeerChat{ChatID: -id}, nil
		}
	}

	s := message.NewSender(api)

	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		peer, err := s.Resolve(raw).AsInputPeer(ctx)
		if err == nil {
			return peer, nil
		}
		last = err

		// FloodWait: wait and retry.
		if ok, _ := tgerr.FloodWait(ctx, err); ok {
			continue
		}

		// Connection dead: pool will recreate; retry shortly.
		if errors.Is(err, pool.ErrConnDead) {
			sleepRandom(ctx, 250*time.Millisecond, 900*time.Millisecond)
			continue
		}

		break
	}

	return nil, last
}

func persistHistoryMaxID(taskID uint, maxID int) error {
	if taskID == 0 {
		return errors.New("task id is required")
	}
	if maxID <= 0 || global.DB == nil {
		return nil
	}
	return global.DB.Model(&model.Task{}).Where("id = ?", taskID).
		Update("history_max_id", gorm.Expr("GREATEST(history_max_id, ?)", maxID)).Error
}

func persistHistoryCursorAndMax(taskID uint, cursor int) error {
	if taskID == 0 {
		return errors.New("task id is required")
	}
	if global.DB == nil {
		return nil
	}
	return global.DB.Model(&model.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"history_cursor": cursor,
			"history_max_id": gorm.Expr("GREATEST(history_max_id, ?)", cursor),
		}).Error
}

func persistHistoryCursor(taskID uint, cursor int) error {
	if taskID == 0 {
		return errors.New("task id is required")
	}
	if global.DB == nil {
		return nil
	}
	return global.DB.Model(&model.Task{}).Where("id = ?", taskID).Update("history_cursor", cursor).Error
}
