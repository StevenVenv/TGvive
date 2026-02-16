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
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
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
	msg       *tg.Message
	groupedID int64
	albumCh   <-chan []*tg.Message
}

type runtimeTaskConfig struct {
	Task       model.Task
	RunID      uint64
	Ctx        context.Context
	TargetPeer tg.InputPeerClass
}

type runtimeTask struct {
	Task       model.Task
	RunID      uint64
	Ctx        context.Context
	TargetPeer tg.InputPeerClass

	allowedTypes map[string]struct{}
	delayMin     time.Duration
	delayMax     time.Duration
	quota        *taskQuota

	queue    chan realtimeJob
	stopOnce sync.Once

	mu        sync.Mutex
	albumWait map[int64]chan []*tg.Message
}

func newRuntimeTask(cfg runtimeTaskConfig) *runtimeTask {
	t := &runtimeTask{
		Task:       cfg.Task,
		RunID:      cfg.RunID,
		Ctx:        cfg.Ctx,
		TargetPeer: cfg.TargetPeer,

		allowedTypes: normalizeTypeSet(cfg.Task.ContentTypes.Strings()),
		delayMin:     defaultMsgDelayMin,
		delayMax:     defaultMsgDelayMax,
		quota:        newTaskQuota(cfg.Task),

		queue:     make(chan realtimeJob, 512),
		albumWait: make(map[int64]chan []*tg.Message),
	}

	t.delayMin, t.delayMax = normalizeDelayRange(cfg.Task.DelayMinMs, cfg.Task.DelayMaxMs, defaultMsgDelayMin, defaultMsgDelayMax)
	return t
}

