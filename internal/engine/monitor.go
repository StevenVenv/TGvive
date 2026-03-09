package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"
	"my-go-server/pkg/retry"

	"github.com/gotd/td/pool"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

type telegramRuntimeManager struct {
	mu       sync.Mutex
	runtimes map[string]*telegramRuntime // key: sessionPath
}

func newTelegramRuntimeManager() *telegramRuntimeManager {
	return &telegramRuntimeManager{
		runtimes: make(map[string]*telegramRuntime),
	}
}

func (rm *telegramRuntimeManager) getOrCreate(sessionPath string) *telegramRuntime {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return nil
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()
	rt := rm.runtimes[sessionPath]
	if rt == nil {
		rt = newTelegramRuntime(sessionPath)
		rm.runtimes[sessionPath] = rt
	}
	return rt
}

func (rm *telegramRuntimeManager) snapshot() []*telegramRuntime {
	if rm == nil {
		return nil
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	out := make([]*telegramRuntime, 0, len(rm.runtimes))
	for _, rt := range rm.runtimes {
		if rt != nil {
			out = append(out, rt)
		}
	}
	return out
}

func (rm *telegramRuntimeManager) stopAndDelete(sessionPath string) {
	if rm == nil {
		return
	}
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return
	}

	rm.mu.Lock()
	rt := rm.runtimes[sessionPath]
	delete(rm.runtimes, sessionPath)
	rm.mu.Unlock()

	if rt != nil {
		rt.shutdown()
	}
}

type realtimeJobKind uint8

const (
	realtimeJobSingle realtimeJobKind = iota
	realtimeJobAlbum
)

type realtimeJob struct {
	kind      realtimeJobKind
	fromPull  bool
	msg       *tg.Message
	groupedID int64
	albumCh   <-chan []*tg.Message
}

type runtimeTaskConfig struct {
	Task            model.Task
	RunID           uint64
	Ctx             context.Context
	SourcePeer      tg.InputPeerClass
	TargetPeer      tg.InputPeerClass
	Keyword         *keywordPolicy
	PollIntervalSec int
	Comment         *commentPipelineConfig
}

type runtimeTask struct {
	Task       model.Task
	RunID      uint64
	Ctx        context.Context
	SourcePeer tg.InputPeerClass
	TargetPeer tg.InputPeerClass

	allowedTypes      map[string]struct{}
	allowedTypesKey   string
	allowFileSuffixes []string
	blockFileSuffixes []string
	fileSuffixKey     string
	delayMin          time.Duration
	delayMax          time.Duration
	keyword           *keywordPolicy
	pollInterval      time.Duration

	comment *commentPipelineConfig

	quota *taskQuota

	queue    chan realtimeJob
	done     chan struct{}
	stopOnce sync.Once

	mu        sync.Mutex
	albumWait map[int64]chan []*tg.Message
}

func newRuntimeTask(cfg runtimeTaskConfig) *runtimeTask {
	allowSuffixes, blockSuffixes, suffixKey := ResolveFileSuffixRules(cfg.Task, nil)

	t := &runtimeTask{
		Task:       cfg.Task,
		RunID:      cfg.RunID,
		Ctx:        cfg.Ctx,
		SourcePeer: cfg.SourcePeer,
		TargetPeer: cfg.TargetPeer,

		allowedTypes:      normalizeTypeSet(cfg.Task.ContentTypes.Strings()),
		allowFileSuffixes: allowSuffixes,
		blockFileSuffixes: blockSuffixes,
		fileSuffixKey:     suffixKey,
		delayMin:          defaultMsgDelayMin,
		delayMax:          defaultMsgDelayMax,
		keyword:           cfg.Keyword,
		comment:           cfg.Comment,
		quota:             newTaskQuota(cfg.Task),

		queue:     make(chan realtimeJob, 512),
		done:      make(chan struct{}),
		albumWait: make(map[int64]chan []*tg.Message),
	}

	t.delayMin, t.delayMax = normalizeDelayRange(cfg.Task.DelayMinMs, cfg.Task.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
	if cfg.PollIntervalSec > 0 {
		sec := cfg.PollIntervalSec
		if sec < 10 {
			sec = 10
		}
		t.pollInterval = time.Duration(sec) * time.Second
	}
	return t
}

func (t *runtimeTask) refreshStrategySnapshot() model.Task {
	if t == nil {
		return model.Task{}
	}

	base := t.Task
	strategy := ResolveRuntimeStrategy(base)
	hotTask := MergeHotFieldsIntoTask(base, strategy)
	types, key := ResolveAllowedTypes(base, strategy)
	allowSuffixes, blockSuffixes, suffixKey := ResolveFileSuffixRules(base, strategy)

	if key != t.allowedTypesKey {
		t.allowedTypesKey = key
		t.allowedTypes = types
	}
	if suffixKey != t.fileSuffixKey {
		t.fileSuffixKey = suffixKey
		t.allowFileSuffixes = allowSuffixes
		t.blockFileSuffixes = blockSuffixes
	}

	t.delayMin, t.delayMax = normalizeDelayRange(hotTask.DelayMinMs, hotTask.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
	return hotTask
}

func (t *runtimeTask) stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() {
		close(t.done)

		t.mu.Lock()
		for gid, ch := range t.albumWait {
			delete(t.albumWait, gid)
			if ch != nil {
				close(ch)
			}
		}
		t.mu.Unlock()
	})
}

