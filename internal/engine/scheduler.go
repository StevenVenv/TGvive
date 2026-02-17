package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"go.uber.org/zap"
)

// ScheduleRule controls pull-based forwarding quota in a time slot.
// It is stored in Strategy.ScheduleRules as JSON array:
// [{ "start":"10:00", "end":"11:00", "limit":2 }, ...]
type ScheduleRule struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Limit int    `json:"limit"`
}

type parsedScheduleRule struct {
	startRaw string
	endRaw   string

	startMin int
	endMin   int
	limit    int

	allDay        bool
	crossMidnight bool
}

func parseScheduleRules(raw []byte) ([]parsedScheduleRule, error) {
	b := raw
	if len(b) == 0 {
		return nil, nil
	}
	if string(b) == "null" {
		return nil, nil
	}

	var in []ScheduleRule
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	if len(in) == 0 {
		return nil, nil
	}

	out := make([]parsedScheduleRule, 0, len(in))
	for _, r := range in {
		start := r.Start
		end := r.End
		startMin, err := parseHHMM(start)
		if err != nil {
			continue
		}
		endMin, err := parseHHMM(end)
		if err != nil {
			continue
		}

		limit := r.Limit
		if limit < 0 {
			limit = 0
		}

		pr := parsedScheduleRule{
			startRaw: start,
			endRaw:   end,
			startMin: startMin,
			endMin:   endMin,
			limit:    limit,
		}
		if startMin == endMin {
			pr.allDay = true
		} else if startMin > endMin {
			pr.crossMidnight = true
		}
		out = append(out, pr)
	}
	return out, nil
}

func (r parsedScheduleRule) contains(now time.Time) bool {
	if r.allDay {
		return true
	}
	minutes := now.Hour()*60 + now.Minute()
	if !r.crossMidnight {
		return minutes >= r.startMin && minutes < r.endMin
	}
	return minutes >= r.startMin || minutes < r.endMin
}

func (r parsedScheduleRule) window(now time.Time) (start time.Time, end time.Time) {
	loc := now.Location()
	minutes := now.Hour()*60 + now.Minute()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	if r.allDay {
		start = day
		end = day.Add(24 * time.Hour)
		return start, end
	}

	if !r.crossMidnight {
		start = timeAtMinutes(now, r.startMin)
		end = timeAtMinutes(now, r.endMin)
		return start, end
	}

	// Cross midnight window.
	if minutes >= r.startMin {
		// today start -> tomorrow end
		start = timeAtMinutes(now, r.startMin)
		end = timeAtMinutes(now.AddDate(0, 0, 1), r.endMin)
		return start, end
	}
	// yesterday start -> today end
	start = timeAtMinutes(now.AddDate(0, 0, -1), r.startMin)
	end = timeAtMinutes(now, r.endMin)
	return start, end
}

func (r parsedScheduleRule) nextStart(now time.Time) time.Time {
	if r.allDay {
		return now
	}
	minutes := now.Hour()*60 + now.Minute()

	if !r.crossMidnight {
		if minutes < r.startMin {
			return timeAtMinutes(now, r.startMin)
		}
		return timeAtMinutes(now.AddDate(0, 0, 1), r.startMin)
	}

	// Not allowed implies minutes in [endMin, startMin).
	if minutes >= r.endMin && minutes < r.startMin {
		return timeAtMinutes(now, r.startMin)
	}
	// Within the slot now, next "start" is tomorrow at startMin.
	return timeAtMinutes(now.AddDate(0, 0, 1), r.startMin)
}

type scheduleSlot struct {
	id    string
	start time.Time
	end   time.Time
	limit int
}

func pickActiveSlot(now time.Time, rules []parsedScheduleRule) (scheduleSlot, bool) {
	var picked scheduleSlot
	ok := false
	for _, r := range rules {
		if r.limit <= 0 {
			continue
		}
		if !r.contains(now) {
			continue
		}
		start, end := r.window(now)
		id := fmt.Sprintf("%s_%d_%d", start.Format("2006-01-02"), r.startMin, r.endMin)
		if !ok || end.Before(picked.end) {
			picked = scheduleSlot{id: id, start: start, end: end, limit: r.limit}
			ok = true
		}
	}
	return picked, ok
}

