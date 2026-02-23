package engine

import (
	"context"
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
	startMin int
	endMin   int
	limit    int

	allDay        bool
	crossMidnight bool
}

func parseScheduleRules(raw []byte) ([]parsedScheduleRule, []string, error) {
	b := raw
	if len(b) == 0 {
		return nil, nil, nil
	}
	if string(b) == "null" {
		return nil, nil, nil
	}

	var in []ScheduleRule
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, nil, err
	}
	if len(in) == 0 {
		return nil, nil, nil
	}

	out := make([]parsedScheduleRule, 0, len(in))
	var warns []string
	for i, r := range in {
		start := r.Start
		end := r.End
		startMin, err := parseHHMM(start)
		if err != nil {
			warns = append(warns, fmt.Sprintf("rule[%d] start=%q: %v", i, start, err))
			continue
		}
		endMin, err := parseHHMM(end)
		if err != nil {
			warns = append(warns, fmt.Sprintf("rule[%d] end=%q: %v", i, end, err))
			continue
		}

		limit := r.Limit
		if limit < 0 {
			limit = 0
		}

		pr := parsedScheduleRule{
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
	return out, warns, nil
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

func (r parsedScheduleRule) endTime(now time.Time) time.Time {
	if r.allDay {
		loc := now.Location()
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return day.AddDate(0, 0, 1)
	}

	minutes := now.Hour()*60 + now.Minute()

	if !r.crossMidnight {
		return timeAtMinutes(now, r.endMin)
	}

	if minutes >= r.startMin {
		return timeAtMinutes(now.AddDate(0, 0, 1), r.endMin)
	}
	return timeAtMinutes(now, r.endMin)
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

func pickActiveSlot(now time.Time, rules []parsedScheduleRule) (ruleIdx int, slotEnd time.Time, limit int, ok bool) {
	ruleIdx = -1
	for idx, r := range rules {
		if r.limit <= 0 {
			continue
		}
		if !r.contains(now) {
			continue
		}
		end := r.endTime(now)
		if ruleIdx < 0 || end.Before(slotEnd) {
			ruleIdx = idx
			slotEnd = end
			limit = r.limit
		}
	}
	if ruleIdx < 0 {
		return -1, time.Time{}, 0, false
	}
	return ruleIdx, slotEnd, limit, true
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
	activeRuleIdx int
	slotEnd       time.Time
	limit         int
	used          int
	reserved      int

	currentSlotKey string

	nextRun time.Time
}

// SchedulerEngine keeps pull quotas based on Strategy.ScheduleRules and persists slot counters on task table.
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

func (s *SchedulerEngine) RefreshNow() {
	if s == nil {
		return
	}
	s.refresh()
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

	ctx := context.Background()
	db := global.DB.WithContext(ctx)

	type row struct {
		TaskID        uint   `gorm:"column:task_id"`
		UserID        uint   `gorm:"column:user_id"`
		StrategyID    uint   `gorm:"column:strategy_id"`
		SlotKey       string `gorm:"column:current_slot_key"`
		SlotCount     int    `gorm:"column:current_slot_count"`
		ScheduleRules []byte `gorm:"column:schedule_rules"`
	}

	var rows []row
	if err := db.
		Table("task").
		Select("task.id as task_id, task.user_id, task.strategy_id, task.current_slot_key, task.current_slot_count, strategy.schedule_rules as schedule_rules").
		Joins("LEFT JOIN strategy ON strategy.id = task.strategy_id AND strategy.user_id = task.user_id").
		Where("task.status = ? AND task.strategy_id <> 0", model.TaskStatusRunning).
		Scan(&rows).Error; err != nil {
		if global.Logger != nil {
			global.Logger.Warn("scheduler refresh tasks failed", zap.Error(err))
		}
		return
	}

	keep := make(map[uint]struct{}, len(rows))
	now := time.Now()
	type stratKey struct {
		UserID     uint
		StrategyID uint
	}
	type parsedEntry struct {
		rules    []parsedScheduleRule
		rulesKey string
	}
	cache := make(map[stratKey]parsedEntry)

	for _, r := range rows {
		if r.TaskID == 0 || r.StrategyID == 0 {
			continue
		}
		keep[r.TaskID] = struct{}{}

		key := stratKey{UserID: r.UserID, StrategyID: r.StrategyID}
		ent, ok := cache[key]
		if !ok {
			rules, warns, err := parseScheduleRules(r.ScheduleRules)
			if err != nil {
				if global.Logger != nil {
					global.Logger.Warn("parse schedule rules failed, ignored", zap.Uint("strategy_id", r.StrategyID), zap.Uint("user_id", r.UserID), zap.Error(err))
				}
				rules = nil
			}
			if len(warns) > 0 && global.Logger != nil {
				sample := warns
				if len(sample) > 3 {
					sample = sample[:3]
				}
				global.Logger.Warn(
					"invalid schedule rule ignored",
					zap.Uint("strategy_id", r.StrategyID),
					zap.Uint("user_id", r.UserID),
					zap.Int("count", len(warns)),
					zap.Strings("sample", sample),
				)
			}
			ent = parsedEntry{rules: rules, rulesKey: scheduleRulesKey(rules)}
			cache[key] = ent
		}

		persistReset := s.applyRules(r.TaskID, r.UserID, r.StrategyID, ent.rules, ent.rulesKey)
		if !persistReset {
			s.syncPersistedState(r.TaskID, r.SlotKey, r.SlotCount)
		}
		next, slotReset := s.computeNextRun(r.TaskID, now)

		updates := map[string]any{
			"next_run_time": nullableTimePtr(next),
		}
		if persistReset {
			if slotReset != nil && strings.TrimSpace(slotReset.SlotKey) != "" {
				updates["current_slot_key"] = slotReset.SlotKey
			} else {
				updates["current_slot_key"] = ""
			}
			updates["current_slot_count"] = 0
		} else if slotReset != nil {
			updates["current_slot_key"] = slotReset.SlotKey
			updates["current_slot_count"] = 0
		}
		if err := db.Model(&model.Task{}).Where("id = ?", r.TaskID).Updates(updates).Error; err != nil && global.Logger != nil {
			global.Logger.Warn("scheduler refresh update task failed", zap.Uint("task_id", r.TaskID), zap.Error(err))
		}
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

	rules, warns, err := parseScheduleRules(raw)
	if err != nil {
		if global.Logger != nil {
			global.Logger.Warn("parse schedule rules failed, ignored", zap.Uint("task_id", taskID), zap.Error(err))
		}
		rules = nil
	}
	if len(warns) > 0 && global.Logger != nil {
		sample := warns
		if len(sample) > 3 {
			sample = sample[:3]
		}
		global.Logger.Warn(
			"invalid schedule rule ignored",
			zap.Uint("task_id", taskID),
			zap.Int("count", len(warns)),
			zap.Strings("sample", sample),
		)
	}
	key := scheduleRulesKey(rules)
	_ = s.applyRules(taskID, userID, strategyID, rules, key)
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

func (s *SchedulerEngine) applyRules(taskID uint, userID uint, strategyID uint, rules []parsedScheduleRule, rulesKey string) (persistReset bool) {
	if s == nil || taskID == 0 {
		return false
	}

	s.mu.Lock()
	st := s.tasks[taskID]
	isNew := st == nil
	if st == nil {
		st = &taskScheduleState{activeRuleIdx: -1}
		s.tasks[taskID] = st
	}

	changed := st.userID != userID || st.strategyID != strategyID || st.rulesKey != rulesKey
	persistReset = !isNew && changed
	if changed {
		st.activeRuleIdx = -1
		st.used = 0
		st.reserved = 0
		if persistReset {
			st.currentSlotKey = ""
		}
		st.slotEnd = time.Time{}
		st.limit = 0
		st.nextRun = time.Time{}
	}

	st.userID = userID
	st.strategyID = strategyID
	st.rules = rules
	st.rulesKey = rulesKey
	s.mu.Unlock()
	return persistReset
}

func (s *SchedulerEngine) syncPersistedState(taskID uint, slotKey string, slotCount int) {
	if s == nil || taskID == 0 {
		return
	}
	if slotCount < 0 {
		slotCount = 0
	}

	s.mu.Lock()
	st := s.tasks[taskID]
	if st == nil {
		s.mu.Unlock()
		return
	}

	if st.reserved > 0 && st.currentSlotKey != "" && slotKey != "" && slotKey != st.currentSlotKey {
		// Do not overwrite slot identity while there are pending reserved jobs.
		s.mu.Unlock()
		return
	}

	st.currentSlotKey = strings.TrimSpace(slotKey)
	st.used = slotCount
	if st.used < 0 {
		st.used = 0
	}
	s.mu.Unlock()
}

// ReservePull checks current time slot and reserves up to `need` pull tokens for this task.
// It returns reserved count (0 means not allowed right now) and next run time hint.
func (s *SchedulerEngine) ReservePull(ctx context.Context, taskID uint, need int) (int, time.Time, error) {
	if need <= 0 {
		return 0, time.Time{}, nil
	}
	if s == nil || taskID == 0 {
		return need, time.Time{}, nil
	}

	now := time.Now()

	var needPersistReset bool
	var persistKey string

	s.mu.Lock()

	st := s.tasks[taskID]
	if st == nil || len(st.rules) == 0 {
		s.mu.Unlock()
		return need, time.Time{}, nil
	}

	idx, end, limit, ok := pickActiveSlot(now, st.rules)
	if !ok {
		next, has := nextSlotStart(now, st.rules)
		if has {
			st.nextRun = next
			s.mu.Unlock()
			return 0, next, nil
		}
		// No valid rules -> unlimited.
		s.mu.Unlock()
		return need, time.Time{}, nil
	}

	// Slot changed -> reset.
	if st.activeRuleIdx != idx || !st.slotEnd.Equal(end) {
		// Do not switch slot while there are pending reserved jobs.
		if st.reserved > 0 {
			next := st.slotEnd
			if next.IsZero() {
				next = end
			}
			st.nextRun = next
			s.mu.Unlock()
			return 0, next, nil
		}

		newKey := buildSlotKey(now, st.rules[idx])

		st.activeRuleIdx = idx
		st.slotEnd = end
		st.limit = limit
		st.nextRun = time.Time{}

		if newKey != "" && newKey != st.currentSlotKey {
			st.currentSlotKey = newKey
			st.used = 0
			st.reserved = 0
			needPersistReset = true
			persistKey = newKey
		}
	}

	remaining := st.limit - st.used - st.reserved
	if remaining <= 0 {
		next := st.slotEnd
		st.nextRun = next
		s.mu.Unlock()
		if needPersistReset {
			_ = persistSlotReset(ctx, taskID, persistKey)
		}
		return 0, next, nil
	}

	reserved := need
	if reserved > remaining {
		reserved = remaining
	}
	st.reserved += reserved

	s.mu.Unlock()
	if needPersistReset {
		_ = persistSlotReset(ctx, taskID, persistKey)
	}
	return reserved, time.Time{}, nil
}

type slotResetInfo struct {
	SlotKey string
}

func (s *SchedulerEngine) computeNextRun(taskID uint, now time.Time) (time.Time, *slotResetInfo) {
	if s == nil || taskID == 0 {
		return time.Time{}, nil
	}

	var reset *slotResetInfo

	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.tasks[taskID]
	if st == nil || len(st.rules) == 0 {
		return time.Time{}, nil
	}

	idx, end, limit, ok := pickActiveSlot(now, st.rules)
	if !ok {
		next, has := nextSlotStart(now, st.rules)
		if has {
			st.nextRun = next
			return next, nil
		}
		st.nextRun = time.Time{}
		return time.Time{}, nil
	}

	if st.activeRuleIdx != idx || !st.slotEnd.Equal(end) {
		if st.reserved == 0 {
			st.activeRuleIdx = idx
			st.slotEnd = end
			st.limit = limit
			st.nextRun = time.Time{}

			newKey := buildSlotKey(now, st.rules[idx])
			if newKey != "" && newKey != st.currentSlotKey {
				st.currentSlotKey = newKey
				st.used = 0
				st.reserved = 0
				reset = &slotResetInfo{SlotKey: newKey}
			}
		}
		return time.Time{}, reset
	}

	if st.limit <= 0 {
		st.nextRun = time.Time{}
		return time.Time{}, reset
	}
	if st.used >= st.limit {
		st.nextRun = st.slotEnd
		return st.slotEnd, reset
	}

	st.nextRun = time.Time{}
	return time.Time{}, reset
}

func (s *SchedulerEngine) RegisterTask(ctx context.Context, task model.Task) error {
	if s == nil {
		return nil
	}
	if task.ID == 0 || task.StrategyID == 0 || task.UserID == 0 {
		return nil
	}
	if global.DB == nil {
		return errors.New("db is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db := global.DB.WithContext(ctx)

	var st model.Strategy
	if err := db.Select("schedule_rules").Where("id = ? AND user_id = ?", task.StrategyID, task.UserID).First(&st).Error; err != nil {
		return err
	}
	s.setRules(task.ID, task.UserID, task.StrategyID, st.ScheduleRules)

	// Sync persisted slot quota into memory to avoid 1-minute window after restart.
	slotKey := strings.TrimSpace(task.CurrentSlotKey)
	slotCount := task.CurrentSlotCount
	if slotKey == "" && global.DB != nil {
		var latest model.Task
		if err := db.Select("current_slot_key", "current_slot_count").Where("id = ? AND user_id = ?", task.ID, task.UserID).First(&latest).Error; err == nil {
			slotKey = strings.TrimSpace(latest.CurrentSlotKey)
			slotCount = latest.CurrentSlotCount
		}
	}
	s.syncPersistedState(task.ID, slotKey, slotCount)
	return nil
}

// PeekPull reports whether pull operations are allowed right now, and the remaining quota in current slot.
// remaining < 0 means unlimited (no schedule rules).
func (s *SchedulerEngine) PeekPull(ctx context.Context, taskID uint) (allowed bool, remaining int, next time.Time) {
	if s == nil || taskID == 0 {
		return true, -1, time.Time{}
	}

	now := time.Now()

	var needPersistReset bool
	var persistKey string

	s.mu.Lock()

	st := s.tasks[taskID]
	if st == nil || len(st.rules) == 0 {
		s.mu.Unlock()
		return true, -1, time.Time{}
	}

	idx, end, limit, ok := pickActiveSlot(now, st.rules)
	if !ok {
		next, has := nextSlotStart(now, st.rules)
		if has {
			st.nextRun = next
			s.mu.Unlock()
			return false, 0, next
		}
		s.mu.Unlock()
		return true, -1, time.Time{}
	}

	if st.activeRuleIdx != idx || !st.slotEnd.Equal(end) {
		if st.reserved > 0 {
			next = st.slotEnd
			if next.IsZero() {
				next = end
			}
			st.nextRun = next
			s.mu.Unlock()
			return false, 0, next
		}

		newKey := buildSlotKey(now, st.rules[idx])
		st.activeRuleIdx = idx
		st.slotEnd = end
		st.limit = limit
		st.nextRun = time.Time{}

		if newKey != "" && newKey != st.currentSlotKey {
			st.currentSlotKey = newKey
			st.used = 0
			st.reserved = 0
			needPersistReset = true
			persistKey = newKey
		}
	}

	remaining = st.limit - st.used - st.reserved
	if remaining <= 0 {
		next = st.slotEnd
		st.nextRun = next
		s.mu.Unlock()
		if needPersistReset {
			_ = persistSlotReset(ctx, taskID, persistKey)
		}
		return false, 0, next
	}
	s.mu.Unlock()
	if needPersistReset {
		_ = persistSlotReset(ctx, taskID, persistKey)
	}
	return true, remaining, time.Time{}
}

func persistSlotReset(ctx context.Context, taskID uint, slotKey string) error {
	if taskID == 0 || global.DB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slotKey = strings.TrimSpace(slotKey)
	return global.DB.WithContext(ctx).Model(&model.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"current_slot_key":   slotKey,
			"current_slot_count": 0,
		}).Error
}

func (s *SchedulerEngine) CommitPull(ctx context.Context, taskID uint, usedDelta int) {
	if usedDelta <= 0 || s == nil || taskID == 0 {
		return
	}

	var slotKey string
	var used int

	s.mu.Lock()
	st := s.tasks[taskID]
	if st == nil || len(st.rules) == 0 || st.limit <= 0 {
		s.mu.Unlock()
		return
	}
	if st.currentSlotKey == "" {
		s.mu.Unlock()
		return
	}

	if st.reserved > 0 {
		st.reserved -= usedDelta
		if st.reserved < 0 {
			st.reserved = 0
		}
	}
	st.used += usedDelta
	if st.used < 0 {
		st.used = 0
	}
	slotKey = st.currentSlotKey
	used = st.used
	s.mu.Unlock()

	if global.DB != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := global.DB.WithContext(ctx).Model(&model.Task{}).Where("id = ?", taskID).
			Updates(map[string]any{
				"current_slot_key":   slotKey,
				"current_slot_count": used,
			}).Error; err != nil && global.Logger != nil {
			global.Logger.Warn("persist slot quota failed", zap.Uint("task_id", taskID), zap.Error(err))
		}
	}
}

func (s *SchedulerEngine) ReleasePull(taskID uint, reservedDelta int) {
	if reservedDelta <= 0 || s == nil || taskID == 0 {
		return
	}

	s.mu.Lock()
	st := s.tasks[taskID]
	if st == nil {
		s.mu.Unlock()
		return
	}
	if st.reserved > 0 {
		st.reserved -= reservedDelta
		if st.reserved < 0 {
			st.reserved = 0
		}
	}
	s.mu.Unlock()
}

func buildSlotKey(now time.Time, r parsedScheduleRule) string {
	loc := now.Location()
	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	if r.allDay {
		return startDay.Format("2006-01-02") + "|all-day"
	}

	minutes := now.Hour()*60 + now.Minute()
	if r.crossMidnight && minutes < r.endMin {
		startDay = startDay.AddDate(0, 0, -1)
	}

	return startDay.Format("2006-01-02") + "|" + formatHHMM(r.startMin) + "-" + formatHHMM(r.endMin)
}

func formatHHMM(minutes int) string {
	if minutes < 0 {
		minutes = 0
	}
	if minutes > 23*60+59 {
		minutes = 23*60 + 59
	}
	h := minutes / 60
	m := minutes % 60
	return fmt.Sprintf("%02d:%02d", h, m)
}