func (t *runtimeTask) enqueue(job realtimeJob) bool {
	if t == nil {
		return false
	}
	if t.Ctx == nil || t.Ctx.Err() != nil {
		return false
	}

	select {
	case <-t.done:
		return false
	case t.queue <- job:
		if global.Stats != nil {
			global.Stats.AddPending(1)
		}
		return true
	default:
		if global.Logger != nil {
			global.Logger.Warn("realtime queue full, dropping message", zap.Uint("task_id", t.Task.ID))
		}
		return false
	}
}

func (t *runtimeTask) ensureAlbumWaiter(groupedID int64, fromPull bool) (chan []*tg.Message, bool) {
	if t == nil || groupedID == 0 {
		return nil, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.albumWait == nil {
		t.albumWait = make(map[int64]chan []*tg.Message)
	}
	if ch, ok := t.albumWait[groupedID]; ok && ch != nil {
		return ch, false
	}
	ch := make(chan []*tg.Message, 1)
	if ok := t.enqueue(realtimeJob{
		kind:      realtimeJobAlbum,
		fromPull:  fromPull,
		groupedID: groupedID,
		albumCh:   ch,
	}); !ok {
		return nil, false
	}

	t.albumWait[groupedID] = ch
	return ch, true
}

func (t *runtimeTask) deliverAlbum(groupedID int64, batch []*tg.Message) {
	t.mu.Lock()
	ch := t.albumWait[groupedID]
	delete(t.albumWait, groupedID)
	t.mu.Unlock()

	if ch == nil {
		return
	}
	select {
	case ch <- batch:
	default:
	}
	close(ch)
}

func (t *runtimeTask) drainQueueOnStop() {
	if t == nil {
		return
	}

	for {
		select {
		case _, ok := <-t.queue:
			if !ok {
				return
			}
			if global.Stats != nil {
				global.Stats.AddPending(-1)
			}
		default:
			return
		}
	}
}

func (t *runtimeTask) cursorSnapshot() (order int, cursor int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Task.HistoryOrder, t.Task.HistoryCursor
}

func (t *runtimeTask) latestProcessedIDSnapshot() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	last := t.Task.HistoryCursor
	if t.Task.HistoryMaxID > last {
		last = t.Task.HistoryMaxID
	}
	return last
}

func (t *runtimeTask) advanceCursor(cursor int) {
	if t == nil || t.Task.ID == 0 || cursor <= 0 {
		return
	}

	taskID := t.Task.ID

	t.mu.Lock()
	order := t.Task.HistoryOrder
	needCursorPersist := false
	needMaxPersist := false

	if cursor > t.Task.HistoryMaxID {
		t.Task.HistoryMaxID = cursor
		needMaxPersist = true
	}
	if order != model.HistoryOrderNewToOld && cursor > t.Task.HistoryCursor {
		t.Task.HistoryCursor = cursor
		needCursorPersist = true
	}
	newCursor := t.Task.HistoryCursor
	t.mu.Unlock()

	if order != model.HistoryOrderNewToOld {
		if needCursorPersist {
			_ = persistHistoryCursorAndMax(t.Ctx, taskID, newCursor)
		} else if needMaxPersist {
			_ = persistHistoryMaxID(t.Ctx, taskID, cursor)
		}
		return
	}

	if needMaxPersist {
		_ = persistHistoryMaxID(t.Ctx, taskID, cursor)
	}
}

func (t *runtimeTask) startPoller(m *TaskManager, api *tg.Client) {
	if t == nil || m == nil || api == nil || t.Ctx == nil || t.SourcePeer == nil || t.Task.ID == 0 {
		return
	}
	if t.pollInterval <= 0 {
		return
	}

	select {
	case <-t.done:
		return
	default:
	}

	// Run an initial poll once to catch up messages that arrived between history clone completion and realtime enter.
	t.pollOnce(m, api)

	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.Ctx.Done():
			return
		case <-t.done:
			return
		case <-ticker.C:
			t.pollOnce(m, api)
		}
	}
}

