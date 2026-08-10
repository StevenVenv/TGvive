package global

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"

	"my-go-server/pkg/procutil"
)

type StatsSnapshot struct {
	TS int64 `json:"ts"`

	OSInfo    string `json:"os_info,omitempty"`
	Kernel    string `json:"kernel,omitempty"`
	UptimeSec uint64 `json:"uptime_sec"`

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

	BizDay        string `json:"biz_day,omitempty"`
	TodaySuccess  uint64 `json:"today_success"`
	TodayFail     uint64 `json:"today_fail"`
	TodayFiltered uint64 `json:"today_filtered"`

	CommentPending uint64 `json:"comment_pending"`
	CommentSuccess uint64 `json:"comment_success"`
	CommentFail    uint64 `json:"comment_fail"`

	FFmpegActive  int `json:"ffmpeg_active"`
	FFmpegThreads int `json:"ffmpeg_threads"`

	HasGPU    bool   `json:"has_gpu"`
	GPUModel  string `json:"gpu_model,omitempty"`
	GPUMemory string `json:"gpu_memory,omitempty"`
	GPUDriver string `json:"gpu_driver,omitempty"`

	// Legacy aliases for earlier dashboard UI.
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

	osInfo atomic.Value // string
	kernel atomic.Value // string

	uptimeSec uint64

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

	commentPending uint64
	commentSuccess uint64
	commentFail    uint64

	ffmpegActive int64

	gpuModel  atomic.Value // string
	gpuMemory atomic.Value // string
	gpuDriver atomic.Value // string

	gpuDetected uint32
	gpuName     atomic.Value // string

	logs *logRing
	hub  *logHub

	bizDay        atomic.Value // string (YYYY-MM-DD)
	todaySuccess  uint64
	todayFail     uint64
	todayFiltered uint64
	bizMu         sync.Mutex

	persistLogCh chan LogEvent
}

var Stats = newAppStats()
var LogChan = make(chan string, 100)

func newAppStats() *AppStats {
	s := &AppStats{
		stopCh: make(chan struct{}),
		logs:   newLogRing(600),
		hub:    newLogHub(),
		// Buffer: avoid blocking hot paths if disk is slow.
		persistLogCh: make(chan LogEvent, 2048),
	}
	s.osInfo.Store(runtime.GOOS)
	s.kernel.Store("")
	s.gpuModel.Store("Integrated Graphics / No GPU")
	s.gpuMemory.Store("")
	s.gpuDriver.Store("")
	s.gpuName.Store("")
	s.bizDay.Store(time.Now().Format("2006-01-02"))
	return s
}

func (s *AppStats) StartMonitor() {
	if s == nil {
		return
	}

	s.startOnce.Do(func() {
		s.loadPersistedDashboardState()
		go s.persistDashboardStatsLoop()
		go s.persistDashboardLogsLoop()
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

	// Prime basic host/GPU info early to avoid an empty dashboard on first connect.
	s.sampleHost()
	s.sampleSystem()
	s.sampleGPU(time.Now())

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
			s.sampleHost()

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

func (s *AppStats) sampleHost() {
	info, err := host.Info()
	if err != nil || info == nil {
		return
	}

	osInfo := strings.TrimSpace(info.Platform)
	if v := strings.TrimSpace(info.PlatformVersion); v != "" && !strings.Contains(osInfo, v) {
		if osInfo == "" {
			osInfo = v
		} else {
			osInfo = osInfo + " " + v
		}
	}
	if osInfo == "" {
		osInfo = strings.TrimSpace(info.OS)
	}
	if osInfo == "" {
		osInfo = runtime.GOOS
	}
	s.osInfo.Store(osInfo)

	kernel := strings.TrimSpace(info.KernelVersion)
	s.kernel.Store(kernel)

	atomic.StoreUint64(&s.uptimeSec, info.Uptime)
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
		s.gpuModel.Store("Integrated Graphics / No GPU")
		s.gpuMemory.Store("")
		s.gpuDriver.Store("")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 850*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		path,
		"--query-gpu=name,memory.used,memory.total,driver_version",
		"--format=csv,noheader,nounits",
	)
	procutil.HideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		atomic.StoreUint32(&s.gpuDetected, 0)
		s.gpuName.Store("")
		s.gpuModel.Store("Integrated Graphics / No GPU")
		s.gpuMemory.Store("")
		s.gpuDriver.Store("")
		return
	}

	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}

		parts := strings.Split(ln, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}

		name := ""
		if len(parts) > 0 {
			name = parts[0]
		}
		if name == "" {
			name = "NVIDIA GPU"
		}

		memStr := ""
		driver := ""
		if len(parts) >= 4 {
			driver = parts[3]
			usedMiB, _ := strconv.ParseFloat(parts[1], 64)
			totalMiB, _ := strconv.ParseFloat(parts[2], 64)
			if totalMiB > 0 {
				totalGB := int(math.Round(totalMiB / 1024.0))
				if usedMiB > 0 {
					usedGB := int(math.Round(usedMiB / 1024.0))
					memStr = fmt.Sprintf("%dGB / %dGB", usedGB, totalGB)
				} else {
					memStr = fmt.Sprintf("%dGB", totalGB)
				}
			}
		}

		atomic.StoreUint32(&s.gpuDetected, 1)
		s.gpuName.Store(name)
		s.gpuModel.Store(name)
		s.gpuMemory.Store(memStr)
		s.gpuDriver.Store(strings.TrimSpace(driver))
		return
	}

	atomic.StoreUint32(&s.gpuDetected, 0)
	s.gpuName.Store("")
	s.gpuModel.Store("Integrated Graphics / No GPU")
	s.gpuMemory.Store("")
	s.gpuDriver.Store("")
}

