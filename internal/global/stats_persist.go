package global

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	dashboardPersistVersion = 1

	dashboardPersistDir     = "./data/dashboard"
	dashboardPersistLogsDir = "./data/dashboard/logs"
	dashboardPersistStats   = "./data/dashboard/stats.json"

	dashboardSeedMaxLogs = 600
	dashboardKeepLogDays = 7
)

type persistedDashboardStats struct {
	Version   int   `json:"version"`
	UpdatedAt int64 `json:"updated_at"`

	Seq      uint64 `json:"seq"`
	Success  uint64 `json:"success"`
	Fail     uint64 `json:"fail"`
	Filtered uint64 `json:"filtered"`

	BizDay        string `json:"biz_day,omitempty"`
	TodaySuccess  uint64 `json:"today_success"`
	TodayFail     uint64 `json:"today_fail"`
	TodayFiltered uint64 `json:"today_filtered"`
}

func bizDayString(t time.Time) string {
	return t.Local().Format("2006-01-02")
}

func (s *AppStats) ensureBizDay(now time.Time) string {
	day := bizDayString(now)

	cur, _ := s.bizDay.Load().(string)
	if cur == day && cur != "" {
		return day
	}

	s.bizMu.Lock()
	defer s.bizMu.Unlock()

	cur, _ = s.bizDay.Load().(string)
	if cur == day && cur != "" {
		return day
	}

	s.bizDay.Store(day)
	atomicStoreUint64(&s.todaySuccess, 0)
	atomicStoreUint64(&s.todayFail, 0)
	atomicStoreUint64(&s.todayFiltered, 0)
	return day
}

func atomicStoreUint64(ptr *uint64, v uint64) {
	// Wrapper for consistent usage across files.
	// (kept here to avoid importing sync/atomic in multiple files).
	atomic.StoreUint64(ptr, v)
}

func atomicLoadUint64(ptr *uint64) uint64 {
	return atomic.LoadUint64(ptr)
}

func (s *AppStats) loadPersistedDashboardState() {
	if s == nil {
		return
	}

	_ = os.MkdirAll(dashboardPersistDir, 0o755)
	_ = os.MkdirAll(dashboardPersistLogsDir, 0o755)

	// Load counters (best-effort).
	if b, err := os.ReadFile(dashboardPersistStats); err == nil && len(b) > 0 {
		var p persistedDashboardStats
		if jerr := json.Unmarshal(b, &p); jerr == nil && p.Version == dashboardPersistVersion {
			atomicStoreUint64(&s.seq, p.Seq)
			atomicStoreUint64(&s.success, p.Success)
			atomicStoreUint64(&s.fail, p.Fail)
			atomicStoreUint64(&s.filtered, p.Filtered)

			if strings.TrimSpace(p.BizDay) != "" {
				s.bizDay.Store(strings.TrimSpace(p.BizDay))
			}
			atomicStoreUint64(&s.todaySuccess, p.TodaySuccess)
			atomicStoreUint64(&s.todayFail, p.TodayFail)
			atomicStoreUint64(&s.todayFiltered, p.TodayFiltered)
		}
	}

	// Seed recent logs from disk and advance seq to avoid ID regression.
	maxID := s.seedDashboardLogsFromDisk(time.Now(), dashboardSeedMaxLogs)
	if maxID > atomicLoadUint64(&s.seq) {
		atomicStoreUint64(&s.seq, maxID)
	}

	// Ensure day rotation after loading.
	s.ensureBizDay(time.Now())
}

func (s *AppStats) seedDashboardLogsFromDisk(now time.Time, max int) uint64 {
	if s == nil || s.logs == nil {
		return 0
	}
	if max <= 0 {
		max = dashboardSeedMaxLogs
	}
	if max > 5000 {
		max = 5000
	}

	days := []string{
		bizDayString(now.AddDate(0, 0, -1)),
		bizDayString(now),
	}

	events := make([]LogEvent, 0, max)
	var maxID uint64

	for _, day := range days {
		path := filepath.Join(dashboardPersistLogsDir, day+".jsonl")
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		sc := bufio.NewScanner(f)
		// Allow long log lines.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var ev LogEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			events = append(events, ev)
			if ev.ID > maxID {
				maxID = ev.ID
			}

			// Keep memory bounded while scanning large files.
			if len(events) > max*4 {
				events = append([]LogEvent(nil), events[len(events)-max:]...)
			}
		}
		_ = f.Close()
	}

	if len(events) > max {
		events = events[len(events)-max:]
	}

	// Ensure increasing order (safety for out-of-order writes).
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].TS == events[j].TS {
			return events[i].ID < events[j].ID
		}
		return events[i].TS < events[j].TS
	})

	for _, ev := range events {
		s.logs.push(ev)
	}

	return maxID
}