func (t *runtimeTask) pollOnce(m *TaskManager, api *tg.Client) {
	if t == nil || m == nil || api == nil || t.Ctx == nil || t.SourcePeer == nil || t.Task.ID == 0 {
		return
	}
	select {
	case <-t.done:
		return
	default:
	}
	if err := t.Ctx.Err(); err != nil {
		return
	}
	if !m.isActiveRun(t.Task.ID, t.RunID) {
		return
	}
	if m.isPausedRun(t.Task.ID, t.RunID) {
		return
	}

	allowed, remaining, _ := Scheduler.PeekPull(t.Ctx, t.Task.ID)
	if !allowed {
		return
	}

	remoteLatest, err := getLatestRemoteMessageID(t.Ctx, api, t.SourcePeer)
	if err != nil || remoteLatest <= 0 {
		if err != nil && global.Logger != nil {
			global.Logger.Warn("realtime poll get latest failed", zap.Uint("task_id", t.Task.ID), zap.Error(err))
		}
		return
	}

	localLast := t.latestProcessedIDSnapshot()
	if localLast <= 0 {
		// Avoid processing the entire history when local cursor is missing.
		t.advanceCursor(remoteLatest)
		return
	}
	if remoteLatest <= localLast {
		return
	}

	maxCatchUpUnits := 500
	if remaining >= 0 {
		if remaining <= 0 {
			return
		}
		if remaining < maxCatchUpUnits {
			maxCatchUpUnits = remaining
		}
	}

	afterID := localLast
	unitsEnqueued := 0
	seenAlbums := make(map[int64]struct{}, 64)

	for iter := 0; iter < 20 && afterID < remoteLatest && unitsEnqueued < maxCatchUpUnits; iter++ {
		limit := defaultHistoryPageSize
		if limit <= 0 {
			limit = 50
		}

		req := &tg.MessagesGetHistoryRequest{
			Peer:      t.SourcePeer,
			OffsetID:  afterID,
			AddOffset: -limit,
			Limit:     limit,
			MinID:     afterID,
			MaxID:     remoteLatest + 1,
		}

		r, err := getHistoryWithFloodWait(t.Ctx, api, req)
		if err != nil {
			if global.Logger != nil {
				global.Logger.Warn("realtime poll history failed", zap.Uint("task_id", t.Task.ID), zap.Error(err))
			}
			return
		}
		msgs := extractTGMessages(r)
		if len(msgs) == 0 {
			return
		}

		sort.Slice(msgs, func(i, j int) bool {
			return msgs[i].ID < msgs[j].ID
		})

		pageMax := afterID
		advanced := false

		for _, msg := range msgs {
			if msg == nil || msg.ID <= 0 {
				continue
			}
			if msg.ID <= afterID || msg.ID > remoteLatest {
				continue
			}

			// Skip if another goroutine already processed/advanced.
			if cur := t.latestProcessedIDSnapshot(); cur > 0 && msg.ID <= cur {
				if msg.ID > pageMax {
					pageMax = msg.ID
				}
				continue
			}

			isAlbum := msg.GroupedID != 0 && msg.Media != nil && m.grouper != nil
			isReplyFree := t.Task.KeepReply && extractReplyToSourceMsgID(msg) > 0

			unitCost := 1
			if isAlbum {
				if _, ok := seenAlbums[msg.GroupedID]; ok {
					unitCost = 0
				} else {
					seenAlbums[msg.GroupedID] = struct{}{}
					if isReplyFree {
						unitCost = 0
					}
				}
			} else if isReplyFree {
				unitCost = 0
			}

			if remaining >= 0 && unitsEnqueued+unitCost > maxCatchUpUnits {
				break
			}

			dispatched := m.dispatchMessageToRuntimeTask(t, msg, true)
			if !dispatched {
				continue
			}
			unitsEnqueued += unitCost
			advanced = true

			if msg.ID > pageMax {
				pageMax = msg.ID
			}
		}

		if !advanced || pageMax <= afterID {
			return
		}
		afterID = pageMax
	}
}

