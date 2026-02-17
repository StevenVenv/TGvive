package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
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

	// Safety valve if caller chooses to keep going on failures (unused for now).
	maxConsecutiveFails = 10
)

// CloneHistory iterates source peer history and sends to target peer using current media pipeline.
// It supports:
//   - order: task.HistoryOrder (old->new / new->old)
//   - resume: task.HistoryCursor
//   - throttling: randomized request/message delays
//
// NOTE: At this stage, CloneMode=2/3 are supported. CloneMode=1 (forward) is not implemented yet.
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

	return m.CloneHistoryWithPeers(ctx, api, sourcePeer, targetPeer, task, 0)
}

func (m *TaskManager) CloneHistoryWithPeers(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, targetPeer tg.InputPeerClass, task model.Task, runID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if sourcePeer == nil {
		return errors.New("source peer is nil")
	}
	if targetPeer == nil {
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

	cursor := task.HistoryCursor
	if order == model.HistoryOrderOldToNew {
		return m.cloneHistoryOldToNew(ctx, api, sourcePeer, targetPeer, task, cursor, bounds, allowedTypes, pageSize, runID, quota)
	}
	return m.cloneHistoryNewToOld(ctx, api, sourcePeer, targetPeer, task, cursor, bounds, allowedTypes, pageSize, runID, quota)
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
	runID uint64,
	quota *taskQuota,
) error {
	msgDelayMin, msgDelayMax := normalizeDelayRange(task.DelayMinMs, task.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
	reqDelayMin, reqDelayMax := defaultReqDelayMin, defaultReqDelayMax

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
					need := quotaSendableAlbumCount(m, group, allowedTypes)
					if skipped := len(group) - need; skipped > 0 {
						global.AddFiltered(uint64(skipped))
					}
					if need > 0 {
						if err := m.waitForQuota(ctx, task.ID, runID, quota, need); err != nil {
							return err
						}
					}
					if err := processWithRetry(ctx, func() error {
						return m.processAlbumBatch(ctx, api, sourcePeer, targetPeer, task, group, allowedTypes)
					}); err != nil {
						if need > 0 {
							global.AddFail(uint64(need))
						} else {
							global.IncFail()
						}
						if errors.Is(err, ErrMediaDownload) {
							if runID != 0 {
								m.record(task.ID, runID, 0, 0, 0, 0, fmt.Sprintf("[Error] Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
							} else {
								global.BroadcastLog(fmt.Sprintf("[Error] Album(GroupedID=%d) download failed: %v (skipping)", gid, err))
							}
							cursor = maxInGroup
							if err := persistHistoryCursor(task.ID, cursor); err != nil {
								return err
							}
							processed += len(group)
							advanced = true
							sleepRandom(ctx, msgDelayMin, msgDelayMax)
							i = j
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
					cursor = maxInGroup
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					processed += len(group)
					advanced = true
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
				}

				i = j
				continue
			}

			if allowedTypes != nil {
				ct := m.DetectContentType(msg)
				if _, ok := allowedTypes[ct]; !ok {
					global.IncFiltered()
					cursor = msg.ID
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					processed++
					advanced = true
					i++
					continue
				}
			}

			need := quotaSendableCount(m, msg, allowedTypes)
			if need > 0 {
				if err := m.waitForQuota(ctx, task.ID, runID, quota, need); err != nil {
					return err
				}
			}
			if err := processWithRetry(ctx, func() error {
				return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, task, msg)
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
			cursor = msg.ID
			if err := persistHistoryCursor(task.ID, cursor); err != nil {
				return err
			}
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
	runID uint64,
	quota *taskQuota,
) error {
	msgDelayMin, msgDelayMax := normalizeDelayRange(task.DelayMinMs, task.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
	reqDelayMin, reqDelayMax := defaultReqDelayMin, defaultReqDelayMax

	if cursor <= 0 {
		if bounds.MaxID > 0 {
			// Include MaxID by setting offset_id to max+1.
			cursor = bounds.MaxID + 1
		} else {
			cursor = 0
		}
	}

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
					need := quotaSendableAlbumCount(m, group, allowedTypes)
					if skipped := len(group) - need; skipped > 0 {
						global.AddFiltered(uint64(skipped))
					}
					if need > 0 {
						if err := m.waitForQuota(ctx, task.ID, runID, quota, need); err != nil {
							return err
						}
					}
					if err := processWithRetry(ctx, func() error {
						return m.processAlbumBatch(ctx, api, sourcePeer, targetPeer, task, group, allowedTypes)
					}); err != nil {
						if need > 0 {
							global.AddFail(uint64(need))
						} else {
							global.IncFail()
						}
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
							processed += len(group)
							advanced = true
							sleepRandom(ctx, msgDelayMin, msgDelayMax)
							i = j
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
					cursor = minInGroup
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					processed += len(group)
					advanced = true
					sleepRandom(ctx, msgDelayMin, msgDelayMax)
				}
				i = j
				continue
			}

			if allowedTypes != nil {
				ct := m.DetectContentType(msg)
				if _, ok := allowedTypes[ct]; !ok {
					global.IncFiltered()
					cursor = msg.ID
					if err := persistHistoryCursor(task.ID, cursor); err != nil {
						return err
					}
					processed++
					advanced = true
					i++
					continue
				}
			}

			need := quotaSendableCount(m, msg, allowedTypes)
			if need > 0 {
				if err := m.waitForQuota(ctx, task.ID, runID, quota, need); err != nil {
					return err
				}
			}
			if err := processWithRetry(ctx, func() error {
				return m.processSingleMessage(ctx, api, sourcePeer, targetPeer, task, msg)
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
			cursor = msg.ID
			if err := persistHistoryCursor(task.ID, cursor); err != nil {
				return err
			}
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
	var last error
	for attempt := 0; attempt < defaultProcessRetries; attempt++ {
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
		if ok, _ := tgerr.FloodWait(ctx, err); ok {
			continue
		}
		if attempt < defaultProcessRetries-1 {
			sleepRandom(ctx, defaultRetryDelayMin, defaultRetryDelayMax)
		}
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

	s := message.NewSender(api)
	return s.Resolve(raw).AsInputPeer(ctx)
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