func nextSlotStart(now time.Time, rules []parsedScheduleRule) (time.Time, bool) {
	var next time.Time
	ok := false
	for _, r := range rules {
		if r.limit <= 0 {
			continue
		}
		t := r.nextStart(now)
		if t.IsZero() {
			continue
		}
		if !ok || t.Before(next) {
			next = t
			ok = true
		}
	}
	return next, ok
}

type taskScheduleState struct {
	// immutable-ish
	userID     uint
	strategyID uint
	rules      []parsedScheduleRule
	rulesKey   string

	// current slot runtime
	slotID string
	end    time.Time
	limit  int
	used   int

	nextRun time.Time
}

// SchedulerEngine keeps in-memory pull quotas based on Strategy.ScheduleRules.
// It is best-effort (development mode): restarting server will reset per-slot counters.
type SchedulerEngine struct {
	mu sync.Mutex

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}

	// key: task_id
	tasks map[uint]*taskScheduleState
}

func NewSchedulerEngine() *SchedulerEngine {
	return &SchedulerEngine{
		stopCh: make(chan struct{}),
		tasks:  make(map[uint]*taskScheduleState),
	}
}

var Scheduler = NewSchedulerEngine()

func (s *SchedulerEngine) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.loop()
	})
}

func (s *SchedulerEngine) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *SchedulerEngine) loop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// initial refresh
	s.refresh()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.refresh()
		}
	}
}

func (s *SchedulerEngine) refresh() {
	if s == nil || global.DB == nil {
		return
	}

	var running []model.Task
	if err := global.DB.Select("id", "user_id", "strategy_id", "status").Where("status = ?", model.TaskStatusRunning).Find(&running).Error; err != nil {
		if global.Logger != nil {
			global.Logger.Warn("scheduler refresh tasks failed", zap.Error(err))
		}
		return
	}

	keep := make(map[uint]struct{}, len(running))
	now := time.Now()

	for _, t := range running {
		if t.ID == 0 || t.StrategyID == 0 {
			continue
		}
		keep[t.ID] = struct{}{}

		var st model.Strategy
		if err := global.DB.Select("schedule_rules").Where("id = ? AND user_id = ?", t.StrategyID, t.UserID).First(&st).Error; err != nil {
			continue
		}
		s.setRules(t.ID, t.UserID, t.StrategyID, st.ScheduleRules)
		next := s.computeNextRun(t.ID, now)
		_ = global.DB.Model(&model.Task{}).Where("id = ?", t.ID).Update("next_run_time", nullableTimePtr(next)).Error
	}

	s.mu.Lock()
	for id := range s.tasks {
		if _, ok := keep[id]; !ok {
			delete(s.tasks, id)
		}
	}
	s.mu.Unlock()
}

func nullableTimePtr(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *SchedulerEngine) setRules(taskID uint, userID uint, strategyID uint, raw []byte) {
	if s == nil || taskID == 0 {
		return
	}

	rules, err := parseScheduleRules(raw)
	if err != nil {
		if global.Logger != nil {
			global.Logger.Warn("parse schedule rules failed, ignored", zap.Uint("task_id", taskID), zap.Error(err))
		}
		rules = nil
	}
	key := scheduleRulesKey(rules)

	s.mu.Lock()
	st := s.tasks[taskID]
	if st == nil {
		st = &taskScheduleState{}
		s.tasks[taskID] = st
	}

	// If strategy changed, reset runtime counters.
	if st.strategyID != strategyID || st.userID != userID {
		st.slotID = ""
		st.used = 0
		st.end = time.Time{}
		st.limit = 0
		st.nextRun = time.Time{}
		st.rulesKey = ""
	}
	if st.rulesKey != key {
		st.slotID = ""
		st.used = 0
		st.end = time.Time{}
		st.limit = 0
		st.nextRun = time.Time{}
		st.rulesKey = key
	}

	st.userID = userID
	st.strategyID = strategyID
	st.rules = rules
	s.mu.Unlock()
}