func (t *runtimeTask) run(m *TaskManager, api *tg.Client) {
	if t == nil || m == nil || api == nil || t.Ctx == nil {
		return
	}
	if t.TargetPeer == nil && strings.TrimSpace(t.Task.PublishType) != "bot" {
		return
	}

	taskID := t.Task.ID
	runID := t.RunID
	if taskID == 0 {
		return
	}
	if t.quota == nil {
		t.quota = newTaskQuota(t.Task)
	}

	waitQuota := func(need int) error {
		for {
			if err := t.Ctx.Err(); err != nil {
				return err
			}
			if err := m.waitForQuota(t.Ctx, taskID, runID, t.quota, need); err == nil {
				return nil
			} else {
				if global.Logger != nil {
					global.Logger.Warn("quota gate failed, retrying", zap.Uint("task_id", taskID), zap.Error(err))
				}
				sleepWithContext(t.Ctx, 10*time.Second)
			}
		}
	}

	waitScheduleWindow := func() error {
		for {
			if err := t.Ctx.Err(); err != nil {
				return err
			}
			allowed, next := Scheduler.PeekWindow(taskID)
			if allowed {
				return nil
			}
			sleepFor := quotaPollInterval
			if !next.IsZero() {
				sleepFor = time.Until(next)
			}
			if sleepFor < 0 {
				sleepFor = quotaPollInterval
			}
			sleepFor = minDuration(sleepFor, quotaPollInterval)
			sleepWithContext(t.Ctx, sleepFor)
		}
	}

	reserveSchedule := func(need int) (int, error) {
		if need <= 0 {
			return 0, nil
		}
		for {
			if err := t.Ctx.Err(); err != nil {
				return 0, err
			}
			reserved, next, err := Scheduler.ReservePull(t.Ctx, taskID, need)
			if err != nil {
				return 0, err
			}
			if reserved > 0 {
				return reserved, nil
			}
			sleepFor := quotaPollInterval
			if !next.IsZero() {
				sleepFor = time.Until(next)
			}
			if sleepFor < 0 {
				sleepFor = quotaPollInterval
			}
			sleepFor = minDuration(sleepFor, quotaPollInterval)
			sleepWithContext(t.Ctx, sleepFor)
		}
	}

	for {
		select {
		case <-t.done:
			t.drainQueueOnStop()
			return
		case <-t.Ctx.Done():
			t.drainQueueOnStop()
			return
		case job, ok := <-t.queue:
			if !ok {
				return
			}
			if global.Stats != nil {
				global.Stats.AddPending(-1)
			}

			if err := t.Ctx.Err(); err != nil {
				t.drainQueueOnStop()
				return
			}
			select {
			case <-t.done:
				t.drainQueueOnStop()
				return
			default:
			}

			switch job.kind {
			case realtimeJobAlbum:
				var batch []*tg.Message
				select {
				case <-t.done:
					t.drainQueueOnStop()
					return
				case <-t.Ctx.Done():
					t.drainQueueOnStop()
					return
				case batch = <-job.albumCh:
				}
				if len(batch) == 0 {
					continue
				}

				hotTask := t.refreshStrategySnapshot()
				plan := PlanMediaGroup(m, batch, t.allowedTypes, t.allowFileSuffixes, t.blockFileSuffixes)
				if plan.Skipped > 0 {
					global.AddFiltered(uint64(plan.Skipped))
				}

				commentEnabled := t.comment != nil && t.comment.Enabled && t.comment.LocalDB != nil
				sentIDs := []int(nil)

				minID := 0
				maxID := 0
				for _, msg := range batch {
					if msg == nil || msg.ID <= 0 {
						continue
					}
					if maxID == 0 || msg.ID > maxID {
						maxID = msg.ID
					}
					if minID == 0 || msg.ID < minID {
						minID = msg.ID
					}
				}

				if plan.Need > 0 && t.keyword != nil {
					carrier := plan.Text
					if carrier == nil && len(plan.Media) > 0 {
						carrier = plan.Media[0]
					}
					if carrier != nil {
						if out, skip := applyKeywordPolicyToMessage(carrier, t.keyword); skip {
							global.AddFiltered(uint64(plan.Need))
							rootDelta, replyDelta := classifyRootReplyBatch(batch)
							recordTaskCounters(m, t.Task.ID, t.RunID, plan.Need+plan.Skipped, 0, 0, rootDelta, replyDelta)
							if maxID > 0 {
								t.advanceCursor(maxID)
							}
							sleepRandom(t.Ctx, t.delayMin, t.delayMax)
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

				var err error
				quotaUnits := 0
				reservedSchedule := 0
				if plan.Need > 0 {
					quotaUnits = quotaUnitsForBatch(hotTask, batch, plan.Need)
					if t.quota != nil {
						t.quota.UpdateConfig(hotTask.DailyLimit, hotTask.RunWindow)
					}
					if qerr := waitQuota(quotaUnits); qerr != nil {
						t.drainQueueOnStop()
						return
					}

					if quotaUnits > 0 {
						r, rerr := reserveSchedule(quotaUnits)
						if rerr != nil {
							t.drainQueueOnStop()
							return
						}
						reservedSchedule = r
					} else {
						if werr := waitScheduleWindow(); werr != nil {
							t.drainQueueOnStop()
							return
						}
					}

					err = processWithRetry(t.Ctx, func() error {
						if plan.Text != nil {
							if commentEnabled {
								ids, serr := m.processSingleMessageResult(t.Ctx, api, t.SourcePeer, t.TargetPeer, hotTask, plan.Text)
								if serr != nil {
									return serr
								}
								sentIDs = ids
								return nil
							}
							return m.processSingleMessage(t.Ctx, api, t.SourcePeer, t.TargetPeer, hotTask, plan.Text)
						}
						if commentEnabled {
							ids, serr := m.processAlbumBatchResult(t.Ctx, api, t.SourcePeer, t.TargetPeer, hotTask, plan.Media)
							if serr != nil {
								return serr
							}
							sentIDs = ids
							return nil
						}
						return m.processAlbumBatch(t.Ctx, api, t.SourcePeer, t.TargetPeer, hotTask, plan.Media, nil)
					})

					if reservedSchedule > 0 {
						if err == nil {
							Scheduler.CommitPull(t.Ctx, taskID, quotaUnits)
						} else {
							Scheduler.ReleasePull(taskID, reservedSchedule)
						}
					}

					if err == nil && quotaUnits > 0 {
						if qerr := m.quotaAdd(t.Ctx, taskID, t.quota, quotaUnits); qerr != nil && global.Logger != nil {
							global.Logger.Warn("persist quota increment failed", zap.Uint("task_id", taskID), zap.Error(qerr))
						}
					}
				}
				if err != nil {
					if plan.Need > 0 {
						global.AddFail(uint64(plan.Need))
					} else {
						global.IncFail()
					}
					failDelta := plan.Need
					if failDelta <= 0 {
						failDelta = 1
					}
					rootDelta, replyDelta := classifyRootReplyBatch(batch)
					recordTaskCounters(m, t.Task.ID, t.RunID, plan.Need+plan.Skipped, 0, failDelta, rootDelta, replyDelta)
					if global.Logger != nil {
						global.Logger.Error(
							"realtime process album failed",
							zap.Uint("task_id", t.Task.ID),
							zap.Int64("grouped_id", job.groupedID),
							zap.Error(err),
						)
					}
				} else {
					if plan.Need > 0 {
						global.AddSuccess(uint64(plan.Need))
					}
					rootDelta, replyDelta := classifyRootReplyBatch(batch)
					recordTaskCounters(m, t.Task.ID, t.RunID, plan.Need+plan.Skipped, plan.Need, 0, rootDelta, replyDelta)

					if commentEnabled && minID > 0 {
						targetID := minPositiveInt(sentIDs)
						if targetID > 0 {
							srcRoot, dstRoot, _, werr := m.StoreMappingForTrunk(t.Ctx, api, t.Task, t.comment, t.SourcePeer, t.TargetPeer, minID, targetID)
							if werr != nil && global.Logger != nil {
								global.Logger.Warn(
									"store local mapping failed",
									zap.Uint("task_id", t.Task.ID),
									zap.Int("source_msg_id", minID),
									zap.Int("target_msg_id", targetID),
									zap.Error(werr),
								)
							} else if srcRoot > 0 && dstRoot > 0 {
								m.sendPendingCommentsForRoot(t.Ctx, api, t.Task, t.comment, srcRoot, dstRoot)
							}
						}
					}
				}
				if maxID > 0 {
					t.advanceCursor(maxID)
				}
				sleepRandom(t.Ctx, t.delayMin, t.delayMax)

			case realtimeJobSingle:
				msg := job.msg
				if msg == nil || msg.ID <= 0 {
					continue
				}

				hotTask := t.refreshStrategySnapshot()

				ct := m.DetectContentType(msg)
				if t.allowedTypes != nil {
					if _, ok := t.allowedTypes[ct]; !ok {
						global.IncFiltered()
						rootDelta, replyDelta := classifyRootReply(msg)
						recordTaskCounters(m, t.Task.ID, t.RunID, 1, 0, 0, rootDelta, replyDelta)
						t.advanceCursor(msg.ID)
						continue
					}
				}
				if ct == "file" && !fileSuffixAllowed(msg, t.allowFileSuffixes, t.blockFileSuffixes) {
					global.IncFiltered()
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, t.Task.ID, t.RunID, 1, 0, 0, rootDelta, replyDelta)
					t.advanceCursor(msg.ID)
					continue
				}

				msgToSend := msg
				if t.keyword != nil {
					if out, skip := applyKeywordPolicyToMessage(msg, t.keyword); skip {
						if msg.Media != nil || strings.TrimSpace(msg.Message) != "" {
							global.IncFiltered()
						}
						rootDelta, replyDelta := classifyRootReply(msg)
						recordTaskCounters(m, t.Task.ID, t.RunID, 1, 0, 0, rootDelta, replyDelta)
						t.advanceCursor(msg.ID)
						continue
					} else if out != nil {
						msgToSend = out
					}
				}

				need := quotaSendableCount(m, msgToSend, nil)
				quotaUnits := quotaUnitsForSingle(hotTask, msgToSend, need)
				commentEnabled := t.comment != nil && t.comment.Enabled && t.comment.LocalDB != nil
				sentIDs := []int(nil)

				reservedSchedule := 0
				if need > 0 {
					if t.quota != nil {
						t.quota.UpdateConfig(hotTask.DailyLimit, hotTask.RunWindow)
					}
					if qerr := waitQuota(quotaUnits); qerr != nil {
						t.drainQueueOnStop()
						return
					}

					if quotaUnits > 0 {
						r, rerr := reserveSchedule(quotaUnits)
						if rerr != nil {
							t.drainQueueOnStop()
							return
						}
						reservedSchedule = r
					} else {
						if werr := waitScheduleWindow(); werr != nil {
							t.drainQueueOnStop()
							return
						}
					}
				}

				err := processWithRetry(t.Ctx, func() error {
					if commentEnabled {
						ids, serr := m.processSingleMessageResult(t.Ctx, api, t.SourcePeer, t.TargetPeer, hotTask, msgToSend)
						if serr != nil {
							return serr
						}
						sentIDs = ids
						return nil
					}
					return m.processSingleMessage(t.Ctx, api, t.SourcePeer, t.TargetPeer, hotTask, msgToSend)
				})

				if reservedSchedule > 0 {
					if err == nil {
						Scheduler.CommitPull(t.Ctx, taskID, quotaUnits)
					} else {
						Scheduler.ReleasePull(taskID, reservedSchedule)
					}
				}
				if err == nil && quotaUnits > 0 {
					if qerr := m.quotaAdd(t.Ctx, taskID, t.quota, quotaUnits); qerr != nil && global.Logger != nil {
						global.Logger.Warn("persist quota increment failed", zap.Uint("task_id", taskID), zap.Error(qerr))
					}
				}

				if err != nil {
					if need > 0 {
						global.AddFail(uint64(need))
					} else {
						global.IncFail()
					}
					failDelta := need
					if failDelta <= 0 {
						failDelta = 1
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, t.Task.ID, t.RunID, 1, 0, failDelta, rootDelta, replyDelta)
					if global.Logger != nil {
						global.Logger.Error(
							"realtime process message failed",
							zap.Uint("task_id", t.Task.ID),
							zap.Int("msg_id", msg.ID),
							zap.Error(err),
						)
					}
				} else {
					if need > 0 {
						global.AddSuccess(uint64(need))
					}
					rootDelta, replyDelta := classifyRootReply(msg)
					recordTaskCounters(m, t.Task.ID, t.RunID, 1, need, 0, rootDelta, replyDelta)
					if commentEnabled {
						targetID := minPositiveInt(sentIDs)
						if targetID > 0 {
							srcRoot, dstRoot, _, werr := m.StoreMappingForTrunk(t.Ctx, api, t.Task, t.comment, t.SourcePeer, t.TargetPeer, msg.ID, targetID)
							if werr != nil && global.Logger != nil {
								global.Logger.Warn(
									"store local mapping failed",
									zap.Uint("task_id", t.Task.ID),
									zap.Int("source_msg_id", msg.ID),
									zap.Int("target_msg_id", targetID),
									zap.Error(werr),
								)
							} else if srcRoot > 0 && dstRoot > 0 {
								m.sendPendingCommentsForRoot(t.Ctx, api, t.Task, t.comment, srcRoot, dstRoot)
							}
						}
					}
				}

				t.advanceCursor(msg.ID)
				sleepRandom(t.Ctx, t.delayMin, t.delayMax)
			}
		}
	}
}

type telegramRuntime struct {
	sessionPath string

	mu sync.Mutex

	ready     chan struct{}
	startDone bool
	startErr  error
	startedAt time.Time

	client *telegram.Client
	api    *tg.Client
	cancel context.CancelFunc

	tasksMu   sync.RWMutex
	tasksByID map[uint]*runtimeTask
	bySource  map[int64]map[uint]*runtimeTask

	linkedMu    sync.Mutex
	linkedChats map[int64]*linkedChatInfo // key: channelID

	commentTasksByID    map[uint]*commentProducerTask
	commentByLinkedChat map[int64]map[uint]*commentProducerTask // key: linked chat channelID
}

func newTelegramRuntime(sessionPath string) *telegramRuntime {
	return &telegramRuntime{
		sessionPath:         sessionPath,
		tasksByID:           make(map[uint]*runtimeTask),
		bySource:            make(map[int64]map[uint]*runtimeTask),
		linkedChats:         make(map[int64]*linkedChatInfo),
		commentTasksByID:    make(map[uint]*commentProducerTask),
		commentByLinkedChat: make(map[int64]map[uint]*commentProducerTask),
	}
}

func (rt *telegramRuntime) shutdown() {
	if rt == nil {
		return
	}

	rt.tasksMu.Lock()
	for _, t := range rt.tasksByID {
		if t != nil {
			t.stop()
		}
	}
	for _, t := range rt.commentTasksByID {
		if t != nil {
			t.stop()
		}
	}
	rt.tasksByID = make(map[uint]*runtimeTask)
	rt.bySource = make(map[int64]map[uint]*runtimeTask)
	rt.commentTasksByID = make(map[uint]*commentProducerTask)
	rt.commentByLinkedChat = make(map[int64]map[uint]*commentProducerTask)
	rt.tasksMu.Unlock()

	rt.mu.Lock()
	cancel := rt.cancel
	rt.cancel = nil
	rt.api = nil
	rt.client = nil
	rt.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (m *TaskManager) ensureTelegramForTask(ctx context.Context, t model.Task) (*telegramRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("task manager is nil")
	}
	if m.tg == nil {
		m.tg = newTelegramRuntimeManager()
	}

	apiID, apiHash, err := pickTelegramApp()
	if err != nil {
		return nil, err
	}

	sessionPath, err := pickSessionPathForTask(t)
	if err != nil {
		return nil, err
	}

	rt := m.tg.getOrCreate(sessionPath)
	if rt == nil {
		return nil, errors.New("telegram runtime is nil")
	}

	if err := rt.ensureStarted(ctx, m, apiID, apiHash, sessionPath); err != nil {
		return nil, err
	}
	return rt, nil
}

func pickSessionPathForTask(t model.Task) (string, error) {
	key := strings.TrimSpace(t.ExecuteBy)
	if key == "" {
		return pickSessionPath()
	}

	path := GetSessionPathForKey(key)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("指定账号 session 不存在: %s", key)
		}
		return "", fmt.Errorf("检查账号 session 失败: %w", err)
	}
	if info == nil || info.IsDir() {
		return "", fmt.Errorf("指定账号 session 无效: %s", path)
	}
	return path, nil
}

