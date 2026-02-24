<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { apiFetch } from '../api'

type LogLevel = 'INFO' | 'WARN' | 'ERROR'
type LogItem = { id: number; level: LogLevel; text: string; ts: number }

type StatsSnapshot = {
  ts: number
  os_info?: string
  kernel?: string
  uptime_sec?: number
  cpu_pct: number
  mem_used: number
  mem_total: number
  disk_used: number
  disk_total: number
  up_bps: number
  down_bps: number
  pending: number
  success: number
  fail: number
  filtered: number
  biz_day?: string
  today_success?: number
  today_fail?: number
  today_filtered?: number
  comment_pending?: number
  comment_success?: number
  comment_fail?: number
  ffmpeg_active: number
  ffmpeg_threads: number
  has_gpu?: boolean
  gpu_model?: string
  gpu_memory?: string
  gpu_driver?: string
  gpu_detected: boolean
  gpu_name?: string
}

type LogEvent = {
  id: number
  ts: number
  level: 'info' | 'warn' | 'error'
  message: string
}

type WSMessage =
  | { type: 'stats'; data: StatsSnapshot }
  | { type: 'log'; data: LogEvent }

function clamp(n: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, n))
}

function fmtGiB(v: number): string {
  return `${v.toFixed(1)} GB`
}

function fmtSpeedBps(bps: number): string {
  const kb = bps / 1024
  if (kb < 1024) return `${kb.toFixed(kb < 10 ? 1 : 0)} KB/s`
  const mb = kb / 1024
  return `${mb.toFixed(mb < 10 ? 2 : 1)} MB/s`
}

function toGiB(bytes: number): number {
  if (!Number.isFinite(bytes) || bytes <= 0) return 0
  return bytes / 1024 / 1024 / 1024
}

