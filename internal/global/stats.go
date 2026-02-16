package global

import (
	"context"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type StatsSnapshot struct {
	TS int64 `json:"ts"`

	CPUPercent float64 `json:"cpu_pct"`

	MemUsed  uint64 `json:"mem_used"`
	MemTotal uint64 `json:"mem_total"`

	DiskUsed  uint64 `json:"disk_used"`
	DiskTotal uint64 `json:"disk_total"`

	UploadBPS   float64 `json:"up_bps"`
	DownloadBPS float64 `json:"down_bps"`

	Pending  uint64 `json:"pending"`
	Success  uint64 `json:"success"`
	Fail     uint64 `json:"fail"`
	Filtered uint64 `json:"filtered"`

	FFmpegActive  int `json:"ffmpeg_active"`
	FFmpegThreads int `json:"ffmpeg_threads"`

	GPUDetected bool   `json:"gpu_detected"`
	GPUName     string `json:"gpu_name,omitempty"`
}

type LogEventLevel string

const (
	LogInfo  LogEventLevel = "info"
	LogWarn  LogEventLevel = "warn"
	LogError LogEventLevel = "error"
)

type LogEvent struct {
	ID      uint64        `json:"id"`
	TS      int64         `json:"ts"`
	Level   LogEventLevel `json:"level"`
	Message string        `json:"message"`
}

type logRing struct {
	mu    sync.Mutex
	buf   []LogEvent
	cap   int
	start int
	size  int
}

func newLogRing(capacity int) *logRing {
	if capacity <= 0 {
		capacity = 1
	}
	return &logRing{
		buf: make([]LogEvent, capacity),
		cap: capacity,
	}
}

func (r *logRing) push(ev LogEvent) {
	if r == nil || r.cap <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size < r.cap {
		r.buf[(r.start+r.size)%r.cap] = ev
		r.size++
		return
	}

	r.buf[r.start] = ev
	r.start = (r.start + 1) % r.cap
}

func (r *logRing) snapshot(afterID uint64, limit int) []LogEvent {
	if r == nil {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 {
		return nil
	}

	if afterID == 0 {
		start := r.size - limit
		if start < 0 {
			start = 0
		}
		out := make([]LogEvent, 0, r.size-start)
		for i := start; i < r.size; i++ {
			out = append(out, r.buf[(r.start+i)%r.cap])
		}
		return out
	}

	out := make([]LogEvent, 0, limit)
	for i := 0; i < r.size; i++ {
		ev := r.buf[(r.start+i)%r.cap]
		if ev.ID <= afterID {
			continue
		}
		out = append(out, ev)
		if len(out) >= limit {
			break
		}
	}
	return out
}

type logHub struct {
	mu   sync.RWMutex
	subs map[chan LogEvent]struct{}
}

func newLogHub() *logHub {
	return &logHub{subs: make(map[chan LogEvent]struct{})}
}

func (h *logHub) subscribe(buf int) chan LogEvent {
	if buf <= 0 {
		buf = 64
	}
	if buf > 512 {
		buf = 512
	}

	ch := make(chan LogEvent, buf)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *logHub) unsubscribe(ch chan LogEvent) {
	if ch == nil {
		return
	}
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *logHub) publish(ev LogEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// drop for slow subscribers
		}
	}
}

type AppStats struct {
	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}

	seq uint64

	cpuBits uint64 // float64 bits

	memUsed  uint64
	memTotal uint64

	diskUsed  uint64
	diskTotal uint64

	uploadTotal   uint64
	downloadTotal uint64

	uploadBpsBits   uint64 // float64 bits
	downloadBpsBits uint64 // float64 bits

	pending  uint64
	success  uint64
	fail     uint64
	filtered uint64

	ffmpegActive int64

	gpuDetected uint32
	gpuName     atomic.Value // string

	logs *logRing
	hub  *logHub
}

var Stats = newAppStats()
var LogChan = make(chan string, 100)

func newAppStats() *AppStats {
	s := &AppStats{
		stopCh: make(chan struct{}),
		logs:   newLogRing(600),
		hub:    newLogHub(),
	}
	s.gpuName.Store("")
	return s
}

func (s *AppStats) StartMonitor() {
	if s == nil {
		return
	}

	s.startOnce.Do(func() {
		go s.monitorLoop()
	})
}