func (t *runtimeTask) stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() {
		close(t.queue)

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

func (t *runtimeTask) enqueue(job realtimeJob) {
	if t == nil {
		return
	}
	if t.Ctx == nil || t.Ctx.Err() != nil {
		return
	}

	select {
	case t.queue <- job:
		if global.Stats != nil {
			global.Stats.AddPending(1)
		}
	default:
		if global.Logger != nil {
			global.Logger.Warn("realtime queue full, dropping message", zap.Uint("task_id", t.Task.ID))
		}
	}
}

func (t *runtimeTask) ensureAlbumWaiter(groupedID int64) (chan []*tg.Message, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.albumWait == nil {
		t.albumWait = make(map[int64]chan []*tg.Message)
	}
	if ch, ok := t.albumWait[groupedID]; ok && ch != nil {
		return ch, false
	}
	ch := make(chan []*tg.Message, 1)
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

func (t *runtimeTask) cursorSnapshot() (order int, cursor int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Task.HistoryOrder, t.Task.HistoryCursor
}

func (t *runtimeTask) advanceCursor(cursor int) {
	if t == nil || t.Task.ID == 0 || cursor <= 0 {
		return
	}

	t.mu.Lock()
	if t.Task.HistoryOrder == model.HistoryOrderNewToOld {
		t.mu.Unlock()
		return
	}
	if cursor <= t.Task.HistoryCursor {
		t.mu.Unlock()
		return
	}
	t.Task.HistoryCursor = cursor
	t.mu.Unlock()

	_ = persistHistoryCursor(t.Task.ID, cursor)
}

func (t *runtimeTask) run(m *TaskManager, api *tg.Client) {
	if t == nil || m == nil || api == nil || t.TargetPeer == nil || t.Ctx == nil {
		return
	}

	for {
		select {
		case <-t.Ctx.Done():
			// Best-effort drain to keep global pending counter consistent on cancel.
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
		case job, ok := <-t.queue:
			if !ok {
				return
			}
			if global.Stats != nil {
				global.Stats.AddPending(-1)
			}
			if err := t.Ctx.Err(); err != nil {
				return
			}

			switch job.kind {
			case realtimeJobAlbum:
				var batch []*tg.Message
				select {
				case <-t.Ctx.Done():
					return
				case batch = <-job.albumCh:
				}
				if len(batch) == 0 {
					continue
				}

				need := quotaSendableAlbumCount(m, batch, t.allowedTypes)
				if skipped := len(batch) - need; skipped > 0 {
					global.AddFiltered(uint64(skipped))
				}
				if need > 0 {
					if err := m.waitForQuota(t.Ctx, t.Task.ID, t.RunID, t.quota, need); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						if global.Logger != nil {
							global.Logger.Error("realtime quota gate failed", zap.Uint("task_id", t.Task.ID), zap.Error(err))
						}
						return
					}
				}

				maxID := 0
				for _, msg := range batch {
					if msg != nil && msg.ID > maxID {
						maxID = msg.ID
					}
				}

				err := processWithRetry(t.Ctx, func() error {
					return m.processAlbumBatch(t.Ctx, api, t.TargetPeer, t.Task, batch, t.allowedTypes)
				})
				if err != nil {
					if need > 0 {
						global.AddFail(uint64(need))
					} else {
						global.IncFail()
					}
					if global.Logger != nil {
						global.Logger.Error(
							"realtime process album failed",
							zap.Uint("task_id", t.Task.ID),
							zap.Int64("grouped_id", job.groupedID),
							zap.Error(err),
						)
					}
				} else if need > 0 {
					global.AddSuccess(uint64(need))
				}
				if err == nil && need > 0 {
					if err := m.quotaAdd(t.Ctx, t.Task.ID, t.quota, need); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						if global.Logger != nil {
							global.Logger.Error("persist quota counter failed", zap.Uint("task_id", t.Task.ID), zap.Error(err))
						}
						return
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

				if t.allowedTypes != nil {
					ct := m.DetectContentType(msg)
					if _, ok := t.allowedTypes[ct]; !ok {
						global.IncFiltered()
						t.advanceCursor(msg.ID)
						continue
					}
				}

				need := quotaSendableCount(m, msg, nil)
				if need > 0 {
					if err := m.waitForQuota(t.Ctx, t.Task.ID, t.RunID, t.quota, need); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						if global.Logger != nil {
							global.Logger.Error("realtime quota gate failed", zap.Uint("task_id", t.Task.ID), zap.Error(err))
						}
						return
					}
				}

				err := processWithRetry(t.Ctx, func() error {
					return m.processSingleMessage(t.Ctx, api, t.TargetPeer, t.Task, msg)
				})
				if err != nil {
					if need > 0 {
						global.AddFail(uint64(need))
					} else {
						global.IncFail()
					}
					if global.Logger != nil {
						global.Logger.Error(
							"realtime process message failed",
							zap.Uint("task_id", t.Task.ID),
							zap.Int("msg_id", msg.ID),
							zap.Error(err),
						)
					}
				} else if need > 0 {
					global.AddSuccess(uint64(need))
				}

				if err == nil && need > 0 {
					if err := m.quotaAdd(t.Ctx, t.Task.ID, t.quota, need); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						if global.Logger != nil {
							global.Logger.Error("persist quota counter failed", zap.Uint("task_id", t.Task.ID), zap.Error(err))
						}
						return
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
}

func newTelegramRuntime(sessionPath string) *telegramRuntime {
	return &telegramRuntime{
		sessionPath: sessionPath,
		tasksByID:   make(map[uint]*runtimeTask),
		bySource:    make(map[int64]map[uint]*runtimeTask),
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
	rt.tasksByID = make(map[uint]*runtimeTask)
	rt.bySource = make(map[int64]map[uint]*runtimeTask)
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

func (m *TaskManager) ensureTelegram(ctx context.Context) (*telegramRuntime, error) {
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

	sessionPath, err := pickSessionPath()
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
		}
		return nil
	})

	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &FileSessionStorage{Path: sessionPath},
		UpdateHandler:  d,
	})

	runCtx, cancel := context.WithCancel(context.Background())

	rt.mu.Lock()
	rt.client = client
	rt.api = client.API()
	rt.cancel = cancel
	rt.startedAt = time.Now()
	rt.mu.Unlock()

	go func() {
		err := client.Run(runCtx, func(ctx context.Context) error {
			status, err := client.Auth().Status(ctx)
			if err != nil {
				rt.finishStart(err, ready)
				return err
			}
			if !status.Authorized {
				err := errors.New("Telegram 未授权：请先运行 cmd/auth_tool 登录或使用 /api/v1/tg/qr 扫码生成 session 文件")
				rt.finishStart(err, ready)
				return err
			}

			rt.finishStart(nil, ready)
			<-ctx.Done()
			return ctx.Err()
		})

		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			rt.finishStart(err, ready)
			if global.Logger != nil {
				global.Logger.Error("telegram runtime stopped", zap.String("session", sessionPath), zap.Error(err))
			}
		}
	}()

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
	base := strings.TrimSpace(global.Config.Telegram.SessionPath)
	if base == "" {
		base = "./sessions/"
	}
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
	if cfg.TargetPeer == nil {
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
		tgRT.tasksMu.Lock()
		if cur := tgRT.tasksByID[taskID]; cur != nil {
			removed = cur
			delete(tgRT.tasksByID, taskID)
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
		if rt == nil || rt.TargetPeer == nil || rt.Ctx == nil {
			continue
		}
		if rt.Task.ID == 0 || msg.ID <= 0 {
			continue
		}
		if rt.Ctx.Err() != nil {
			continue
		}
		if !m.isActiveRun(rt.Task.ID, rt.RunID) {
			continue
		}

		order, cursor := rt.cursorSnapshot()
		// 基础去重：防止历史刚跑完，实时 difference 又推来同一条
		if order != model.HistoryOrderNewToOld && cursor > 0 && msg.ID <= cursor {
			continue
		}
		if m.dedup != nil && m.dedup.Seen(rt.Task.ID, msg.ID) {
			continue
		}

		if msg.GroupedID != 0 && msg.Media != nil && m.grouper != nil {
			groupedID := msg.GroupedID
			ch, first := rt.ensureAlbumWaiter(groupedID)
			m.grouper.Add(rt.Task.ID, groupedID, msg, func(batch []*tg.Message) {
				rt.deliverAlbum(groupedID, batch)
			})
			if first {
				rt.enqueue(realtimeJob{
					kind:      realtimeJobAlbum,
					groupedID: groupedID,
					albumCh:   ch,
				})
			}
			continue
		}

		rt.enqueue(realtimeJob{
			kind: realtimeJobSingle,
			msg:  msg,
		})
	}
}

// resolveTargetPeer is a small helper for monitor/history orchestration.
func resolveTargetPeer(ctx context.Context, api *tg.Client, raw string) (tg.InputPeerClass, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty target peer")
	}

	s := message.NewSender(api)
	return s.Resolve(raw).AsInputPeer(ctx)
}
