package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"go.uber.org/zap"
)

// TaskProgress matches the polling response expected by UI.
type TaskProgress struct {
	TaskID       uint     `json:"task_id"`
	Status       string   `json:"status"`
	Speed        string   `json:"speed"`
	ProgressPct  int      `json:"progress_pct"`
	ProcessedCnt int      `json:"processed_cnt"`
	SuccessCnt   int      `json:"success_cnt"`
	FailCnt      int      `json:"fail_cnt"`
	TotalMsg     int      `json:"total_msg"`
	Logs         []string `json:"logs"`
}

// TaskManager manages concurrent running tasks.
type TaskManager struct {
	mu        sync.RWMutex
	cancelers map[uint]context.CancelFunc
	states    map[uint]*taskState
	grouper   *AlbumGrouper
	dedup     *Deduper
	tg        *telegramRuntimeManager
}

type TaskCounters struct {
	TaskID     uint `json:"task_id"`
	Status     int  `json:"status"`
	Total      int  `json:"total"`
	Processed  int  `json:"processed"`
	Success    int  `json:"success"`
	Fail       int  `json:"fail"`
	Realtime   bool `json:"realtime"`
	Completed  bool `json:"completed"`
	HasRuntime bool `json:"has_runtime"`
}

var Manager = &TaskManager{
	cancelers: make(map[uint]context.CancelFunc),
	states:    make(map[uint]*taskState),
	grouper:   NewAlbumGrouper(150 * time.Millisecond),
	dedup:     NewDeduper(10*time.Minute, 50_000),
	tg:        newTelegramRuntimeManager(),
}

type taskState struct {
	TaskID uint

	RunID  uint64
	Status int

	Realtime  bool
	Completed bool

	Total     int
	Processed int
	Success   int
	Fail      int

	SpeedBaseTime      time.Time
	SpeedBaseProcessed int

	Logs []string
}

const (
	defaultTotalMsg = 2798
	maxLogs         = 50
	maxRespLogs     = 20
)