func (s *AppStats) StopMonitor() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *AppStats) monitorLoop() {
	_, _ = cpu.Percent(0, false) // prime baseline for interval=0

	lastUp := atomic.LoadUint64(&s.uploadTotal)
	lastDown := atomic.LoadUint64(&s.downloadTotal)
	lastTS := time.Now()

	gpuTick := 0

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			elapsed := now.Sub(lastTS).Seconds()
			if elapsed <= 0 {
				elapsed = 1
			}

			curUp := atomic.LoadUint64(&s.uploadTotal)
			curDown := atomic.LoadUint64(&s.downloadTotal)

			var du uint64
			var dd uint64
			if curUp >= lastUp {
				du = curUp - lastUp
			}
			if curDown >= lastDown {
				dd = curDown - lastDown
			}

			atomic.StoreUint64(&s.uploadBpsBits, math.Float64bits(float64(du)/elapsed))
			atomic.StoreUint64(&s.downloadBpsBits, math.Float64bits(float64(dd)/elapsed))

			lastUp = curUp
			lastDown = curDown
			lastTS = now

			s.sampleSystem()

			gpuTick++
			if gpuTick >= 10 {
				gpuTick = 0
				s.sampleGPU(now)
			}
		case <-s.stopCh:
			return
		}
	}
}

func (s *AppStats) sampleSystem() {
	pcts, err := cpu.Percent(0, false)
	if err == nil && len(pcts) > 0 && pcts[0] >= 0 {
		atomic.StoreUint64(&s.cpuBits, math.Float64bits(pcts[0]))
	}

	vm, err := mem.VirtualMemory()
	if err == nil && vm != nil {
		atomic.StoreUint64(&s.memUsed, vm.Used)
		atomic.StoreUint64(&s.memTotal, vm.Total)
	}

	du, err := disk.Usage(diskRootPath())
	if err == nil && du != nil {
		atomic.StoreUint64(&s.diskUsed, du.Used)
		atomic.StoreUint64(&s.diskTotal, du.Total)
	}
}