function fmtTime(ts: number): string {
  const d = new Date(ts || Date.now())
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${hh}:${mm}:${ss}`
}

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - (el.scrollTop + el.clientHeight) < 64
}

const sys = reactive({
  osInfo: '',
  kernel: '',
  uptimeSec: 0,
  cpu: 0,
  memUsed: 0,
  memTotal: 0,
  diskUsed: 0,
  diskTotal: 0,
  hasGPU: false,
  gpuModel: '',
  gpuMemory: '',
  gpuDriver: '',
  upBps: 0,
  downBps: 0,
})

const biz = reactive({
  pending: 0,
  forwarded: 0,
  filtered: 0,
  errors: 0,
  bizDay: '',
  todayForwarded: 0,
  todayFiltered: 0,
  todayErrors: 0,
})

const commentQ = reactive({
  pending: 0,
  success: 0,
  fail: 0,
})

const service = reactive({
  ffmpegQueue: 0,
  ffmpegThreads: 0,
  proxyEndpoint: '',
  proxyLatency: 0,
  proxyEnabled: false,
})

const cpuPct = computed(() => Math.round(sys.cpu))
const memPct = computed(() => clamp(Math.round((sys.memUsed / Math.max(0.1, sys.memTotal)) * 100), 0, 100))
const diskPct = computed(() => clamp(Math.round((sys.diskUsed / Math.max(0.1, sys.diskTotal)) * 100), 0, 100))

const totalProcessed = computed(() => Math.max(0, biz.forwarded + biz.filtered + biz.errors))
const todayProcessed = computed(() => Math.max(0, biz.todayForwarded + biz.todayFiltered + biz.todayErrors))
const commentProcessed = computed(() => Math.max(0, commentQ.success + commentQ.fail))
const commentTotal = computed(() => Math.max(0, commentQ.pending + commentQ.success + commentQ.fail))
const overallProcessed = computed(() => Math.max(0, totalProcessed.value + commentProcessed.value))

const totalFilterRate = computed(() => Math.round((biz.filtered / Math.max(1, totalProcessed.value)) * 100))
const totalErrorRate = computed(() => Math.round((biz.errors / Math.max(1, totalProcessed.value)) * 100))
const todayFilterRate = computed(() => Math.round((biz.todayFiltered / Math.max(1, todayProcessed.value)) * 100))
const todayErrorRate = computed(() => Math.round((biz.todayErrors / Math.max(1, todayProcessed.value)) * 100))

function fmtUptime(sec: number): string {
  const s = Math.max(0, Math.floor(Number(sec || 0)))
  if (!s) return '--'
  const days = Math.floor(s / 86400)
  const hours = Math.floor((s % 86400) / 3600)
  const mins = Math.floor((s % 3600) / 60)
  if (days > 0) return `${days}天${hours}小时`
  if (hours > 0) return `${hours}小时${mins}分钟`
  return `${Math.max(1, mins)}分钟`
}

const osIcon = computed(() => {
  const v = String(sys.osInfo || '').toLowerCase()
  if (v.includes('ubuntu')) return 'ri-ubuntu-line'
  if (v.includes('windows')) return 'ri-windows-fill'
  if (v.includes('mac') || v.includes('macos') || v.includes('darwin') || v.includes('os x')) return 'ri-apple-fill'
  return 'ri-computer-line'
})

const osTone = computed(() => {
  const v = String(sys.osInfo || '').toLowerCase()
  if (v.includes('ubuntu')) return 'ubuntu'
  if (v.includes('windows')) return 'windows'
  if (v.includes('mac') || v.includes('macos') || v.includes('darwin') || v.includes('os x')) return 'apple'
  return 'generic'
})

const uptimeText = computed(() => fmtUptime(sys.uptimeSec))

const proxyText = computed(() => {
  if (!service.proxyEnabled || !service.proxyEndpoint) return '直连模式'
  if (!service.proxyLatency || service.proxyLatency <= 0) return `${service.proxyEndpoint} (--ms)`
  return `${service.proxyEndpoint} (${service.proxyLatency}ms)`
})

const lastStatsTs = ref(0)
const lastStatsText = computed(() => (lastStatsTs.value > 0 ? fmtTime(lastStatsTs.value) : '--'))

const wsConnected = ref(false)
const lastWSErr = ref('')

const modeBadge = computed(() => (wsConnected.value ? 'LIVE' : 'OFFLINE'))
const modeClass = computed(() => (wsConnected.value ? 'live' : 'offline'))

const logs = ref<LogItem[]>([])
const logBoxRef = ref<HTMLElement | null>(null)
let localLogId = -1
let lastServerLogID = 0

const logQuery = ref('')
const logLevels = ref<LogLevel[]>(['INFO', 'WARN', 'ERROR'])
const stickToBottom = ref(true)

type LogPreset = 'all' | 'filter_error' | 'errors'
const logPreset = ref<LogPreset>('all')
const logPresetLabel = computed(() => {
  if (logPreset.value === 'filter_error') return '过滤 + 错误'
  if (logPreset.value === 'errors') return '仅错误'
  return ''
})

const filteredLogs = computed(() => {
  const q = (logQuery.value || '').trim().toLowerCase()
  const allowed = new Set(logLevels.value)
  return (logs.value || []).filter((it) => {
    if (!allowed.has(it.level)) return false
    const lower = it.text.toLowerCase()
    if (logPreset.value === 'errors') {
      if (it.level !== 'ERROR') return false
    } else if (logPreset.value === 'filter_error') {
      const isErr = it.level === 'ERROR'
      const isFilter =
        lower.includes('[filter]') ||
        lower.includes('filter') ||
        lower.includes('filtered') ||
        lower.includes('skip') ||
        lower.includes('blocked') ||
        lower.includes('过滤') ||
        lower.includes('屏蔽') ||
        lower.includes('忽略') ||
        lower.includes('白名单') ||
        lower.includes('绑定') ||
        lower.includes('trusted')
      if (!isErr && !isFilter) return false
    }
    if (!q) return true
    return lower.includes(q)
  })
})

function pushLog(level: LogLevel, text: string, id?: number, ts?: number) {
  const el = logBoxRef.value
  const shouldStick = stickToBottom.value && (!el || isNearBottom(el))
  const entry: LogItem = {
    id: typeof id === 'number' ? id : localLogId--,
    level,
    text,
    ts: typeof ts === 'number' ? ts : Date.now(),
  }
  logs.value = [...logs.value, entry].slice(-200)
  void nextTick(() => {
    if (!shouldStick) return
    const el = logBoxRef.value
    if (!el) return
    el.scrollTop = el.scrollHeight
  })
}

function clearLogs() {
  logs.value = []
}

function setLogPreset(p: LogPreset) {
  logPreset.value = p
  logQuery.value = ''
  if (p === 'errors') {
    logLevels.value = ['ERROR']
    return
  }
  logLevels.value = ['INFO', 'WARN', 'ERROR']
}

async function copyVisibleLogs() {
  const text = filteredLogs.value.map((x) => x.text).join('\n').trim()
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败（浏览器权限限制）')
  }
}

let lastProxyFetchAt = 0
async function loadProxyConfig(force = false) {
  const now = Date.now()
  if (!force && now - lastProxyFetchAt < 5000) return
  lastProxyFetchAt = now

  try {
    const cfg = (await apiFetch<any>('/api/v1/settings/proxy', { method: 'GET' })) || {}
    const enabled = !!cfg?.enabled
    const host = String(cfg?.host || '').trim()
    const port = Number(cfg?.port || 0)
    if (!enabled || !host || !port) {
      service.proxyEnabled = false
      service.proxyEndpoint = ''
      return
    }
    service.proxyEnabled = true
    service.proxyEndpoint = `${host}:${port}`
  } catch {
    // ignore (e.g. not logged in yet)
  }
}

function applyStats(d: StatsSnapshot) {
  if (!d) return
  const snapTS = Number(d.ts || 0)
  if (Number.isFinite(snapTS) && snapTS > 0) lastStatsTs.value = snapTS

  sys.osInfo = String(d.os_info || '')
  sys.kernel = String(d.kernel || '')
  sys.uptimeSec = Math.max(0, Number(d.uptime_sec || 0))
  sys.cpu = clamp(Number(d.cpu_pct || 0), 0, 100)
  sys.memUsed = toGiB(Number(d.mem_used || 0))
  sys.memTotal = Math.max(0.1, toGiB(Number(d.mem_total || 0)) || 0.1)
  sys.diskUsed = toGiB(Number(d.disk_used || 0))
  sys.diskTotal = Math.max(0.1, toGiB(Number(d.disk_total || 0)) || 0.1)
  sys.upBps = Math.max(0, Number(d.up_bps || 0))
  sys.downBps = Math.max(0, Number(d.down_bps || 0))
  sys.hasGPU = !!(d.has_gpu ?? d.gpu_detected)
  sys.gpuModel = String(d.gpu_model || d.gpu_name || '')
  sys.gpuMemory = String(d.gpu_memory || '')
  sys.gpuDriver = String(d.gpu_driver || '')

  biz.pending = Math.max(0, Number(d.pending || 0))
  biz.forwarded = Math.max(0, Number(d.success || 0))
  biz.filtered = Math.max(0, Number(d.filtered || 0))
  biz.errors = Math.max(0, Number(d.fail || 0))
  biz.bizDay = String(d.biz_day || '')
  biz.todayForwarded = Math.max(0, Number(d.today_success || 0))
  biz.todayFiltered = Math.max(0, Number(d.today_filtered || 0))
  biz.todayErrors = Math.max(0, Number(d.today_fail || 0))

  commentQ.pending = Math.max(0, Number(d.comment_pending || 0))
  commentQ.success = Math.max(0, Number(d.comment_success || 0))
  commentQ.fail = Math.max(0, Number(d.comment_fail || 0))

  service.ffmpegQueue = Math.max(0, Number(d.ffmpeg_active || 0))
  service.ffmpegThreads = Math.max(0, Number(d.ffmpeg_threads || 0))

  void loadProxyConfig()
}

function applyLog(ev: LogEvent) {
  if (!ev || typeof ev.message !== 'string') return
  lastServerLogID = Math.max(lastServerLogID, Number(ev.id || 0) || 0)
  const level: LogLevel = ev.level === 'error' ? 'ERROR' : ev.level === 'warn' ? 'WARN' : 'INFO'
  const text = `[${level}] ${fmtTime(ev.ts)} ${ev.message.trim()}`
  pushLog(level, text, Number(ev.id || 0) || undefined, ev.ts || undefined)
}

function wsURL(): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}/api/v1/ws/dashboard`
}

