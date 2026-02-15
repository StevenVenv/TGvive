package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

type runtimeTask struct {
	Task       model.Task
	RunID      uint64
	Ctx        context.Context
	TargetPeer tg.InputPeerClass
}

type telegramRuntime struct {
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

func newTelegramRuntime() *telegramRuntime {
	return &telegramRuntime{
		tasksByID: make(map[uint]*runtimeTask),
		bySource:  make(map[int64]map[uint]*runtimeTask),
	}
}

func (m *TaskManager) ensureTelegram(ctx context.Context) (*tg.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("task manager is nil")
	}
	if m.tg == nil {
		m.tg = newTelegramRuntime()
	}

	rt := m.tg

	rt.mu.Lock()
	if rt.ready != nil {
		ready := rt.ready
		rt.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ready:
		}

		rt.mu.Lock()
		api := rt.api
		err := rt.startErr
		rt.mu.Unlock()
		return api, err
	}

	rt.ready = make(chan struct{})
	ready := rt.ready
	rt.mu.Unlock()

	apiID := global.Config.Telegram.APIID
	apiHash := strings.TrimSpace(global.Config.Telegram.APIHash)
	if apiID == 0 || apiHash == "" {
		return m.finishTelegramStart(fmt.Errorf("telegram 配置缺失: telegram.api_id / telegram.api_hash"), ready)
	}

	sessionPath, err := pickSessionPath()
	if err != nil {
		return m.finishTelegramStart(err, ready)
	}

	d := tg.NewUpdateDispatcher()
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg == nil {
			return nil
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok {
			m.dispatchChannelMessage(chID, msg)
		}
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg == nil {
			return nil
		}
		if chID, ok := peerToChannelID(msg.PeerID); ok {
			m.dispatchChannelMessage(chID, msg)
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
				m.finishTelegramStart(err, ready)
				return err
			}
			if !status.Authorized {
				err := errors.New("Telegram 未授权：请先运行 cmd/auth_tool 登录或使用 /api/v1/tg/qr 扫码生成 session 文件")
				m.finishTelegramStart(err, ready)
				return err
			}

			m.finishTelegramStart(nil, ready)
			<-ctx.Done()
			return ctx.Err()
		})

		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			m.finishTelegramStart(err, ready)
			if global.Logger != nil {
				global.Logger.Error("telegram runtime stopped", zap.Error(err))
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ready:
	}

	rt.mu.Lock()
	api := rt.api
	startErr := rt.startErr
	rt.mu.Unlock()
	return api, startErr
}

func (m *TaskManager) finishTelegramStart(err error, ready chan struct{}) (*tg.Client, error) {
	rt := m.tg
	rt.mu.Lock()
	if rt.startErr == nil && err != nil {
		rt.startErr = err
	}
	// Close only once.
	if rt.ready == ready && rt.ready != nil && !rt.startDone {
		rt.startDone = true
		close(rt.ready)
	}
	api := rt.api
	startErr := rt.startErr
	rt.mu.Unlock()
	return api, startErr
}

func pickSessionPath() (string, error) {
	// 1) Explicit session_key
	if key := strings.TrimSpace(global.Config.Telegram.SessionKey); key != "" {
		return GetSessionPathForKey(key), nil
	}

	// 2) ENV override (useful for local dev / CI)
	if key := strings.TrimSpace(os.Getenv("TG_SESSION_KEY")); key != "" {
		return GetSessionPathForKey(key), nil
	}

	// 3) Fallback: auto-detect if there is exactly one session_*.json under session_path
	base := strings.TrimSpace(global.Config.Telegram.SessionPath)
	if base == "" {
		base = "./sessions/"
	}
	pattern := filepath.Join(base, "session_*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("扫描 session 文件失败: %w", err)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("未找到 session 文件(%s)，请先登录生成 sessions/session_*.json 或配置 telegram.session_key", pattern)
	}
	return "", fmt.Errorf("发现多个 session 文件(%s)，请配置 telegram.session_key 或设置 TG_SESSION_KEY 指定使用哪个", pattern)
}

func peerToChannelID(peer tg.PeerClass) (int64, bool) {
	switch v := peer.(type) {
	case *tg.PeerChannel:
		return v.ChannelID, true
	default:
		return 0, false
	}
}

func (m *TaskManager) registerRealtimeTask(rt runtimeTask, sourceChannelID int64) error {
	if m == nil || m.tg == nil {
		return errors.New("telegram runtime not initialized")
	}
	if rt.Task.ID == 0 {
		return errors.New("task id is required")
	}
	if sourceChannelID == 0 {
		return errors.New("source_channel_id is required")
	}
	if rt.TargetPeer == nil {
		return errors.New("target peer is nil")
	}
	if rt.Ctx == nil {
		return errors.New("task context is nil")
	}

	m.tg.tasksMu.Lock()
	defer m.tg.tasksMu.Unlock()

	cp := rt
	m.tg.tasksByID[rt.Task.ID] = &cp
	mm := m.tg.bySource[sourceChannelID]
	if mm == nil {
		mm = make(map[uint]*runtimeTask)
		m.tg.bySource[sourceChannelID] = mm
	}
	mm[rt.Task.ID] = &cp
	return nil
}

func (m *TaskManager) unregisterTask(taskID uint, sourceChannelID int64) {
	if m == nil || m.tg == nil || taskID == 0 {
		return
	}

	m.tg.tasksMu.Lock()
	delete(m.tg.tasksByID, taskID)
	if sourceChannelID != 0 {
		if mm := m.tg.bySource[sourceChannelID]; mm != nil {
			delete(mm, taskID)
			if len(mm) == 0 {
				delete(m.tg.bySource, sourceChannelID)
			}
		}
	} else {
		// best-effort remove from all sources
		for sid, mm := range m.tg.bySource {
			delete(mm, taskID)
			if len(mm) == 0 {
				delete(m.tg.bySource, sid)
			}
		}
	}
	m.tg.tasksMu.Unlock()

	if m.dedup != nil {
		m.dedup.DropTask(taskID)
	}
}

func (m *TaskManager) dispatchChannelMessage(channelID int64, msg *tg.Message) {
	if m == nil || m.tg == nil || channelID == 0 || msg == nil {
		return
	}

	m.tg.tasksMu.RLock()
	mm := m.tg.bySource[channelID]
	if len(mm) == 0 {
		m.tg.tasksMu.RUnlock()
		return
	}
	tasks := make([]*runtimeTask, 0, len(mm))
	for _, rt := range mm {
		if rt != nil {
			tasks = append(tasks, rt)
		}
	}
	api := m.tg.api
	m.tg.tasksMu.RUnlock()

	if api == nil {
		return
	}

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

		// 基础去重：防止历史刚跑完，实时 difference 又推来同一条
		if rt.Task.HistoryOrder != model.HistoryOrderNewToOld && rt.Task.HistoryCursor > 0 && msg.ID <= rt.Task.HistoryCursor {
			continue
		}
		if m.dedup != nil && m.dedup.Seen(rt.Task.ID, msg.ID) {
			continue
		}

		taskCopy := rt.Task
		peer := rt.TargetPeer
		taskCtx := rt.Ctx

		go func() {
			if err := m.ProcessMessage(taskCtx, api, peer, taskCopy, msg); err != nil {
				if global.Logger != nil {
					global.Logger.Error(
						"realtime process message failed",
						zap.Uint("task_id", taskCopy.ID),
						zap.Int("msg_id", msg.ID),
						zap.Error(err),
					)
				}
			}
		}()
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
