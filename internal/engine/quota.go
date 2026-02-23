package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"gorm.io/gorm"
)

const quotaPollInterval = 10 * time.Minute

type runWindow struct {
	enabled bool

	startMin int
	endMin   int

	allDay        bool
	crossMidnight bool
}

func parseRunWindow(raw string) (runWindow, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return runWindow{}, nil
	}

	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "—", "-")
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "～", "-")
	s = strings.ReplaceAll(s, "~", "-")

	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return runWindow{}, fmt.Errorf("invalid run window %q", raw)
	}

	startMin, err := parseHHMM(parts[0])
	if err != nil {
		return runWindow{}, fmt.Errorf("invalid run window start %q: %w", parts[0], err)
	}
	endMin, err := parseHHMM(parts[1])
	if err != nil {
		return runWindow{}, fmt.Errorf("invalid run window end %q: %w", parts[1], err)
	}

	w := runWindow{
		enabled:  true,
		startMin: startMin,
		endMin:   endMin,
	}
	if startMin == endMin {
		w.allDay = true
		return w, nil
	}
	if startMin > endMin {
		w.crossMidnight = true
	}
	return w, nil
}

func parseHHMM(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty time")
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid HH:MM %q", s)
	}

	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid hour %q", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid minute %q", parts[1])
	}

	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("out of range %q", s)
	}
	return h*60 + m, nil
}

func (w runWindow) contains(now time.Time) bool {
	if !w.enabled || w.allDay {
		return true
	}

	minutes := now.Hour()*60 + now.Minute()
	if !w.crossMidnight {
		return minutes >= w.startMin && minutes < w.endMin
	}
	return minutes >= w.startMin || minutes < w.endMin
}

func (w runWindow) nextStart(now time.Time) time.Time {
	if !w.enabled || w.allDay {
		return now
	}

	minutes := now.Hour()*60 + now.Minute()
	if !w.crossMidnight {
		if minutes < w.startMin {
			return timeAtMinutes(now, w.startMin)
		}
		// After end.
		return timeAtMinutes(now.AddDate(0, 0, 1), w.startMin)
	}

	// Not allowed implies minutes in [endMin, startMin).
	return timeAtMinutes(now, w.startMin)
}

func timeAtMinutes(t time.Time, minutes int) time.Time {
	if minutes < 0 {
		minutes = 0
	}
	if minutes > 23*60+59 {
		minutes = 23*60 + 59
	}
	h := minutes / 60
	m := minutes % 60
	return time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, t.Location())
}

func durationUntilNextMidnight(now time.Time) time.Duration {
	loc := now.Location()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	return next.Sub(now)
}

type taskQuota struct {
	dailyLimit int
	windowRaw  string
	window     runWindow
	windowErr  error

	todayDate  string
	todayCount int

	pausedReason string // "", "daily", "window"
}

func newTaskQuota(task model.Task) *taskQuota {
	q := &taskQuota{
		dailyLimit: task.DailyLimit,
		windowRaw:  strings.TrimSpace(task.RunWindow),
		todayDate:  strings.TrimSpace(task.TodayDate),
		todayCount: task.TodayCount,
	}
	if q.dailyLimit < 0 {
		q.dailyLimit = 0
	}
	if q.todayCount < 0 {
		q.todayCount = 0
	}
	if q.windowRaw != "" {
		w, err := parseRunWindow(q.windowRaw)
		if err != nil {
			q.windowErr = err
		} else {
			q.window = w
		}
	}
	return q
}

func (q *taskQuota) UpdateConfig(dailyLimit int, windowRaw string) {
	if q == nil {
		return
	}
	if dailyLimit < 0 {
		dailyLimit = 0
	}
	windowRaw = strings.TrimSpace(windowRaw)
	if q.dailyLimit == dailyLimit && strings.TrimSpace(q.windowRaw) == windowRaw {
		return
	}

	q.dailyLimit = dailyLimit
	q.windowRaw = windowRaw
	q.windowErr = nil
	q.window = runWindow{}

	if windowRaw == "" {
		return
	}
	w, err := parseRunWindow(windowRaw)
	if err != nil {
		q.windowErr = err
		return
	}
	q.window = w
}

func (m *TaskManager) waitForQuota(ctx context.Context, taskID uint, runID uint64, q *taskQuota, need int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || q == nil || taskID == 0 {
		return nil
	}
	if need < 0 {
		need = 0
	}

	windowWarned := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		now := time.Now()
		today := now.Format("2006-01-02")
		if strings.TrimSpace(q.todayDate) != today {
			q.todayDate = today
			q.todayCount = 0
			if err := persistQuotaReset(ctx, taskID, today); err != nil {
				return err
			}
		}

		if q.windowErr != nil && !windowWarned {
			windowWarned = true
			if runID != 0 {
				m.record(taskID, runID, 0, 0, 0, 0, "[警告] run_window 配置无效，已忽略: "+q.windowErr.Error())
			}
		}

		if q.windowErr == nil && q.window.enabled && !q.window.contains(now) {
			next := q.window.nextStart(now)
			sleepFor := next.Sub(now)
			if sleepFor < 0 {
				sleepFor = quotaPollInterval
			}
			sleepFor = minDuration(sleepFor, quotaPollInterval)

			m.markQuotaPaused(ctx, taskID, runID, q, "window", fmt.Sprintf("当前不在运行时间段(%s)", q.windowRaw))
			sleepWithContext(ctx, sleepFor)
			continue
		}

		if q.dailyLimit > 0 && q.todayCount+need > q.dailyLimit {
			sleepFor := durationUntilNextMidnight(now)
			if sleepFor < 0 {
				sleepFor = quotaPollInterval
			}
			sleepFor = minDuration(sleepFor, quotaPollInterval)

			m.markQuotaPaused(ctx, taskID, runID, q, "daily", fmt.Sprintf("今日配额已用完 (%d/%d)", q.todayCount, q.dailyLimit))
			sleepWithContext(ctx, sleepFor)
			continue
		}

		if need == 0 && q.pausedReason == "daily" && q.dailyLimit > 0 && q.todayCount >= q.dailyLimit {
			// Special case: allow non-quota operations (need=0) to proceed even when daily quota is exhausted,
			// but keep task status as paused to reflect that quota-consuming jobs are still blocked.
			return nil
		}

		m.markQuotaRunning(ctx, taskID, runID, q)
		return nil
	}
}