let ws: WebSocket | null = null
let reconnectTimer: number | undefined
let pollTimer: number | undefined
let pollInFlight = false
const logsAnchorRef = ref<HTMLElement | null>(null)

function cleanupWS() {
  if (reconnectTimer) {
    window.clearTimeout(reconnectTimer)
    reconnectTimer = undefined
  }
  if (ws) {
    try {
      ws.onopen = null
      ws.onclose = null
      ws.onerror = null
      ws.onmessage = null
      ws.close()
    } catch {
      // ignore
    }
    ws = null
  }
}

function stopPolling() {
  if (pollTimer) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
}

async function pollOnce() {
  if (pollInFlight) return
  pollInFlight = true
  try {
    try {
      const snap = await apiFetch<StatsSnapshot>('/api/v1/dashboard/summary', { method: 'GET' })
      applyStats(snap)
    } catch {
      // best-effort
    }

    try {
      const evs = await apiFetch<LogEvent[]>(
        `/api/v1/dashboard/events?after_id=${encodeURIComponent(String(lastServerLogID || 0))}&limit=50`,
        { method: 'GET' },
      )
      for (const ev of evs || []) {
        applyLog(ev)
      }
    } catch {
      // best-effort
    }
  } finally {
    pollInFlight = false
  }
}

function startPolling() {
  stopPolling()
  void pollOnce()
  pollTimer = window.setInterval(() => {
    if (wsConnected.value) return
    void pollOnce()
  }, 2000)
}