func (s *AppStats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{TS: time.Now().UnixMilli()}
	}

	osInfo, _ := s.osInfo.Load().(string)
	kernel, _ := s.kernel.Load().(string)

	cpuPct := math.Float64frombits(atomic.LoadUint64(&s.cpuBits))
	upBps := math.Float64frombits(atomic.LoadUint64(&s.uploadBpsBits))
	downBps := math.Float64frombits(atomic.LoadUint64(&s.downloadBpsBits))

	gpuModel, _ := s.gpuModel.Load().(string)
	gpuMemory, _ := s.gpuMemory.Load().(string)
	gpuDriver, _ := s.gpuDriver.Load().(string)
	gpuName, _ := s.gpuName.Load().(string)
	hasGPU := atomic.LoadUint32(&s.gpuDetected) == 1

	bizDay := s.ensureBizDay(time.Now())

	return StatsSnapshot{
		TS:          time.Now().UnixMilli(),
		OSInfo:      strings.TrimSpace(osInfo),
		Kernel:      strings.TrimSpace(kernel),
		UptimeSec:   atomic.LoadUint64(&s.uptimeSec),
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
		BizDay:      bizDay,
		TodaySuccess: func() uint64 {
			return atomic.LoadUint64(&s.todaySuccess)
		}(),
		TodayFail: func() uint64 {
			return atomic.LoadUint64(&s.todayFail)
		}(),
		TodayFiltered: func() uint64 {
			return atomic.LoadUint64(&s.todayFiltered)
		}(),
		CommentPending: atomic.LoadUint64(&s.commentPending),
		CommentSuccess: atomic.LoadUint64(&s.commentSuccess),
		CommentFail:    atomic.LoadUint64(&s.commentFail),
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
		HasGPU:        hasGPU,
		GPUModel:      strings.TrimSpace(gpuModel),
		GPUMemory:     strings.TrimSpace(gpuMemory),
		GPUDriver:     strings.TrimSpace(gpuDriver),
		GPUDetected:   hasGPU,
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
	s.ensureBizDay(time.Now())
	atomic.AddUint64(&s.success, 1)
	atomic.AddUint64(&s.todaySuccess, 1)
}

func (s *AppStats) IncFail() {
	if s == nil {
		return
	}
	s.ensureBizDay(time.Now())
	atomic.AddUint64(&s.fail, 1)
	atomic.AddUint64(&s.todayFail, 1)
}

func (s *AppStats) IncFiltered() {
	if s == nil {
		return
	}
	s.ensureBizDay(time.Now())
	atomic.AddUint64(&s.filtered, 1)
	atomic.AddUint64(&s.todayFiltered, 1)
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
	s.ensureBizDay(time.Now())
	atomic.AddUint64(&s.success, n)
	atomic.AddUint64(&s.todaySuccess, n)
}

func (s *AppStats) AddFail(n uint64) {
	if s == nil || n == 0 {
		return
	}
	s.ensureBizDay(time.Now())
	atomic.AddUint64(&s.fail, n)
	atomic.AddUint64(&s.todayFail, n)
}

func (s *AppStats) AddFiltered(n uint64) {
	if s == nil || n == 0 {
		return
	}
	s.ensureBizDay(time.Now())
	atomic.AddUint64(&s.filtered, n)
	atomic.AddUint64(&s.todayFiltered, n)
}

func (s *AppStats) SetCommentQueueStats(pending, success, fail uint64) {
	if s == nil {
		return
	}
	atomic.StoreUint64(&s.commentPending, pending)
	atomic.StoreUint64(&s.commentSuccess, success)
	atomic.StoreUint64(&s.commentFail, fail)
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
	// Persist best-effort (never block business logic).
	if s.persistLogCh != nil {
		select {
		case s.persistLogCh <- ev:
		default:
		}
	}
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