func (rt *telegramRuntime) ensureStarted(ctx context.Context, m *TaskManager, apiID int, apiHash, sessionPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rt == nil {
		return errors.New("telegram runtime is nil")
	}

	rt.mu.Lock()
	if rt.ready != nil {
		ready := rt.ready
		rt.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
		}

		rt.mu.Lock()
		err := rt.startErr
		rt.mu.Unlock()
		return err
	}

	rt.ready = make(chan struct{})
	ready := rt.ready
	rt.sessionPath = sessionPath
	rt.mu.Unlock()

	d := tg.NewUpdateDispatcher()
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg == nil {
			return nil
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok {
			m.dispatchChannelMessage(rt, chID, msg)
			m.dispatchCommentNew(rt, chID, msg)
		}
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg == nil {
			return nil
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok {
			m.dispatchChannelMessage(rt, chID, msg)
			m.dispatchCommentNew(rt, chID, msg)
		}
		return nil
	})
	d.OnEditMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateEditMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg == nil {
			return nil
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok {
			m.dispatchCommentEdit(rt, chID, msg)
		}
		return nil
	})
	d.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateEditChannelMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg == nil {
			return nil
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok {
			m.dispatchCommentEdit(rt, chID, msg)
		}
		return nil
	})

	client, err := newTelegramClient(apiID, apiHash, sessionPath, d)
	if err != nil {
		rt.finishStart(err, ready)
		return err
	}

	runCtx, cancel := context.WithCancel(context.Background())

	rt.mu.Lock()
	rt.client = client
	rt.api = client.API()
	rt.cancel = cancel
	rt.startedAt = time.Now()
	rt.mu.Unlock()

	go rt.runWithReconnect(runCtx, client, sessionPath, ready)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
	}

	rt.mu.Lock()
	startErr := rt.startErr
	rt.mu.Unlock()
	return startErr
}