func scheduleRulesKey(rules []parsedScheduleRule) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range rules {
		_, _ = fmt.Fprintf(&b, "%d-%d-%d|", r.startMin, r.endMin, r.limit)
	}
	return b.String()
}

// ReservePull checks current time slot and reserves up to `need` pull tokens for this task.
// It returns reserved count (0 means not allowed right now) and next run time hint.
func (s *SchedulerEngine) ReservePull(taskID uint, need int) (int, time.Time, error) {
	if need <= 0 {
		return 0, time.Time{}, nil
	}
	if s == nil || taskID == 0 {
		return need, time.Time{}, nil
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.tasks[taskID]
	if st == nil || len(st.rules) == 0 {
		return need, time.Time{}, nil
	}

	slot, ok := pickActiveSlot(now, st.rules)
	if !ok {
		next, has := nextSlotStart(now, st.rules)
		if has {
			st.nextRun = next
			return 0, next, nil
		}
		// No valid rules -> unlimited.
		return need, time.Time{}, nil
	}

	// Slot changed -> reset.
	if st.slotID != slot.id {
		st.slotID = slot.id
		st.used = 0
		st.end = slot.end
		st.limit = slot.limit
		st.nextRun = time.Time{}
	}

	remaining := st.limit - st.used
	if remaining <= 0 {
		next := st.end
		st.nextRun = next
		return 0, next, nil
	}

	reserved := need
	if reserved > remaining {
		reserved = remaining
	}
	st.used += reserved
	return reserved, time.Time{}, nil
}

func (s *SchedulerEngine) computeNextRun(taskID uint, now time.Time) time.Time {
	if s == nil || taskID == 0 {
		return time.Time{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.tasks[taskID]
	if st == nil || len(st.rules) == 0 {
		return time.Time{}
	}

	slot, ok := pickActiveSlot(now, st.rules)
	if !ok {
		next, has := nextSlotStart(now, st.rules)
		if has {
			st.nextRun = next
			return next
		}
		st.nextRun = time.Time{}
		return time.Time{}
	}

	if st.slotID != slot.id {
		st.slotID = slot.id
		st.used = 0
		st.end = slot.end
		st.limit = slot.limit
		st.nextRun = time.Time{}
		return time.Time{}
	}

	if st.limit <= 0 {
		st.nextRun = time.Time{}
		return time.Time{}
	}
	if st.used >= st.limit {
		st.nextRun = st.end
		return st.end
	}

	st.nextRun = time.Time{}
	return time.Time{}
}

func (s *SchedulerEngine) RegisterTask(task model.Task) error {
	if s == nil {
		return nil
	}
	if task.ID == 0 || task.StrategyID == 0 || task.UserID == 0 {
		return nil
	}
	if global.DB == nil {
		return errors.New("db is nil")
	}

	var st model.Strategy
	if err := global.DB.Select("schedule_rules").Where("id = ? AND user_id = ?", task.StrategyID, task.UserID).First(&st).Error; err != nil {
		return err
	}
	s.setRules(task.ID, task.UserID, task.StrategyID, st.ScheduleRules)
	return nil
}

// PeekPull reports whether pull operations are allowed right now, and the remaining quota in current slot.
// remaining < 0 means unlimited (no schedule rules).
func (s *SchedulerEngine) PeekPull(taskID uint) (allowed bool, remaining int, next time.Time) {
	if s == nil || taskID == 0 {
		return true, -1, time.Time{}
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.tasks[taskID]
	if st == nil || len(st.rules) == 0 {
		return true, -1, time.Time{}
	}

	slot, ok := pickActiveSlot(now, st.rules)
	if !ok {
		next, has := nextSlotStart(now, st.rules)
		if has {
			st.nextRun = next
			return false, 0, next
		}
		return true, -1, time.Time{}
	}

	if st.slotID != slot.id {
		st.slotID = slot.id
		st.used = 0
		st.end = slot.end
		st.limit = slot.limit
		st.nextRun = time.Time{}
	}

	remaining = st.limit - st.used
	if remaining <= 0 {
		next = st.end
		st.nextRun = next
		return false, 0, next
	}
	return true, remaining, time.Time{}
}
