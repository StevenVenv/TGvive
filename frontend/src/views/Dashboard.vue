<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'

type LogLevel = 'INFO' | 'WARN' | 'ERROR'
type LogItem = { id: number; level: LogLevel; text: string; ts: number }

type StatsSnapshot = {
  ts: number
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
  ffmpeg_active: number
  ffmpeg_threads: number
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

const storageProxyKey = 'tgvive_proxy_config'

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

const sys = reactive({
  cpu: 0,
  memUsed: 0,
  memTotal: 0,
  diskUsed: 0,
  diskTotal: 0,
  gpuDetected: false,
  gpuName: '',
  upBps: 0,
  downBps: 0,
})

const biz = reactive({
  pending: 0,
  forwarded: 0,
  filtered: 0,
  errors: 0,
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

const proxyText = computed(() => {
  if (!service.proxyEnabled || !service.proxyEndpoint) return '直连模式'
  if (!service.proxyLatency || service.proxyLatency <= 0) return `${service.proxyEndpoint} (--ms)`
  return `${service.proxyEndpoint} (${service.proxyLatency}ms)`
})

const wsConnected = ref(false)
const lastWSErr = ref('')

const modeBadge = computed(() => (wsConnected.value ? 'LIVE' : 'OFFLINE'))
const modeClass = computed(() => (wsConnected.value ? 'live' : 'offline'))

const logs = ref<LogItem[]>([])
const logBoxRef = ref<HTMLElement | null>(null)
let localLogId = -1

function pushLog(level: LogLevel, text: string, id?: number, ts?: number) {
  const entry: LogItem = {
    id: typeof id === 'number' ? id : localLogId--,
    level,
    text,
    ts: typeof ts === 'number' ? ts : Date.now(),
  }
  logs.value = [...logs.value, entry].slice(-50)
  void nextTick(() => {
    const el = logBoxRef.value
    if (!el) return
    el.scrollTop = el.scrollHeight
  })
}

function loadProxyConfig() {
  try {
    const raw = (localStorage.getItem(storageProxyKey) || '').trim()
    if (!raw) {
      service.proxyEnabled = false
      service.proxyEndpoint = ''
      return
    }
    const cfg = JSON.parse(raw || '{}') as any
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
    service.proxyEnabled = false
    service.proxyEndpoint = ''
  }
}

function applyStats(d: StatsSnapshot) {
  if (!d) return
  sys.cpu = clamp(Number(d.cpu_pct || 0), 0, 100)
  sys.memUsed = toGiB(Number(d.mem_used || 0))
  sys.memTotal = Math.max(0.1, toGiB(Number(d.mem_total || 0)) || 0.1)
  sys.diskUsed = toGiB(Number(d.disk_used || 0))
  sys.diskTotal = Math.max(0.1, toGiB(Number(d.disk_total || 0)) || 0.1)
  sys.upBps = Math.max(0, Number(d.up_bps || 0))
  sys.downBps = Math.max(0, Number(d.down_bps || 0))
  sys.gpuDetected = !!d.gpu_detected
  sys.gpuName = String(d.gpu_name || '')

  biz.pending = Math.max(0, Number(d.pending || 0))
  biz.forwarded = Math.max(0, Number(d.success || 0))
  biz.filtered = Math.max(0, Number(d.filtered || 0))
  biz.errors = Math.max(0, Number(d.fail || 0))

  service.ffmpegQueue = Math.max(0, Number(d.ffmpeg_active || 0))
  service.ffmpegThreads = Math.max(0, Number(d.ffmpeg_threads || 0))

  loadProxyConfig()
}

function applyLog(ev: LogEvent) {
  if (!ev || typeof ev.message !== 'string') return
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
    scheduleReconnect()
  }
}

function refresh() {
  connectWS()
}

defineExpose({ refresh })

onMounted(() => {
  loadProxyConfig()
  connectWS()
})

onBeforeUnmount(() => {
  cleanupWS()
})
</script>

<template>
  <div class="dashboard">
    <el-row :gutter="12">
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
                <span class="dot">·</span>
                <span class="pill" :class="modeClass">{{ modeBadge }}</span>
                <span v-if="!wsConnected && lastWSErr" class="sys-err" :title="lastWSErr">WS_OFFLINE</span>
              </div>
            </div>
          </template>

          <div class="metric">
            <div class="metric-top">
              <span class="label">CPU 使用率</span>
              <span class="value">{{ cpuPct }}%</span>
            </div>
            <el-progress :percentage="cpuPct" :stroke-width="10" :show-text="false" />
          </div>

          <div class="metric">
            <div class="metric-top">
              <span class="label">内存使用</span>
              <span class="value">{{ fmtGiB(sys.memUsed) }} / {{ fmtGiB(sys.memTotal) }}</span>
            </div>
            <el-progress :percentage="memPct" :stroke-width="10" :show-text="false" status="success" />
          </div>

          <div class="metric">
            <div class="metric-top">
              <span class="label">磁盘存储</span>
              <span class="value">{{ fmtGiB(sys.diskUsed) }} / {{ fmtGiB(sys.diskTotal) }}</span>
            </div>
            <el-progress :percentage="diskPct" :stroke-width="10" :show-text="false" status="warning" />
          </div>

          <div class="split-line" />

          <div class="gpu-row">
            <i class="ri-cpu-line" :class="sys.gpuDetected ? 'gpu-ok' : 'gpu-off'" />
            <div class="gpu-meta">
              <div class="gpu-name" :class="{ muted: !sys.gpuDetected }">
                {{ sys.gpuDetected ? sys.gpuName : '未检测到 GPU' }}
              </div>
              <div class="gpu-sub muted">GPU Status</div>
            </div>
          </div>

          <div class="net-row">
            <div class="net-item up">
              <i class="ri-upload-2-line" />
              <div class="net-meta">
                <div class="net-label">上传</div>
                <div class="net-value">{{ fmtSpeedBps(sys.upBps) }}</div>
              </div>
            </div>
            <div class="net-item down">
              <i class="ri-download-2-line" />
              <div class="net-meta">
                <div class="net-label">下载</div>
                <div class="net-value">{{ fmtSpeedBps(sys.downBps) }}</div>
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
                <span class="dot">·</span>
                <span class="pill" :class="modeClass">{{ modeBadge }}</span>
              </div>
            </div>
          </template>

          <el-row :gutter="10" class="stat-row">
            <el-col :xs="12" :sm="12" :lg="6">
              <div class="stat-card">
                <div class="stat-icon pending"><i class="ri-time-line" /></div>
                <div class="stat-meta">
                  <div class="stat-value">{{ biz.pending }}</div>
                  <div class="stat-label muted">待转发</div>
                </div>
              </div>
            </el-col>

            <el-col :xs="12" :sm="12" :lg="6">
              <div class="stat-card">
                <div class="stat-icon ok"><i class="ri-line-chart-line" /></div>
                <div class="stat-meta">
                  <div class="stat-value">{{ biz.forwarded }}</div>
                  <div class="stat-label muted">已转发</div>
                </div>
                <i class="ri-arrow-up-line stat-trend ok" />
              </div>
            </el-col>

            <el-col :xs="12" :sm="12" :lg="6">
              <div class="stat-card">
                <div class="stat-icon muted"><i class="ri-filter-3-line" /></div>
                <div class="stat-meta">
                  <div class="stat-value">{{ biz.filtered }}</div>
                  <div class="stat-label muted">已过滤</div>
                </div>
              </div>
            </el-col>

            <el-col :xs="12" :sm="12" :lg="6">
              <div class="stat-card">
                <div class="stat-icon err"><i class="ri-error-warning-line" /></div>
                <div class="stat-meta">
                  <div class="stat-value err">{{ biz.errors }}</div>
                  <div class="stat-label muted">错误数</div>
                </div>
              </div>
            </el-col>
          </el-row>

          <div class="split-line" />

          <div class="mini-kpis">
            <div class="kpi">
              <div class="kpi-k">过滤率</div>
              <div class="kpi-v">{{ Math.round((biz.filtered / Math.max(1, biz.forwarded)) * 100) }}%</div>
            </div>
            <div class="kpi">
              <div class="kpi-k">错误占比</div>
              <div class="kpi-v err">{{ Math.round((biz.errors / Math.max(1, biz.forwarded)) * 100) }}%</div>
            </div>
            <div class="kpi">
              <div class="kpi-k">任务积压</div>
              <div class="kpi-v">{{ biz.pending }}</div>
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
                <span class="dot">·</span>
                <span class="pill" :class="modeClass">{{ modeBadge }}</span>
              </div>
            </div>
          </template>

          <div class="svc-block">
            <div class="svc-title">
              <i class="ri-movie-2-line" />
              <span>FFmpeg 队列</span>
            </div>
            <div class="svc-kv">
              <div class="kv">
                <span class="k muted">任务数</span>
                <span class="v">{{ service.ffmpegQueue }}</span>
              </div>
              <div class="kv">
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
            <div class="proxy-line" :class="{ direct: !service.proxyEnabled }">
              <span class="proxy-value">{{ proxyText }}</span>
              <span v-if="service.proxyEnabled" class="proxy-tag ok">PROXY</span>
              <span v-else class="proxy-tag muted">DIRECT</span>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col :span="24">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-terminal-box-line" />
                <span>实时事件日志</span>
              </div>
              <div class="card-sub">
                <span>Event Logs</span>
                <span class="dot">·</span>
                <span class="pill" :class="modeClass">{{ modeBadge }}</span>
              </div>
            </div>
          </template>

          <div class="console" ref="logBoxRef">
            <div v-for="it in logs" :key="it.id" class="line" :class="it.level.toLowerCase()">
              {{ it.text }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped lang="scss">
.dashboard {
  width: 100%;
}

.bt-card {
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.02);

  :deep(.el-card__header) {
    padding: 12px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
    background: rgba(0, 0, 0, 0.18);
  }

  :deep(.el-card__body) {
    padding: 14px;
  }
}

.card-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  color: var(--el-text-color-primary);

  i {
    font-size: 16px;
    color: #409eff;
  }
}

.card-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
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

.muted {
  color: var(--el-text-color-secondary);
}

.split-line {
  height: 1px;
  background: rgba(255, 255, 255, 0.06);
  margin: 12px 0;
}

.metric {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 12px;
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

.gpu-row {
  display: flex;
  align-items: center;
  gap: 10px;

  i {
    font-size: 18px;
  }
}

.gpu-ok {
  color: #67c23a;
}

.gpu-off {
  color: rgba(191, 203, 217, 0.55);
}

.gpu-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.gpu-name {
  font-weight: 650;
  font-size: 13px;
}

.gpu-sub {
  font-size: 12px;
}

.net-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  margin-top: 12px;
}

.net-item {
  display: flex;
  gap: 10px;
  padding: 10px 10px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.015);

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
  padding: 12px 12px;
  border-radius: 12px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.015);
  min-height: 64px;
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
  padding: 10px 10px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(0, 0, 0, 0.12);
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
  padding: 10px 10px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.015);

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
  padding: 10px 10px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.015);
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

@media (max-width: 768px) {
  .mini-kpis {
    grid-template-columns: 1fr;
  }

  .net-row {
    grid-template-columns: 1fr;
  }
}
</style>