function scheduleReconnect() {
  if (reconnectTimer) return
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = undefined
    connectWS()
  }, 3000)
}

function connectWS() {
  if (ws) cleanupWS()

  wsConnected.value = false
  lastWSErr.value = ''

  let sock: WebSocket
  try {
    sock = new WebSocket(wsURL())
  } catch (err: any) {
    lastWSErr.value = String(err?.message || 'WebSocket init failed')
    scheduleReconnect()
    return
  }

  ws = sock
  sock.onopen = () => {
    wsConnected.value = true
    lastWSErr.value = ''
    logs.value = []
    stopPolling()
    pushLog('INFO', `[INFO] ${fmtTime(Date.now())} WS connected`)
  }

  sock.onmessage = (e) => {
    try {
      const msg = JSON.parse(String(e.data || '{}')) as WSMessage
      if (msg.type === 'stats') {
        applyStats(msg.data)
        return
      }
      if (msg.type === 'log') {
        applyLog(msg.data)
        return
      }
    } catch {
      // ignore
    }
  }

  sock.onerror = () => {
    lastWSErr.value = 'WebSocket error'
  }

  sock.onclose = () => {
    wsConnected.value = false
    if (!lastWSErr.value) lastWSErr.value = 'WebSocket closed'
    pushLog('WARN', `[WARN] ${fmtTime(Date.now())} WS disconnected`)
    startPolling()
    scheduleReconnect()
  }
}

function refresh() {
  connectWS()
}