func (s *AppStats) persistDashboardStatsLoop() {
	if s == nil {
		return
	}

	_ = os.MkdirAll(dashboardPersistDir, 0o755)

	t := time.NewTicker(2 * time.Second)
	defer t.Stop()

	var last persistedDashboardStats
	last.Version = -1

	flush := func() {
		cur := persistedDashboardStats{
			Version:   dashboardPersistVersion,
			UpdatedAt: time.Now().UnixMilli(),
			Seq:       atomicLoadUint64(&s.seq),
			Success:   atomicLoadUint64(&s.success),
			Fail:      atomicLoadUint64(&s.fail),
			Filtered:  atomicLoadUint64(&s.filtered),
			BizDay:    s.ensureBizDay(time.Now()),
			TodaySuccess: func() uint64 {
				return atomicLoadUint64(&s.todaySuccess)
			}(),
			TodayFail: func() uint64 {
				return atomicLoadUint64(&s.todayFail)
			}(),
			TodayFiltered: func() uint64 {
				return atomicLoadUint64(&s.todayFiltered)
			}(),
		}

		if last.Version == cur.Version &&
			last.Seq == cur.Seq &&
			last.Success == cur.Success &&
			last.Fail == cur.Fail &&
			last.Filtered == cur.Filtered &&
			last.BizDay == cur.BizDay &&
			last.TodaySuccess == cur.TodaySuccess &&
			last.TodayFail == cur.TodayFail &&
			last.TodayFiltered == cur.TodayFiltered {
			return
		}

		b, err := json.MarshalIndent(cur, "", "  ")
		if err != nil {
			return
		}
		_ = writeFileAtomic(dashboardPersistStats, b, 0o644)
		last = cur
	}

	flush()

	for {
		select {
		case <-s.stopCh:
			flush()
			return
		case <-t.C:
			flush()
		}
	}
}

func (s *AppStats) persistDashboardLogsLoop() {
	if s == nil || s.persistLogCh == nil {
		return
	}

	_ = os.MkdirAll(dashboardPersistLogsDir, 0o755)

	var (
		curDay string
		f      *os.File
		w      *bufio.Writer
	)

	closeFile := func() {
		if w != nil {
			_ = w.Flush()
		}
		if f != nil {
			_ = f.Close()
		}
		w = nil
		f = nil
	}

	openForDay := func(day string) error {
		day = strings.TrimSpace(day)
		if day == "" {
			return errors.New("day is empty")
		}

		path := filepath.Join(dashboardPersistLogsDir, day+".jsonl")
		ff, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		f = ff
		w = bufio.NewWriterSize(f, 64*1024)
		curDay = day
		return nil
	}

	if err := openForDay(bizDayString(time.Now())); err != nil {
		return
	}
	cleanupDashboardLogRetention(time.Now())

	flushTick := time.NewTicker(1 * time.Second)
	defer flushTick.Stop()

	flush := func() {
		if w != nil {
			_ = w.Flush()
		}
	}

	for {
		select {
		case <-s.stopCh:
			flush()
			closeFile()
			return
		case <-flushTick.C:
			flush()
		case ev := <-s.persistLogCh:
			day := bizDayString(time.UnixMilli(ev.TS))
			if day == "" {
				day = bizDayString(time.Now())
			}

			if day != curDay {
				flush()
				closeFile()
				if err := openForDay(day); err != nil {
					// If rotate fails, keep dropping until next successful open.
					continue
				}
				cleanupDashboardLogRetention(time.Now())
			}

			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if w == nil {
				continue
			}
			_, _ = w.Write(b)
			_ = w.WriteByte('\n')
		}
	}
}

func cleanupDashboardLogRetention(now time.Time) {
	entries, err := os.ReadDir(dashboardPersistLogsDir)
	if err != nil {
		return
	}

	cutoff := bizDayString(now.AddDate(0, 0, -dashboardKeepLogDays))
	if cutoff == "" {
		return
	}

	for _, ent := range entries {
		if ent == nil || ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(name, ".jsonl")
		if len(day) != len("2006-01-02") {
			continue
		}
		// Lexicographic compare works for YYYY-MM-DD.
		if day < cutoff {
			_ = os.Remove(filepath.Join(dashboardPersistLogsDir, name))
		}
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}

	return os.Rename(tmp, path)
}