func diskRootPath() string {
	if runtime.GOOS == "windows" {
		drive := strings.TrimSpace(os.Getenv("SystemDrive"))
		if drive == "" {
			drive = "C:"
		}
		return drive + `\`
	}
	return "/"
}

func (s *AppStats) sampleGPU(now time.Time) {
	_ = now
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		atomic.StoreUint32(&s.gpuDetected, 0)
		s.gpuName.Store("")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 850*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "--query-gpu=name", "--format=csv,noheader")
	out, err := cmd.Output()
	if err != nil {
		atomic.StoreUint32(&s.gpuDetected, 0)
		s.gpuName.Store("")
		return
	}

	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		name := strings.TrimSpace(ln)
		if name == "" {
			continue
		}
		atomic.StoreUint32(&s.gpuDetected, 1)
		s.gpuName.Store(name)
		return
	}

	atomic.StoreUint32(&s.gpuDetected, 1)
	s.gpuName.Store("NVIDIA GPU")
}

func (s *AppStats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{TS: time.Now().UnixMilli()}
	}

	cpuPct := math.Float64frombits(atomic.LoadUint64(&s.cpuBits))
	upBps := math.Float64frombits(atomic.LoadUint64(&s.uploadBpsBits))
	downBps := math.Float64frombits(atomic.LoadUint64(&s.downloadBpsBits))

	gpuName, _ := s.gpuName.Load().(string)

	return StatsSnapshot{
		TS:          time.Now().UnixMilli(),
		CPUPercent:  clampF(cpuPct, 0, 100),
		MemUsed:     atomic.LoadUint64(&s.memUsed),
		MemTotal:    atomic.LoadUint64(&s.memTotal),
		DiskUsed:    atomic.LoadUint64(&s.diskUsed),
		DiskTotal:   atomic.LoadUint64(&s.diskTotal),
		UploadBPS:   maxF(upBps, 0),
		DownloadBPS: maxF(downBps, 0),
		Pending:     atomic.LoadUint64(&s.pending),
		Success:     atomic.LoadUint64(&s.success),
		Fail:        atomic.LoadUint64(&s.fail),
		Filtered:    atomic.LoadUint64(&s.filtered),
		FFmpegActive: func() int {
			n := atomic.LoadInt64(&s.ffmpegActive)
			if n < 0 {
				return 0
			}
			if n > int64(^uint(0)>>1) {
				return int(^uint(0) >> 1)
			}
			return int(n)
		}(),
		FFmpegThreads: runtime.GOMAXPROCS(0),
		GPUDetected:   atomic.LoadUint32(&s.gpuDetected) == 1,
		GPUName:       strings.TrimSpace(gpuName),
	}
}

func (s *AppStats) AddUploadBytes(n uint64) {
	if s == nil || n == 0 {
		return
	}
	atomic.AddUint64(&s.uploadTotal, n)
}

func (s *AppStats) AddDownloadBytes(n uint64) {
	if s == nil || n == 0 {
		return
	}
	atomic.AddUint64(&s.downloadTotal, n)
}

func (s *AppStats) IncSuccess() {
	if s == nil {
		return
	}
	atomic.AddUint64(&s.success, 1)
}

func (s *AppStats) IncFail() {
	if s == nil {
		return
	}
	atomic.AddUint64(&s.fail, 1)
}

func (s *AppStats) IncFiltered() {
	if s == nil {
		return
	}
	atomic.AddUint64(&s.filtered, 1)
}

func (s *AppStats) AddPending(delta int64) {
	if s == nil || delta == 0 {
		return
	}
	for {
		cur := atomic.LoadUint64(&s.pending)
		var next uint64
		if delta > 0 {
			next = cur + uint64(delta)
		} else {
			dec := uint64(-delta)
			if dec >= cur {
				next = 0
			} else {
				next = cur - dec
			}
		}
		if atomic.CompareAndSwapUint64(&s.pending, cur, next) {
			return
		}
	}
}

func (s *AppStats) AddSuccess(n uint64) {
	if s == nil || n == 0 {
		return
	}
	atomic.AddUint64(&s.success, n)
}

func (s *AppStats) AddFail(n uint64) {
	if s == nil || n == 0 {
		return
	}
	atomic.AddUint64(&s.fail, n)
}

func (s *AppStats) AddFiltered(n uint64) {
	if s == nil || n == 0 {
		return
	}
	atomic.AddUint64(&s.filtered, n)
}

func (s *AppStats) IncFFmpegActive() {
	if s == nil {
		return
	}
	atomic.AddInt64(&s.ffmpegActive, 1)
}

func (s *AppStats) DecFFmpegActive() {
	if s == nil {
		return
	}
	atomic.AddInt64(&s.ffmpegActive, -1)
}

func (s *AppStats) BroadcastLog(msg string) {
	msg = strings.TrimSpace(msg)
	if s == nil || msg == "" {
		return
	}

	now := time.Now()
	ev := LogEvent{
		ID:      atomic.AddUint64(&s.seq, 1),
		TS:      now.UnixMilli(),
		Level:   inferLogLevel(msg),
		Message: msg,
	}
	s.logs.push(ev)
	s.hub.publish(ev)
	select {
	case LogChan <- msg:
	default:
	}
}

func (s *AppStats) SubscribeLogs(buf int) chan LogEvent {
	if s == nil {
		return nil
	}
	return s.hub.subscribe(buf)
}

func (s *AppStats) UnsubscribeLogs(ch chan LogEvent) {
	if s == nil {
		return
	}
	s.hub.unsubscribe(ch)
}

func (s *AppStats) SnapshotLogs(afterID uint64, limit int) []LogEvent {
	if s == nil {
		return nil
	}
	return s.logs.snapshot(afterID, limit)
}

func inferLogLevel(msg string) LogEventLevel {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return LogInfo
	}
	if strings.Contains(m, "[error]") || strings.Contains(m, "error") || strings.Contains(msg, "失败") || strings.Contains(msg, "异常") {
		return LogError
	}
	if strings.Contains(m, "[warn]") || strings.Contains(m, "warn") || strings.Contains(msg, "警告") || strings.Contains(msg, "暂停") {
		return LogWarn
	}
	return LogInfo
}

func clampF(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// Optional helpers for legacy code, aligned with prompt wording.
func StartMonitor() { Stats.StartMonitor() }
func AddUploadBytes(n uint64) {
	Stats.AddUploadBytes(n)
}
func AddDownloadBytes(n uint64) {
	Stats.AddDownloadBytes(n)
}
func IncSuccess()          { Stats.IncSuccess() }
func IncFail()             { Stats.IncFail() }
func IncFiltered()         { Stats.IncFiltered() }
func AddSuccess(n uint64)  { Stats.AddSuccess(n) }
func AddFail(n uint64)     { Stats.AddFail(n) }
func AddFiltered(n uint64) { Stats.AddFiltered(n) }
func BroadcastLog(msg string) {
	Stats.BroadcastLog(msg)
}