function focusLogs(preset?: LogPreset) {
  if (preset) setLogPreset(preset)
  void nextTick(() => logsAnchorRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
}

defineExpose({ refresh })

onMounted(() => {
  void loadProxyConfig(true)
  connectWS()
  startPolling()
})

onBeforeUnmount(() => {
  cleanupWS()
  stopPolling()
})
</script>

<template>
  <div class="dashboard">
    <div class="dash-head">
      <div class="dash-title">
        <i class="ri-dashboard-3-line" />
        <div class="dash-text">
          <div class="dash-main">仪表盘</div>
          <div class="dash-sub muted">
            <span>实时监控</span>
            <span class="dot">·</span>
            <span>{{ wsConnected ? 'WebSocket' : 'Polling' }}</span>
            <span class="dot">·</span>
            <span>更新 {{ lastStatsText }}</span>
          </div>
        </div>
      </div>

      <el-space size="small" wrap>
        <span class="pill" :class="modeClass">{{ modeBadge }}</span>
        <el-button size="small" @click="refresh">
          <i class="ri-refresh-line" />
          刷新
        </el-button>
      </el-space>
    </div>

    <el-row :gutter="12" class="dash-row">
      <el-col :xs="24" :md="12" :lg="8">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-dashboard-3-line" />
                <span>实时资源监控</span>
              </div>
              <div class="card-sub">
                <span>System Status</span>
                <span v-if="!wsConnected && lastWSErr" class="sys-err" :title="lastWSErr">WS_OFFLINE</span>
              </div>
            </div>
          </template>

          <div class="dash-stack">
            <div class="metric dash-box">
              <div class="metric-top">
                <span class="label">CPU 使用率</span>
                <span class="value">{{ cpuPct }}%</span>
              </div>
              <el-progress :percentage="cpuPct" :stroke-width="10" :show-text="false" />
            </div>

            <div class="metric dash-box">
              <div class="metric-top">
                <span class="label">内存使用</span>
                <span class="value">{{ fmtGiB(sys.memUsed) }} / {{ fmtGiB(sys.memTotal) }}</span>
              </div>
              <el-progress :percentage="memPct" :stroke-width="10" :show-text="false" status="success" />
            </div>

            <div class="metric dash-box">
              <div class="metric-top">
                <span class="label">磁盘存储</span>
                <span class="value">{{ fmtGiB(sys.diskUsed) }} / {{ fmtGiB(sys.diskTotal) }}</span>
              </div>
              <el-progress :percentage="diskPct" :stroke-width="10" :show-text="false" status="warning" />
            </div>

            <div class="split-line" />

            <div class="overview-grid">
              <div class="ov-card dash-box">
                <div class="ov-icon">
                  <i :class="[osIcon, 'os-icon', osTone]" />
                </div>
                <div class="ov-meta">
                  <div class="ov-main">{{ sys.osInfo || 'Unknown OS' }}</div>
                  <div class="ov-sub muted">
                    <span v-if="sys.kernel">Kernel {{ sys.kernel }}</span>
                    <span v-if="sys.kernel && sys.uptimeSec" class="dot">·</span>
                    <span v-if="sys.uptimeSec">已运行 {{ uptimeText }}</span>
                  </div>
                </div>
              </div>

              <div class="ov-card dash-box">
                <div class="ov-icon">
                  <i class="ri-cpu-line" :class="sys.hasGPU ? 'gpu-ok' : 'gpu-off'" />
                </div>
                <div class="ov-meta">
                  <div class="ov-main" :class="{ muted: !sys.hasGPU }">
                    {{ sys.gpuModel || 'Integrated Graphics / No GPU' }}
                  </div>
                  <div class="ov-tags">
                    <el-tag v-if="sys.gpuMemory" size="small" effect="dark" class="ov-tag">显存: {{ sys.gpuMemory }}</el-tag>
                    <el-tag v-if="sys.gpuDriver" size="small" effect="dark" class="ov-tag">驱动: v{{ sys.gpuDriver }}</el-tag>
                    <el-tag v-if="!sys.hasGPU" size="small" effect="dark" type="info" class="ov-tag">NO GPU</el-tag>
                  </div>
                </div>
              </div>
            </div>

            <div class="net-row">
              <div class="net-item up dash-box">
                <i class="ri-upload-2-line" />
                <div class="net-meta">
                  <div class="net-label">上传</div>
                  <div class="net-value">{{ fmtSpeedBps(sys.upBps) }}</div>
                </div>
              </div>
              <div class="net-item down dash-box">
                <i class="ri-download-2-line" />
                <div class="net-meta">
                  <div class="net-label">下载</div>
                  <div class="net-value">{{ fmtSpeedBps(sys.downBps) }}</div>
                </div>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col :xs="24" :md="12" :lg="10">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-bar-chart-2-line" />
                <span>业务统计数据</span>
              </div>
              <div class="card-sub">
                <span>Business Stats</span>
                <span v-if="biz.bizDay" class="dot">·</span>
                <span v-if="biz.bizDay">{{ biz.bizDay }}</span>
              </div>
            </div>
          </template>

          <div class="dash-stack">
            <el-row :gutter="10" class="stat-row">
              <el-col :xs="12" :sm="12" :md="12" :lg="12" :xl="6">
                <div class="stat-card dash-box">
                  <div class="stat-icon pending"><i class="ri-time-line" /></div>
                  <div class="stat-meta">
                    <div class="stat-value">{{ commentQ.pending }}</div>
                    <div class="stat-label muted">评论待转发</div>
                    <div class="stat-sub muted">实时队列 {{ biz.pending }}</div>
                  </div>
                </div>
              </el-col>

              <el-col :xs="12" :sm="12" :md="12" :lg="12" :xl="6">
                <div class="stat-card dash-box">
                  <div class="stat-icon ok"><i class="ri-line-chart-line" /></div>
                  <div class="stat-meta">
                    <div class="stat-value">{{ commentQ.success }}</div>
                    <div class="stat-label muted">评论已转发</div>
                    <div class="stat-sub muted">消息累计 {{ biz.forwarded }} · 今日 +{{ biz.todayForwarded }}</div>
                  </div>
                  <i class="ri-arrow-up-line stat-trend ok" />
                </div>
              </el-col>

              <el-col :xs="12" :sm="12" :md="12" :lg="12" :xl="6">
                <div class="stat-card dash-box clickable" role="button" tabindex="0" @click="focusLogs('filter_error')">
                  <div class="stat-icon muted"><i class="ri-filter-3-line" /></div>
                  <div class="stat-meta">
                    <div class="stat-value">{{ biz.filtered }}</div>
                    <div class="stat-label muted">已过滤</div>
                    <div class="stat-sub muted">今日 +{{ biz.todayFiltered }}</div>
                  </div>
                </div>
              </el-col>

              <el-col :xs="12" :sm="12" :md="12" :lg="12" :xl="6">
                <div class="stat-card dash-box clickable" role="button" tabindex="0" @click="focusLogs('errors')">
                  <div class="stat-icon err"><i class="ri-error-warning-line" /></div>
                  <div class="stat-meta">
                    <div class="stat-value err">{{ biz.errors + commentQ.fail }}</div>
                    <div class="stat-label muted">错误数</div>
                    <div class="stat-sub muted">评论失败 {{ commentQ.fail }} · 今日 +{{ biz.todayErrors }}</div>
                  </div>
                </div>
              </el-col>
            </el-row>

            <div class="split-line" />

            <div class="mini-kpis">
              <div class="kpi dash-box dash-box-soft">
                <div class="kpi-k">今日处理</div>
                <div class="kpi-v">{{ todayProcessed }}</div>
              </div>
              <div class="kpi dash-box dash-box-soft">
                <div class="kpi-k">今日过滤率</div>
                <div class="kpi-v">{{ todayFilterRate }}%</div>
              </div>
              <div class="kpi dash-box dash-box-soft">
                <div class="kpi-k">今日错误率</div>
                <div class="kpi-v err">{{ todayErrorRate }}%</div>
              </div>
            </div>

            <div class="kpi-sub muted">
              累计处理 {{ totalProcessed }} · 过滤率 {{ totalFilterRate }}% · 错误率 {{ totalErrorRate }}%
            </div>

            <div class="dash-box dash-box-soft total-box">
              <div class="total-grid">
                <div class="total-item">
                  <div class="total-k muted">共计处理</div>
                  <div class="total-v">{{ overallProcessed }}</div>
                  <div class="total-sub muted">消息 {{ totalProcessed }} · 评论 {{ commentProcessed }}</div>
                </div>
                <div class="total-item">
                  <div class="total-k muted">评论队列总量</div>
                  <div class="total-v">{{ commentTotal }}</div>
                  <div class="total-sub muted">待转发 {{ commentQ.pending }} · 成功 {{ commentQ.success }} · 失败 {{ commentQ.fail }}</div>
                </div>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="6">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-service-line" />
                <span>核心服务状态</span>
              </div>
              <div class="card-sub">
                <span>Service Status</span>
              </div>
            </div>
          </template>

          <div class="dash-stack">
            <div class="svc-block">
              <div class="svc-title">
                <i class="ri-movie-2-line" />
                <span>FFmpeg 队列</span>
              </div>
              <div class="svc-kv">
                <div class="kv dash-box">
                  <span class="k muted">任务数</span>
                  <span class="v">{{ service.ffmpegQueue }}</span>
                </div>
                <div class="kv dash-box">
                  <span class="k muted">活跃线程</span>
                  <span class="v">{{ service.ffmpegThreads }}</span>
                </div>
              </div>
            </div>

            <div class="split-line" />

            <div class="svc-block">
              <div class="svc-title">
                <i class="ri-global-line" />
                <span>当前代理</span>
              </div>
              <div class="proxy-line dash-box" :class="{ direct: !service.proxyEnabled }">
                <span class="proxy-value">{{ proxyText }}</span>
                <span v-if="service.proxyEnabled" class="proxy-tag ok">PROXY</span>
                <span v-else class="proxy-tag muted">DIRECT</span>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col :span="24">
        <div ref="logsAnchorRef" />
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-terminal-box-line" />
                <span>实时事件日志</span>
              </div>
              <div class="card-sub">
                <span>Event Logs</span>
                <span v-if="logs.length" class="dot">·</span>
                <span v-if="logs.length">最近 {{ logs.length }} 条</span>
              </div>
            </div>
          </template>

          <div class="log-toolbar">
            <div class="log-left">
              <el-input
                v-model="logQuery"
                size="small"
                clearable
                class="log-search"
                placeholder="搜索日志（关键词）"
              />
              <el-tag
                v-if="logPreset !== 'all'"
                size="small"
                closable
                class="log-preset"
                @close="setLogPreset('all')"
              >
                {{ logPresetLabel }}
              </el-tag>
              <el-checkbox-group v-model="logLevels" size="small" class="log-levels">
                <el-checkbox label="INFO">INFO</el-checkbox>
                <el-checkbox label="WARN">WARN</el-checkbox>
                <el-checkbox label="ERROR">ERROR</el-checkbox>
              </el-checkbox-group>
            </div>
            <div class="log-right">
              <el-checkbox v-model="stickToBottom" size="small">自动滚动</el-checkbox>
              <el-button size="small" @click="clearLogs">清空</el-button>
              <el-button size="small" @click="copyVisibleLogs">复制</el-button>
            </div>
          </div>

          <div class="console" ref="logBoxRef">
            <div v-for="it in filteredLogs" :key="it.id" class="line" :class="it.level.toLowerCase()">
              {{ it.text }}
            </div>
          </div>

          <div class="log-foot muted">显示 {{ filteredLogs.length }} / {{ logs.length }} 条</div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped lang="scss">
.dashboard {
  width: 100%;
  --dash-box-radius: 12px;
  --dash-box-border: var(--el-border-color-lighter);
  --dash-box-bg: var(--el-fill-color-extra-light, var(--el-fill-color-lighter));
  --dash-box-bg-soft: var(--el-fill-color-light);
}

.dash-row > :deep(.el-col) {
  display: flex;
  min-height: 0;
}

.dash-row > :deep(.el-col) > .bt-card {
  width: 100%;
  height: 100%;
}

.dash-stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dash-stack .split-line {
  margin: 0;
}

.dash-box {
  border: 1px solid var(--dash-box-border);
  background: var(--dash-box-bg);
  border-radius: var(--dash-box-radius);
  padding: 10px 10px;
}

.dash-box-soft {
  background: var(--dash-box-bg-soft);
}

.dash-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}