func (m *TaskManager) quotaAdd(ctx context.Context, taskID uint, q *taskQuota, delta int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q == nil || taskID == 0 || delta <= 0 {
		return nil
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	if strings.TrimSpace(q.todayDate) != today {
		q.todayDate = today
		q.todayCount = 0
		if err := persistQuotaReset(ctx, taskID, today); err != nil {
			return err
		}
	}

	if err := persistQuotaIncrement(ctx, taskID, today, delta); err != nil {
		return err
	}
	q.todayCount += delta
	return nil
}

func (m *TaskManager) markQuotaPaused(ctx context.Context, taskID uint, runID uint64, q *taskQuota, reason string, msg string) {
	if m == nil || q == nil || taskID == 0 {
		return
	}
	if q.pausedReason == reason {
		return
	}
	q.pausedReason = reason

	if runID != 0 {
		m.record(taskID, runID, 0, 0, 0, 0, "[暂停] "+msg)
		m.setStateStatus(taskID, runID, model.TaskStatusPaused)
	}
	_ = updateTaskStatus(ctx, taskID, model.TaskStatusPaused)
}

func (m *TaskManager) markQuotaRunning(ctx context.Context, taskID uint, runID uint64, q *taskQuota) {
	if m == nil || q == nil || taskID == 0 {
		return
	}
	if q.pausedReason == "" {
		return
	}
	prev := q.pausedReason
	q.pausedReason = ""

	if runID != 0 {
		m.record(taskID, runID, 0, 0, 0, 0, "[继续] 恢复运行("+prev+")")
		m.setStateStatus(taskID, runID, model.TaskStatusRunning)
	}
	_ = updateTaskStatus(ctx, taskID, model.TaskStatusRunning)
}

func persistQuotaReset(ctx context.Context, taskID uint, today string) error {
	if taskID == 0 {
		return errors.New("task id is required")
	}
	if global.DB == nil {
		return nil
	}
	today = strings.TrimSpace(today)
	if ctx == nil {
		ctx = context.Background()
	}
	return global.DB.WithContext(ctx).Model(&model.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"today_date":  today,
			"today_count": 0,
		}).Error
}

func persistQuotaIncrement(ctx context.Context, taskID uint, today string, delta int) error {
	if taskID == 0 {
		return errors.New("task id is required")
	}
	if delta <= 0 {
		return nil
	}
	if global.DB == nil {
		return nil
	}
	today = strings.TrimSpace(today)

	if ctx == nil {
		ctx = context.Background()
	}
	return global.DB.WithContext(ctx).Model(&model.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"today_date":  today,
			"today_count": gorm.Expr("today_count + ?", delta),
		}).Error
}

func quotaSendableCount(m *TaskManager, msg *tg.Message, allowedTypes map[string]struct{}) int {
	if m == nil || msg == nil {
		return 0
	}
	if allowedTypes != nil {
		ct := m.DetectContentType(msg)
		if _, ok := allowedTypes[ct]; !ok {
			return 0
		}
	}

	if msg.Media == nil {
		if strings.TrimSpace(msg.Message) == "" {
			return 0
		}
		return 1
	}

	if _, err := convertMessageMediaToInput(msg.Media); err != nil {
		if errors.Is(err, ErrUnsupportedMedia) && strings.TrimSpace(msg.Message) != "" {
			return 1
		}
		return 0
	}
	return 1
}

// quotaUnitsForSingle converts a sendable message count into quota "units":
// - Non-sendable messages -> 0
// - When task.KeepReply is enabled, reply messages do not consume quota -> 0
// - Otherwise -> 1
func quotaUnitsForSingle(task model.Task, msg *tg.Message, sendableCount int) int {
	if sendableCount <= 0 || msg == nil {
		return 0
	}
	if task.KeepReply && extractReplyToSourceMsgID(msg) > 0 {
		return 0
	}
	return 1
}

// quotaUnitsForBatch converts a sendable album batch into quota "units":
// - Empty/non-sendable batch -> 0
// - When task.KeepReply is enabled, reply batches do not consume quota -> 0
// - Otherwise -> 1 (album counts as one)
func quotaUnitsForBatch(task model.Task, batch []*tg.Message, sendableCount int) int {
	if sendableCount <= 0 {
		return 0
	}
	if task.KeepReply {
		for _, msg := range batch {
			if extractReplyToSourceMsgID(msg) > 0 {
				return 0
			}
		}
	}
	return 1
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return 0
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