func (m *TaskManager) StartTask(t model.Task) {
	if t.ID == 0 {
		return
	}

	m.mu.Lock()

	if m.grouper != nil {
		m.grouper.DropTask(t.ID)
	}
	m.unregisterTask(t.ID, 0)

	if cancel, ok := m.cancelers[t.ID]; ok {
		cancel()
		delete(m.cancelers, t.ID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelers[t.ID] = cancel

	st := m.ensureStateLocked(t)
	st.RunID++
	runID := st.RunID

	// Restart from scratch if it has been completed before.
	if st.Completed && st.Status == model.TaskStatusStopped {
		st.Processed = 0
		st.Success = 0
		st.Fail = 0
		st.Completed = false
		st.Logs = nil
	}

	st.Status = model.TaskStatusRunning
	st.Realtime = t.Realtime
	if st.Total <= 0 {
		st.Total = inferTotal(t)
	}
	st.SpeedBaseTime = time.Now()
	st.SpeedBaseProcessed = st.Processed
	st.appendLogLocked(fmt.Sprintf("开始转发任务 [%d]: %s -> %s", t.ID, t.SourceURL, t.TargetURL))

	m.mu.Unlock()

	if global.Logger != nil {
		global.Logger.Info("task started", zap.Uint("task_id", t.ID))
	}

	go m.runTransferLoop(ctx, t, runID)
}

func (m *TaskManager) PauseTask(taskID uint) {
	if taskID == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.grouper != nil {
		m.grouper.DropTask(taskID)
	}
	m.unregisterTask(taskID, 0)

	if cancel, ok := m.cancelers[taskID]; ok {
		cancel()
		delete(m.cancelers, taskID)
	}

	st := m.states[taskID]
	if st == nil {
		st = &taskState{TaskID: taskID}
		m.states[taskID] = st
	}

	st.RunID++
	st.Status = model.TaskStatusPaused
	st.SpeedBaseTime = time.Time{}
	st.SpeedBaseProcessed = st.Processed
	st.appendLogLocked("任务已暂停")
}

func (m *TaskManager) StopTask(taskID uint) {
	if taskID == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.grouper != nil {
		m.grouper.DropTask(taskID)
	}
	m.unregisterTask(taskID, 0)

	if cancel, ok := m.cancelers[taskID]; ok {
		cancel()
		delete(m.cancelers, taskID)
	}

	st := m.states[taskID]
	if st == nil {
		st = &taskState{TaskID: taskID}
		m.states[taskID] = st
	}

	st.RunID++
	st.Status = model.TaskStatusStopped
	st.Realtime = false
	st.Completed = false
	st.Processed = 0
	st.Success = 0
	st.Fail = 0
	st.SpeedBaseTime = time.Time{}
	st.SpeedBaseProcessed = 0
	st.Logs = nil
	st.appendLogLocked("任务已停止")
}

func (m *TaskManager) Shutdown(ctx context.Context) {
	if m == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var ids []uint
	var cancelers []context.CancelFunc

	m.mu.Lock()
	for id, cancel := range m.cancelers {
		ids = append(ids, id)
		cancelers = append(cancelers, cancel)
	}
	m.cancelers = make(map[uint]context.CancelFunc)
	m.mu.Unlock()

	for _, cancel := range cancelers {
		if cancel != nil {
			cancel()
		}
	}

	for _, id := range ids {
		m.unregisterTask(id, 0)
	}

	if m.tg != nil {
		for _, rt := range m.tg.snapshot() {
			if rt != nil {
				rt.shutdown()
			}
		}
	}

	select {
	case <-ctx.Done():
	default:
	}
}

func (m *TaskManager) GetTaskProgress(t model.Task) TaskProgress {
	if t.ID == 0 {
		return TaskProgress{}
	}

	m.getOrCreateState(t)

	m.mu.RLock()
	st := m.states[t.ID]
	if st == nil {
		m.mu.RUnlock()
		return TaskProgress{TaskID: t.ID}
	}
	progress := snapshotLocked(time.Now(), st)
	m.mu.RUnlock()
	return progress
}

func (m *TaskManager) GetTaskCounters(t model.Task) TaskCounters {
	if t.ID == 0 {
		return TaskCounters{}
	}

	m.getOrCreateState(t)

	m.mu.RLock()
	st := m.states[t.ID]
	_, hasRuntime := m.cancelers[t.ID]
	if st == nil {
		m.mu.RUnlock()
		return TaskCounters{TaskID: t.ID, HasRuntime: hasRuntime}
	}
	out := TaskCounters{
		TaskID:     st.TaskID,
		Status:     st.Status,
		Total:      st.Total,
		Processed:  st.Processed,
		Success:    st.Success,
		Fail:       st.Fail,
		Realtime:   st.Realtime,
		Completed:  st.Completed,
		HasRuntime: hasRuntime,
	}
	m.mu.RUnlock()
	return out
}

func (m *TaskManager) getOrCreateState(t model.Task) *taskState {
	m.mu.RLock()
	st := m.states[t.ID]
	m.mu.RUnlock()

	if st != nil {
		m.syncFromDB(t, st)
		return st
	}

	m.mu.Lock()
	st = m.ensureStateLocked(t)
	m.mu.Unlock()
	return st
}

func (m *TaskManager) ensureStateLocked(t model.Task) *taskState {
	st := m.states[t.ID]
	if st == nil {
		st = &taskState{TaskID: t.ID}
		m.states[t.ID] = st
	}

	if st.Total <= 0 {
		st.Total = inferTotal(t)
	}
	if st.Status == 0 && t.Status != 0 {
		st.Status = t.Status
	} else if st.Status == 0 && t.Status == 0 {
		st.Status = model.TaskStatusStopped
	}
	st.Realtime = t.Realtime

	return st
}

func (m *TaskManager) syncFromDB(t model.Task, st *taskState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if st == nil {
		return
	}

	cur := m.states[t.ID]
	if cur == nil {
		return
	}
	if cur != st {
		st = cur
	}

	if st.Total <= 0 {
		st.Total = inferTotal(t)
	}
	st.Realtime = t.Realtime

	if st.Status != t.Status {
		st.Status = t.Status
		if st.Status != model.TaskStatusRunning {
			st.SpeedBaseTime = time.Time{}
			st.SpeedBaseProcessed = st.Processed
		}
	}
}

func snapshotLocked(now time.Time, st *taskState) TaskProgress {
	processed := st.Processed
	total := st.Total
	if total > 0 && processed > total {
		processed = total
	}
	if processed < 0 {
		processed = 0
	}

	progressPct := 0
	if total > 0 {
		progressPct = int(float64(processed) / float64(total) * 100)
		if progressPct > 100 {
			progressPct = 100
		}
	}

	statusText := statusTextByCode(st.Status)
	if st.Completed && !st.Realtime {
		statusText = "已完成"
	}

	speedText := "0 消息/秒"
	if st.Status == model.TaskStatusRunning && !st.SpeedBaseTime.IsZero() {
		elapsed := now.Sub(st.SpeedBaseTime).Seconds()
		if elapsed > 0 {
			speed := float64(st.Processed-st.SpeedBaseProcessed) / elapsed
			if speed < 0 {
				speed = 0
			}
			speedText = fmt.Sprintf("%.1f 消息/秒", speed)
		}
	}

	return TaskProgress{
		TaskID:       st.TaskID,
		Status:       statusText,
		Speed:        speedText,
		ProgressPct:  progressPct,
		ProcessedCnt: processed,
		SuccessCnt:   st.Success,
		FailCnt:      st.Fail,
		TotalMsg:     total,
		Logs:         tail(st.Logs, maxRespLogs),
	}
}

func (st *taskState) appendLogLocked(msg string) {
	ts := time.Now().Format("15:04:05")
	st.Logs = append(st.Logs, fmt.Sprintf("[%s] %s", ts, msg))
	if len(st.Logs) > maxLogs {
		st.Logs = st.Logs[len(st.Logs)-maxLogs:]
	}
	if st.TaskID > 0 {
		global.BroadcastLog(fmt.Sprintf("[Task-%d] %s", st.TaskID, msg))
	} else {
		global.BroadcastLog(msg)
	}
}

func (st *taskState) appendLogLockedNoBroadcast(msg string) {
	ts := time.Now().Format("15:04:05")
	st.Logs = append(st.Logs, fmt.Sprintf("[%s] %s", ts, msg))
	if len(st.Logs) > maxLogs {
		st.Logs = st.Logs[len(st.Logs)-maxLogs:]
	}
}

func statusTextByCode(code int) string {
	switch code {
	case model.TaskStatusStopped:
		return "已停止"
	case model.TaskStatusRunning:
		return "进行中"
	case model.TaskStatusPaused:
		return "已暂停"
	case model.TaskStatusError:
		return "异常"
	default:
		return "未知"
	}
}

func tail[T any](in []T, n int) []T {
	if n <= 0 || len(in) == 0 {
		return nil
	}
	if len(in) <= n {
		out := make([]T, len(in))
		copy(out, in)
		return out
	}
	out := make([]T, n)
	copy(out, in[len(in)-n:])
	return out
}

func inferTotal(t model.Task) int {
	switch t.ScopeType {
	case 2: // 最近 N 条
		ints := extractInts(t.ScopeValue, 1)
		if len(ints) == 1 && ints[0] > 0 {
			return ints[0]
		}
	case 4: // ID 范围
		ints := extractInts(t.ScopeValue, 2)
		if len(ints) >= 2 && ints[0] > 0 && ints[1] > 0 {
			start, end := ints[0], ints[1]
			if end < start {
				start, end = end, start
			}
			if end-start+1 > 0 {
				return end - start + 1
			}
		}
	}

	return defaultTotalMsg
}

func extractInts(s string, max int) []int {
	out := make([]int, 0, 2)
	n := 0
	inNumber := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
			inNumber = true
			continue
		}
		if inNumber {
			out = append(out, n)
			if max > 0 && len(out) >= max {
				return out
			}
			n = 0
			inNumber = false
		}
	}
	if inNumber {
		out = append(out, n)
	}
	return out
}