.dash-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.dash-title i {
  font-size: 20px;
  color: var(--el-color-primary);
}

.dash-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.dash-main {
  font-size: 16px;
  font-weight: 850;
  line-height: 1.1;
  color: var(--el-text-color-primary);
}

.dash-sub {
  font-size: 12px;
  line-height: 1.1;
}

.dot {
  opacity: 0.7;
}

.pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  line-height: 1;
  padding: 3px 8px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  letter-spacing: 0.2px;
}

.pill.live {
  color: #67c23a;
  background: rgba(103, 194, 58, 0.12);
}

.pill.mock {
  color: rgba(191, 203, 217, 0.75);
  background: rgba(191, 203, 217, 0.08);
}

.pill.offline {
  color: #f56c6c;
  background: rgba(245, 108, 108, 0.12);
}

.sys-err {
  font-size: 11px;
  color: #f56c6c;
  border: 1px solid rgba(245, 108, 108, 0.22);
  background: rgba(245, 108, 108, 0.08);
  padding: 2px 6px;
  border-radius: 999px;
}

.metric {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.metric-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;

  .label {
    font-size: 12px;
    color: var(--el-text-color-regular);
  }
  .value {
    font-size: 12px;
    color: var(--el-text-color-primary);
    font-variant-numeric: tabular-nums;
  }
}