func (rt *telegramRuntime) runWithReconnect(runCtx context.Context, client *telegram.Client, sessionPath string, ready chan struct{}) {
	if rt == nil || client == nil {
		return
	}
	if runCtx == nil {
		runCtx = context.Background()
	}

	readyPublished := atomic.Bool{}
	attempt := 0

	for {
		if err := runCtx.Err(); err != nil {
			if !readyPublished.Load() {
				rt.finishStart(err, ready)
			}
			return
		}

		err := client.Run(runCtx, func(ctx context.Context) error {
			status, serr := client.Auth().Status(ctx)
			if serr != nil {
				return serr
			}
			if !status.Authorized {
				return errors.New("telegram 未授权：请先运行 cmd/auth_tool 登录或使用 /api/v1/tg/qr 扫码生成 session 文件")
			}

			// Reset backoff after we successfully establish a session.
			attempt = 0

			if !readyPublished.Load() {
				readyPublished.Store(true)
				rt.finishStart(nil, ready)
			}

			<-ctx.Done()
			return ctx.Err()
		})

		if runCtx.Err() != nil {
			return
		}

		// Ignore normal shutdown.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		// If we haven't ever reached a ready state, decide whether to keep retrying.
		if !readyPublished.Load() {
			if isRetryableTelegramRuntimeErr(err) {
				attempt++
				wait := retry.WithJitter(retry.Backoff(attempt, 2*time.Second, 45*time.Second), 0.25)
				if global.Logger != nil {
					global.Logger.Warn(
						"telegram runtime start failed, retrying",
						zap.String("session", sessionPath),
						zap.Int("attempt", attempt),
						zap.Duration("wait", wait),
						zap.Error(err),
					)
				}
				if attempt == 1 || attempt%5 == 0 {
					global.BroadcastLog(fmt.Sprintf(
						"[WARN] Telegram 连接失败，将在 %s 后重试 (session=%s): %v",
						wait.Round(time.Second),
						filepath.Base(sessionPath),
						err,
					))
				}
				_ = retry.Sleep(runCtx, wait)
				continue
			}

			// Fatal on start.
			rt.finishStart(err, ready)
			if global.Logger != nil {
				global.Logger.Error("telegram runtime start failed", zap.String("session", sessionPath), zap.Error(err))
			}
			return
		}

		// Runtime was ready before: attempt reconnect on transient errors.
		if isRetryableTelegramRuntimeErr(err) {
			attempt++
			wait := retry.WithJitter(retry.Backoff(attempt, 2*time.Second, 60*time.Second), 0.3)
			if global.Logger != nil {
				global.Logger.Warn(
					"telegram runtime stopped, reconnecting",
					zap.String("session", sessionPath),
					zap.Int("attempt", attempt),
					zap.Duration("wait", wait),
					zap.Error(err),
				)
			}
			if attempt == 1 || attempt%10 == 0 {
				global.BroadcastLog(fmt.Sprintf(
					"[WARN] Telegram 连接中断，将在 %s 后重连 (session=%s): %v",
					wait.Round(time.Second),
					filepath.Base(sessionPath),
					err,
				))
			}
			_ = retry.Sleep(runCtx, wait)
			continue
		}

		if global.Logger != nil {
			global.Logger.Error("telegram runtime stopped (fatal)", zap.String("session", sessionPath), zap.Error(err))
		}
		return
	}
}

func isRetryableTelegramRuntimeErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, pool.ErrConnDead) {
		return true
	}
	if retry.IsRetryableNetErr(err) {
		return true
	}
	// Telegram RPC/server-side errors (best-effort).
	if rpcErr, ok := tgerr.As(err); ok && rpcErr != nil {
		if rpcErr.Code >= 500 || rpcErr.Code == 420 {
			return true
		}
		if rpcErr.IsOneOf("RPC_CALL_FAIL", "TIMEOUT", "INTERNAL", "SERVER_ERROR", "SERVICE_UNAVAILABLE") {
			return true
		}
	}
	return false
}

func (rt *telegramRuntime) finishStart(err error, ready chan struct{}) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	if rt.startErr == nil && err != nil {
		rt.startErr = err
	}
	if rt.ready == ready && rt.ready != nil && !rt.startDone {
		rt.startDone = true
		close(rt.ready)
	}
	rt.mu.Unlock()
}

func pickSessionPath() (string, error) {
	// Auto-detect session_*.json under session_path.
	base := SessionBasePath()
	pattern := filepath.Join(base, "session_*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("扫描会话文件失败: %w", err)
	}

	// Filter out meta files and non-regular files.
	sessions := make([]string, 0, len(matches))
	for _, p := range matches {
		name := filepath.Base(p)
		if strings.HasSuffix(name, ".meta.json") {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || info == nil || info.IsDir() {
			continue
		}
		sessions = append(sessions, p)
	}

	if len(sessions) == 0 {
		return "", fmt.Errorf("未找到账号会话文件(%s)，请先在「账号管理」里登录生成 sessions/session_*.json", pattern)
	}
	if len(sessions) == 1 {
		return sessions[0], nil
	}

	sort.Slice(sessions, func(i, j int) bool {
		a, errA := os.Stat(sessions[i])
		b, errB := os.Stat(sessions[j])
		if errA != nil || a == nil {
			return false
		}
		if errB != nil || b == nil {
			return true
		}
		if a.ModTime().Equal(b.ModTime()) {
			return sessions[i] < sessions[j]
		}
		return a.ModTime().After(b.ModTime())
	})

	picked := sessions[0]
	if global.Logger != nil {
		global.Logger.Warn("发现多个账号会话文件，默认选择最新的一个", zap.String("picked", picked), zap.Int("count", len(sessions)))
	}
	return picked, nil
}

func peerToChannelID(peer tg.PeerClass) (int64, bool) {
	switch v := peer.(type) {
	case *tg.PeerChannel:
		return v.ChannelID, true
	default:
		return 0, false
	}
}

func (m *TaskManager) registerRealtimeTask(tgRT *telegramRuntime, cfg runtimeTaskConfig, sourceChannelID int64) error {
	if m == nil || tgRT == nil {
		return errors.New("telegram runtime not initialized")
	}
	if cfg.Task.ID == 0 {
		return errors.New("task id is required")
	}
	if sourceChannelID == 0 {
		return errors.New("source_channel_id is required")
	}
	if cfg.TargetPeer == nil && strings.TrimSpace(cfg.Task.PublishType) != "bot" {
		return errors.New("target peer is nil")
	}
	if cfg.Ctx == nil {
		return errors.New("task context is nil")
	}
	taskID := cfg.Task.ID

	tgRT.tasksMu.Lock()
	if prev := tgRT.tasksByID[taskID]; prev != nil {
		prev.stop()
	}
	taskPtr := newRuntimeTask(cfg)
	tgRT.tasksByID[taskID] = taskPtr
	mm := tgRT.bySource[sourceChannelID]
	if mm == nil {
		mm = make(map[uint]*runtimeTask)
		tgRT.bySource[sourceChannelID] = mm
	}
	mm[taskID] = taskPtr
	api := tgRT.api
	tgRT.tasksMu.Unlock()

	if api == nil {
		return errors.New("tg api is nil")
	}

	go taskPtr.run(m, api)
	if taskPtr.pollInterval > 0 {
		go taskPtr.startPoller(m, api)
	}
	return nil
}

func (m *TaskManager) unregisterTask(taskID uint, sourceChannelID int64) {
	if m == nil || m.tg == nil || taskID == 0 {
		return
	}

	runtimes := m.tg.snapshot()
	for _, tgRT := range runtimes {
		if tgRT == nil {
			continue
		}

		var removed *runtimeTask
		var removedComment *commentProducerTask
		tgRT.tasksMu.Lock()
		if cur := tgRT.tasksByID[taskID]; cur != nil {
			removed = cur
			delete(tgRT.tasksByID, taskID)
		}
		if cur := tgRT.commentTasksByID[taskID]; cur != nil {
			removedComment = cur
			delete(tgRT.commentTasksByID, taskID)
			if cur.comment != nil {
				if mm := tgRT.commentByLinkedChat[cur.comment.SourceLinkedChatID]; mm != nil {
					delete(mm, taskID)
					if len(mm) == 0 {
						delete(tgRT.commentByLinkedChat, cur.comment.SourceLinkedChatID)
					}
				}
			}
		}
		if sourceChannelID != 0 {
			if mm := tgRT.bySource[sourceChannelID]; mm != nil {
				delete(mm, taskID)
				if len(mm) == 0 {
					delete(tgRT.bySource, sourceChannelID)
				}
			}
		} else {
			// best-effort remove from all sources
			for sid, mm := range tgRT.bySource {
				delete(mm, taskID)
				if len(mm) == 0 {
					delete(tgRT.bySource, sid)
				}
			}
		}
		tgRT.tasksMu.Unlock()

		if removed != nil {
			removed.stop()
		}
		if removedComment != nil {
			removedComment.stop()
		}
	}

	if m.dedup != nil {
		m.dedup.DropTask(taskID)
	}
}

func (m *TaskManager) dispatchChannelMessage(tgRT *telegramRuntime, channelID int64, msg *tg.Message) {
	if m == nil || tgRT == nil || channelID == 0 || msg == nil {
		return
	}

	tgRT.tasksMu.RLock()
	mm := tgRT.bySource[channelID]
	if len(mm) == 0 {
		tgRT.tasksMu.RUnlock()
		return
	}
	tasks := make([]*runtimeTask, 0, len(mm))
	for _, rt := range mm {
		if rt != nil {
			tasks = append(tasks, rt)
		}
	}
	tgRT.tasksMu.RUnlock()

	for _, rt := range tasks {
		// Push dispatcher only: allow poll-only tasks to stay silent on updates.
		if rt == nil || !rt.Task.Realtime {
			continue
		}
		_ = m.dispatchMessageToRuntimeTask(rt, msg, false)
	}
}

func (m *TaskManager) dispatchMessageToRuntimeTask(rt *runtimeTask, msg *tg.Message, fromPull bool) bool {
	if m == nil || rt == nil || msg == nil {
		return false
	}
	if rt.Ctx == nil {
		return false
	}
	if rt.TargetPeer == nil && strings.TrimSpace(rt.Task.PublishType) != "bot" {
		return false
	}
	if rt.Task.ID == 0 || msg.ID <= 0 {
		return false
	}
	if rt.Ctx.Err() != nil {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
	}
	if !m.isActiveRun(rt.Task.ID, rt.RunID) {
		return false
	}

	order, cursor := rt.cursorSnapshot()
	// 基础去重：防止历史刚跑完，实时 difference 又推来同一条
	if order != model.HistoryOrderNewToOld && cursor > 0 && msg.ID <= cursor {
		return false
	}
	dedupMarked := false
	if m.dedup != nil {
		if m.dedup.Seen(rt.Task.ID, msg.ID) {
			return false
		}
		dedupMarked = true
	}

	if msg.GroupedID != 0 && msg.Media != nil && m.grouper != nil {
		groupedID := msg.GroupedID
		ch, _ := rt.ensureAlbumWaiter(groupedID, fromPull)
		if ch == nil {
			if dedupMarked && m.dedup != nil {
				m.dedup.Forget(rt.Task.ID, msg.ID)
			}
			return false
		}
		m.grouper.Add(rt.Task.ID, groupedID, msg, func(batch []*tg.Message) {
			rt.deliverAlbum(groupedID, batch)
		})
		return true
	}

	if ok := rt.enqueue(realtimeJob{
		kind:     realtimeJobSingle,
		fromPull: fromPull,
		msg:      msg,
	}); !ok {
		if dedupMarked && m.dedup != nil {
			m.dedup.Forget(rt.Task.ID, msg.ID)
		}
		return false
	}
	return true
}