.gpu-ok {
  color: #67c23a;
}

.gpu-off {
  color: rgba(191, 203, 217, 0.55);
}

.overview-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.ov-card {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.ov-icon {
  margin-top: 2px;

  i {
    font-size: 18px;
  }
}

.os-icon {
  color: rgba(191, 203, 217, 0.9);
}

.os-icon.ubuntu {
  color: #e95420;
}

.os-icon.windows {
  color: #409eff;
}

.os-icon.apple {
  color: rgba(191, 203, 217, 0.92);
}

.ov-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ov-main {
  font-weight: 750;
  font-size: 13px;
  line-height: 1.25;
  color: var(--el-text-color-primary);
  word-break: break-word;
}

.ov-sub {
  font-size: 12px;
  line-height: 1.2;
}

.ov-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.ov-tag {
  border-color: rgba(255, 255, 255, 0.12);
  background: rgba(0, 0, 0, 0.22);
  color: rgba(191, 203, 217, 0.92);
}

.net-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.net-item {
  display: flex;
  gap: 10px;

  i {
    font-size: 18px;
    margin-top: 2px;
  }
}

.net-item.up i {
  color: #409eff;
}

.net-item.down i {
  color: #67c23a;
}

.net-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.net-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.net-value {
  font-size: 13px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

.stat-row {
  margin-top: 2px;
}

.stat-card {
  position: relative;
  display: flex;
  gap: 10px;
  align-items: center;
  min-height: 64px;
}

.clickable {
  cursor: pointer;
  transition: border-color 0.15s ease, transform 0.15s ease;
}

.clickable:hover {
  border-color: rgba(64, 158, 255, 0.35);
}

.clickable:active {
  transform: translateY(0.5px);
}

.clickable:focus-visible {
  outline: 2px solid rgba(64, 158, 255, 0.35);
  outline-offset: 2px;
}

.stat-icon {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(255, 255, 255, 0.08);

  i {
    font-size: 18px;
  }
}

.stat-icon.pending {
  color: #409eff;
  background: rgba(64, 158, 255, 0.12);
}

.stat-icon.ok {
  color: #67c23a;
  background: rgba(103, 194, 58, 0.12);
}

.stat-icon.err {
  color: #f56c6c;
  background: rgba(245, 108, 108, 0.12);
}

.stat-icon.muted {
  color: rgba(191, 203, 217, 0.75);
  background: rgba(191, 203, 217, 0.08);
}

.stat-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.stat-value {
  font-size: 18px;
  font-weight: 800;
  letter-spacing: 0.2px;
  font-variant-numeric: tabular-nums;
}

.stat-sub {
  font-size: 12px;
  line-height: 1.1;
}

.stat-value.err {
  color: #f56c6c;
}

.stat-label {
  font-size: 12px;
}

.stat-trend {
  position: absolute;
  right: 10px;
  top: 10px;
  font-size: 14px;
}

.stat-trend.ok {
  color: #67c23a;
}

.mini-kpis {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
  gap: 10px;
}

.kpi {
}

.kpi-k {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.kpi-v {
  margin-top: 4px;
  font-size: 14px;
  font-weight: 750;
  font-variant-numeric: tabular-nums;
}

.kpi-v.err {
  color: #f56c6c;
}

.kpi-sub {
  font-size: 12px;
}

.total-box {
  padding: 12px 12px;
}

.total-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.total-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.total-k {
  font-size: 12px;
}

.total-v {
  font-size: 20px;
  font-weight: 850;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-primary);
}

.total-sub {
  font-size: 12px;
  line-height: 1.2;
  word-break: break-word;
}

.svc-block {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.svc-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;

  i {
    font-size: 16px;
    color: rgba(191, 203, 217, 0.9);
  }
}

.svc-kv {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.kv {
  display: flex;
  justify-content: space-between;
  gap: 10px;

  .k {
    font-size: 12px;
  }
  .v {
    font-size: 14px;
    font-weight: 750;
    font-variant-numeric: tabular-nums;
  }
}

.proxy-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.proxy-value {
  font-size: 13px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

.proxy-tag {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.proxy-tag.ok {
  color: #67c23a;
  background: rgba(103, 194, 58, 0.12);
}

.proxy-tag.muted {
  color: rgba(191, 203, 217, 0.75);
  background: rgba(191, 203, 217, 0.08);
}

.log-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}

.log-left {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}

.log-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.log-search {
  width: 260px;
  max-width: 100%;
}

.log-preset {
  border-color: rgba(64, 158, 255, 0.22);
  background: rgba(64, 158, 255, 0.12);
  color: rgba(191, 203, 217, 0.92);
}

.log-levels :deep(.el-checkbox) {
  margin-right: 6px;
}

.console {
  height: 320px;
  overflow: auto;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: #000;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.55;
  color: #c9d1d9;
}

.line {
  white-space: pre-wrap;
  word-break: break-word;
  font-variant-numeric: tabular-nums;
}

.line.info {
  color: #c9d1d9;
}

.line.warn {
  color: #f59e0b;
}

.line.error {
  color: #f87171;
}

div.log-foot {
  margin-top: 8px;
  font-size: 12px;
}

@media (max-width: 1200px) {
  .mini-kpis {
    grid-template-columns: 1fr 1fr;
  }

  .overview-grid {
    grid-template-columns: 1fr;
  }

  .net-row {
    grid-template-columns: 1fr;
  }

  .total-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .mini-kpis {
    grid-template-columns: 1fr;
  }

  .overview-grid {
    grid-template-columns: 1fr;
  }

  .net-row {
    grid-template-columns: 1fr;
  }

  .log-search {
    width: 100%;
  }

  .console {
    height: 260px;
  }
}
</style>
