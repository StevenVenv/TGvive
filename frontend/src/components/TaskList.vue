<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { CopyDocument, UserFilled } from '@element-plus/icons-vue'

import {
  getKeywordProfiles,
  getStrategies,
  getTaskProgress,
  getTaskProgressBatch,
  getTasks,
  listTGAccounts,
  listTGBots,
  taskAction,
  type KeywordProfile,
  type Strategy,
  type Task,
  type TaskProgress,
  type TGAccount,
  type TGBot,
} from '../api'
import { deleteTask, updateTask } from '../api/task'
import { useInterval } from '../composables/useInterval'
import { useTaskLogCache } from '../composables/useTaskLogCache'

const props = defineProps<{
  active?: boolean
}>()

const tasks = ref<Task[]>([])
const loading = ref(false)
const progressMap = ref<Record<number, TaskProgress>>({})
const autoRefresh = ref(true)

const selectedTaskId = ref<number>(0)
const selectedTask = computed(() => tasks.value.find((t) => t.ID === selectedTaskId.value) || null)
const logProgress = computed(() => (selectedTaskId.value ? progressMap.value[selectedTaskId.value] : undefined))
const autoScroll = ref(true)
const logBoxRef = ref<HTMLElement | null>(null)

const logCache = useTaskLogCache({ storageKey: 'tgvive_task_logs_v1', maxLines: 100 })

function clearTaskLogs(taskID: number) {
  logCache.clear(taskID)
}

const displayLogs = computed(() => {
  const id = selectedTaskId.value
  if (!id) return []
  const cached = logCache.get(id)
  if (Array.isArray(cached) && cached.length > 0) return cached
  const logs = logProgress.value?.logs
  if (Array.isArray(logs) && logs.length > 0) return logs.slice(-100)
  return []
})

const editVisible = ref(false)
const editLoading = ref(false)
const editSubmitting = ref(false)
const editFormRef = ref<FormInstance>()

const accounts = ref<TGAccount[]>([])
const strategies = ref<Strategy[]>([])
const keywordProfiles = ref<KeywordProfile[]>([])
const bots = ref<TGBot[]>([])

const editForm = reactive({
  id: 0,
  source_url: '',
  target_url: '',
  session_key: '',
  publish_type: 'same' as 'same' | 'account' | 'bot',
  publish_session_key: '',
  publish_bot_id: '',
  strategy_id: 0,
  keyword_profile_id: 0,
})

const progressPoll = useInterval(() => {
  if (props.active === false) return
  if (!autoRefresh.value) return
  void refreshProgress()
}, 2000)

const logPoll = useInterval(() => {
  if (props.active === false) return
  const id = selectedTaskId.value
  if (id <= 0) return
  void refreshOne(id)
}, 1000)

const selectedCrawlerAccount = computed(() => accounts.value.find((a) => a.key === editForm.session_key) || null)
const selectedPublishAccount = computed(() => accounts.value.find((a) => a.key === editForm.publish_session_key) || null)
const separatedPublish = computed(() => editForm.publish_type !== 'same')

const editRules: FormRules = {
  source_url: [{ required: true, message: '请输入源频道/群组', trigger: 'blur' }],
  target_url: [{ required: true, message: '请输入目标频道/群组', trigger: 'blur' }],
  session_key: [{ required: true, message: '请选择爬虫账号', trigger: 'change' }],
  publish_session_key: [
    {
      validator: (_: any, v: any, cb: any) => {
        if (editForm.publish_type !== 'account') return cb()
        const s = String(v || '').trim()
        if (s) cb()
        else cb(new Error('请选择发布账号'))
      },
      trigger: 'change',
    },
  ],
  publish_bot_id: [
    {
      validator: (_: any, v: any, cb: any) => {
        if (editForm.publish_type !== 'bot') return cb()
        const s = String(v || '').trim()
        if (s) cb()
        else cb(new Error('请选择发布 Bot'))
      },
      trigger: 'change',
    },
  ],
  strategy_id: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v || 0)
        if (n > 0) cb()
        else cb(new Error('请选择策略模版'))
      },
      trigger: 'change',
    },
  ],
}

function accountLabel(a: TGAccount): string {
  const name = (a.name || '').trim()
  if (name) return name
  const u = (a.username || '').trim()
  if (u) return u.startsWith('@') ? u : '@' + u
  const p = (a.phone || '').trim()
  if (p) return p
  return a.key
}

function accountFileName(key: string): string {
  key = (key || '').trim()
  if (!key) return '-'
  return `session_${key}.json`
}

type StatusView = {
  type: 'success' | 'warning' | 'danger' | 'info'
  text: string
  icon: string
  tooltip?: string
}

function parseNextRunTime(raw?: string | null): Date | null {
  const s = String(raw || '').trim()
  if (!s) return null
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return null
  return d
}

function formatDateTime(d: Date): string {
  try {
    return d.toLocaleString()
  } catch {
    return d.toISOString()
  }
}

function statusView(task: Task): StatusView {
  const status = Number(task?.status ?? 0)

  if (status === 3) {
    const tip = String(task?.last_error || '').trim()
    return {
      type: 'danger',
      text: '异常',
      icon: 'ri-error-warning-line',
      tooltip: tip || '点击任务行查看右侧日志',
    }
  }
  if (status === 2) return { type: 'warning', text: '已暂停', icon: 'ri-pause-circle-line' }
  if (status !== 1) return { type: 'info', text: '已停止', icon: 'ri-stop-circle-line' }

  if (task?.realtime) {
    return { type: 'success', text: '实时监控', icon: 'ri-broadcast-line' }
  }

  const next = parseNextRunTime(task?.next_run_time)
  if (next && next.getTime() > Date.now()) {
    return {
      type: 'warning',
      text: '等待调度',
      icon: 'ri-time-line',
      tooltip: `下次唤醒: ${formatDateTime(next)}`,
    }
  }

  return { type: 'success', text: '运行中', icon: 'ri-play-circle-line' }
}

function statusTagType(task: Task): 'success' | 'warning' | 'danger' | 'info' {
  return statusView(task).type
}

function statusText(task: Task): string {
  return statusView(task).text
}

function statusIcon(task: Task): string {
  return statusView(task).icon
}

function statusTooltip(task: Task): string {
  return statusView(task).tooltip || ''
}

function progressLine(p?: TaskProgress): string {
  const processed = Number(p?.processed_cnt ?? 0)
  const total = Number(p?.total_msg ?? 0)
  if (total > 0) return `已处理 ${processed} / ${total}`
  return `已处理 ${processed}`
}

function hasTotal(p?: TaskProgress): boolean {
  return Number(p?.total_msg ?? 0) > 0
}

function progressPct(p?: TaskProgress): number {
  const processed = Number(p?.processed_cnt ?? 0)
  const total = Number(p?.total_msg ?? 0)
  if (Number.isFinite(processed) && processed >= 0 && Number.isFinite(total) && total > 0) {
    const pct = (processed / total) * 100
    if (!Number.isFinite(pct) || pct <= 0) return 0
    if (pct >= 100) return 100
    return Math.round(pct * 10) / 10
  }

  const pct = Number(p?.progress_pct ?? 0)
  if (!Number.isFinite(pct) || pct <= 0) return 0
  if (pct >= 100) return 100
  return Math.round(pct)
}

function filteredCount(p?: TaskProgress): number {
  const processed = Number(p?.processed_cnt ?? 0)
  const success = Number(p?.success_cnt ?? 0)
  const fail = Number(p?.fail_cnt ?? 0)
  return Math.max(0, processed - success - fail)
}

function threadLine(p?: TaskProgress): string {
  const root = Number(p?.root_cnt ?? 0)
  const reply = Number(p?.reply_cnt ?? 0)
  if (!Number.isFinite(root) || !Number.isFinite(reply) || root + reply <= 0) return ''
  return `主贴 ${Math.max(0, root)} / 回复 ${Math.max(0, reply)}`
}

type ProgressStatus = 'success' | 'warning' | 'exception' | undefined

function isTaskRunningNow(task: Task): boolean {
  const status = Number(task?.status ?? 0)
  if (status !== 1) return false
  if (task?.realtime) return true

  const next = parseNextRunTime(task?.next_run_time)
  if (next && next.getTime() > Date.now()) return false
  return true
}

function progressBarStatus(task: Task, p?: TaskProgress): ProgressStatus {
  const status = Number(task?.status ?? 0)
  if (status === 3) return 'exception'
  if (status === 2) return 'warning'
  if (p?.status === '已完成') return 'success'
  return undefined
}

function progressBarIndeterminate(task: Task, p?: TaskProgress): boolean {
  return !hasTotal(p) && isTaskRunningNow(task)
}

function progressBarPercentage(task: Task, p?: TaskProgress): number {
  if (hasTotal(p)) return progressPct(p)
  if (progressBarIndeterminate(task, p)) return 40
  return 0
}

async function reloadTasks() {
  loading.value = true
  try {
    tasks.value = await getTasks()
    logCache.prune(tasks.value.map((t) => t.ID))
    if (selectedTaskId.value > 0 && !tasks.value.some((t) => t.ID === selectedTaskId.value)) {
      selectedTaskId.value = 0
    }
    await refreshProgress()
  } catch (err: any) {
    ElMessage.error(err?.message || '加载任务失败')
  } finally {
    loading.value = false
  }
}

async function refreshProgress() {
  if (tasks.value.length === 0) return

  const ids = tasks.value.map((t) => t.ID).filter((id) => id > 0)
  if (ids.length === 0) return

  try {
    const res = await getTaskProgressBatch(ids)
    const next = { ...progressMap.value }
    const entries = Object.entries(res || {})
    for (const [k, v] of entries) {
      const id = Math.floor(Number(k || 0))
      if (!Number.isFinite(id) || id <= 0) continue
      logCache.ingest(id, v?.logs)
      next[id] = v as TaskProgress
    }
    progressMap.value = next
  } catch {
    // best-effort
  }
}

async function refreshOne(id: number) {
  if (id <= 0) return
  try {
    const p = await getTaskProgress(id)
    logCache.ingest(id, p?.logs)
    progressMap.value = { ...progressMap.value, [id]: p }
  } catch {
    // best-effort
  }
}

async function doAction(task: Task, action: 'start' | 'pause' | 'stop' | 'restart') {
  if (task.ID <= 0) return

  if (action === 'stop' || action === 'restart') {
    try {
      const title = '确认'
      const tip = action === 'restart' ? `确定重启任务 #${task.ID} 吗？` : `确定停止任务 #${task.ID} 吗？`
      const confirmText = action === 'restart' ? '重启' : '停止'
      await ElMessageBox.confirm(tip, title, {
        confirmButtonText: confirmText,
        cancelButtonText: '取消',
        type: 'warning',
      })
    } catch {
      return
    }
  }

  try {
    await taskAction(task.ID, action)
    if (action === 'restart') clearTaskLogs(task.ID)
    await reloadTasks()
    await refreshOne(task.ID)
    ElMessage.success('指令已发送')
  } catch (err: any) {
    ElMessage.error(err?.message || '操作失败')
  }
}

async function loadEditOptions() {
  editLoading.value = true
  try {
    const [acc, stg, kw, bb] = await Promise.all([listTGAccounts(), getStrategies(), getKeywordProfiles(), listTGBots()])
    accounts.value = acc || []
    strategies.value = stg || []
    keywordProfiles.value = kw || []
    bots.value = bb || []
  } catch (err: any) {
    ElMessage.error(err?.message || '加载选项失败')
    accounts.value = []
    strategies.value = []
    keywordProfiles.value = []
    bots.value = []
  } finally {
    editLoading.value = false
  }
}

function openEdit(task: Task) {
  const rawPub = String((task as any).publish_type || '').trim()
  const publishType: 'same' | 'account' | 'bot' = rawPub === 'account' || rawPub === 'bot' ? (rawPub as any) : 'same'
  Object.assign(editForm, {
    id: Number(task.ID || 0),
    source_url: String(task.source_url || '').trim(),
    target_url: String(task.target_url || '').trim(),
    session_key: String(task.session_key || '').trim(),
    publish_type: publishType,
    publish_session_key: String((task as any).publish_session_key || '').trim(),
    publish_bot_id: String((task as any).publish_bot_id || '').trim(),
    strategy_id: Number(task.strategy_id || 0),
    keyword_profile_id: Number(task.keyword_profile_id || 0),
  })
  editVisible.value = true
  void loadEditOptions()
  void nextTick(() => editFormRef.value?.clearValidate?.())
}

async function submitEdit() {
  const inst = editFormRef.value
  if (!inst) return

  try {
    const ok = await inst.validate()
    if (!ok) return
  } catch {
    return
  }

  const id = Number(editForm.id || 0)
  if (id <= 0) return

  const payload = {
    source_url: String(editForm.source_url || '').trim(),
    target_url: String(editForm.target_url || '').trim(),
    session_key: String(editForm.session_key || '').trim(),
    publish_type: editForm.publish_type,
    publish_session_key: String(editForm.publish_session_key || '').trim(),
    publish_bot_id: String(editForm.publish_bot_id || '').trim(),
    strategy_id: Number(editForm.strategy_id || 0),
    keyword_profile_id: Number(editForm.keyword_profile_id || 0),
  }

  if (payload.publish_type === 'account' && !payload.publish_session_key) {
    ElMessage.warning('请选择发布账号')
    return
  }
  if (payload.publish_type === 'bot' && !payload.publish_bot_id) {
    ElMessage.warning('请选择发布 Bot')
    return
  }

  editSubmitting.value = true
  try {
    await updateTask(id, payload)
    ElMessage.success(`任务已更新 #${id}`)
    editVisible.value = false
    await reloadTasks()
    if (selectedTaskId.value === id) await refreshOne(id)
  } catch (err: any) {
    ElMessage.error(err?.message || '更新失败')
  } finally {
    editSubmitting.value = false
  }
}

async function removeTask(task: Task) {
  const id = Number(task?.ID || 0)
  if (id <= 0) return
  try {
    await ElMessageBox.confirm(`确定删除任务 #${id} 吗？`, '确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await deleteTask(id)
    clearTaskLogs(id)
    ElMessage.success('已删除')
    if (selectedTaskId.value === id) selectedTaskId.value = 0
    await reloadTasks()
  } catch (err: any) {
    ElMessage.error(err?.message || '删除失败')
  }
}

type TaskCommand = 'start' | 'restart' | 'pause' | 'stop' | 'edit' | 'log' | 'delete'

function handleTaskCommand(task: Task, cmd: string) {
  const c = String(cmd || '') as TaskCommand
  switch (c) {
    case 'start':
    case 'restart':
    case 'pause':
    case 'stop':
      void doAction(task, c)
      return
    case 'edit':
      openEdit(task)
      return
    case 'log':
      selectTask(task)
      return
    case 'delete':
      void removeTask(task)
      return
  }
}

function handleSelectedTaskCommand(cmd: TaskCommand | string) {
  const task = selectedTask.value
  if (!task) return
  handleTaskCommand(task, String(cmd || ''))
}

function selectTask(task: Task) {
  if (!task?.ID) return
  selectedTaskId.value = task.ID
  void refreshOne(task.ID)
}

function scrollLogsToBottom() {
  const el = logBoxRef.value
  if (!el) return
  el.scrollTop = el.scrollHeight
}

function fallbackCopyText(text: string): boolean {
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', 'true')
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    ta.style.top = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

async function copyLogs() {
  const text = displayLogs.value.join('\n').trim()
  if (!text) {
    ElMessage.info('暂无可复制日志')
    return
  }

  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      ElMessage.success('日志已复制')
      return
    }
  } catch {
    // ignore and fallback
  }

  const ok = fallbackCopyText(text)
  if (ok) ElMessage.success('日志已复制')
  else ElMessage.error('复制失败')
}

onMounted(async () => {
  await reloadTasks()
  if (props.active !== false) progressPoll.start()
})

onBeforeUnmount(() => {
  progressPoll.stop()
  logPoll.stop()
  logCache.persistNow()
})

watch(
  () => props.active,
  (v) => {
    if (v === false) {
      progressPoll.stop()
      logPoll.stop()
      return
    }
    progressPoll.start()
    if (selectedTaskId.value > 0) logPoll.start()
  },
)

watch(selectedTaskId, (id) => {
  if (props.active === false) return
  if (id > 0) logPoll.start()
  else logPoll.stop()
})

watch(
  () => displayLogs.value.length,
  async () => {
    if (!autoScroll.value) return
    await nextTick()
    scrollLogsToBottom()
  },
)

watch(autoScroll, async (v) => {
  if (!v) return
  await nextTick()
  scrollLogsToBottom()
})

watch(
  () => editForm.publish_type,
  () => {
    if (editForm.publish_type === 'same') {
      editForm.publish_session_key = ''
      editForm.publish_bot_id = ''
      return
    }
    if (editForm.publish_type === 'account') {
      editForm.publish_bot_id = ''
    }
    if (editForm.publish_type === 'bot') {
      editForm.publish_session_key = ''
    }

    const cur = strategies.value.find((s) => s.ID === Number(editForm.strategy_id || 0))
    const cm = Number(cur?.clone_mode || 0)
    if (cm && cm !== 3) {
      editForm.strategy_id = 0
    }
  },
)

defineExpose({
  reloadTasks,
})
</script>

<template>
  <div class="task-list">
    <el-row :gutter="12">
      <el-col :xs="24" :lg="16">
        <el-card class="bt-card pane-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-todo-line" />
                <span>任务管理</span>
              </div>
              <div class="card-sub">共 {{ tasks.length }} 个任务</div>
            </div>
          </template>

          <div class="pane">
            <div class="toolbar">
              <div class="toolbar-left">
                <el-button type="primary" @click="reloadTasks" :loading="loading">
                  <i class="ri-refresh-line" />
                  刷新
                </el-button>
                <el-switch v-model="autoRefresh" active-text="自动刷新" inactive-text="手动" />
              </div>
              <div class="toolbar-right">
                <el-space size="small" wrap>
                  <el-text type="info">点击任务行查看右侧日志</el-text>
                  <el-text v-if="selectedTask" type="success">已选中 #{{ selectedTask.ID }}</el-text>
                  <el-dropdown
                    trigger="click"
                    placement="bottom-end"
                    :disabled="!selectedTask"
                    @command="handleSelectedTaskCommand"
                  >
                    <el-button size="small" :disabled="!selectedTask">
                      操作
                      <i class="ri-arrow-down-s-line" />
                    </el-button>
                    <template #dropdown>
                      <el-dropdown-menu>
                        <el-dropdown-item command="start">启动</el-dropdown-item>
                        <el-dropdown-item command="restart">重启</el-dropdown-item>
                        <el-dropdown-item command="pause">暂停</el-dropdown-item>
                        <el-dropdown-item command="stop">停止</el-dropdown-item>
                        <el-dropdown-item divided command="edit">编辑</el-dropdown-item>
                        <el-dropdown-item command="log">日志</el-dropdown-item>
                        <el-dropdown-item divided command="delete" class="danger-item">删除</el-dropdown-item>
                      </el-dropdown-menu>
                    </template>
                  </el-dropdown>
                </el-space>
              </div>
            </div>

            <div class="table-body">
              <el-table
                :data="tasks"
                v-loading="loading"
                stripe
                highlight-current-row
                row-key="ID"
                :current-row-key="selectedTaskId || undefined"
                height="100%"
                style="width: 100%"
                @row-click="selectTask"
              >
                <el-table-column prop="ID" label="ID" width="80" />

                <el-table-column label="源 / 目标" min-width="320">
                  <template #default="{ row }">
                    <div class="peers">
                      <div class="peer">
                        <el-text type="info">源：</el-text>
                        <el-text>{{ row.source_url }}</el-text>
                      </div>
                      <div class="peer">
                        <el-text type="info">目标：</el-text>
                        <el-text>{{ row.target_url }}</el-text>
                      </div>
                    </div>
                  </template>
                </el-table-column>

                <el-table-column label="状态" width="160">
                  <template #default="{ row }">
                    <el-tooltip v-if="statusTooltip(row)" :content="statusTooltip(row)" placement="top" :show-after="200">
                      <el-tag :type="statusTagType(row)" class="status-tag">
                        <i class="status-icon" :class="statusIcon(row)" />
                        <span>{{ statusText(row) }}</span>
                      </el-tag>
                    </el-tooltip>
                    <el-tag v-else :type="statusTagType(row)" class="status-tag">
                      <i class="status-icon" :class="statusIcon(row)" />
                      <span>{{ statusText(row) }}</span>
                    </el-tag>
                  </template>
                </el-table-column>

                <el-table-column label="进度" min-width="220">
                  <template #default="{ row }">
                    <el-tooltip placement="top" :show-after="200">
                      <template #content>
                        <div class="progress-tip">
                          <div class="tip-line">状态：{{ statusText(row) }}</div>
                          <div class="tip-line">
                            {{ progressLine(progressMap[row.ID]) }}
                            <template v-if="hasTotal(progressMap[row.ID])">（{{ progressPct(progressMap[row.ID]) }}%）</template>
                          </div>
                        <div class="tip-line">
                          成功 {{ progressMap[row.ID]?.success_cnt ?? 0 }} / 失败 {{ progressMap[row.ID]?.fail_cnt ?? 0 }} /
                          过滤 {{ filteredCount(progressMap[row.ID]) }}
                        </div>
                        <div v-if="threadLine(progressMap[row.ID])" class="tip-line">{{ threadLine(progressMap[row.ID]) }}</div>
                        <div class="tip-line">速度：{{ progressMap[row.ID]?.speed ?? '0 消息/秒' }}</div>
                      </div>
                    </template>
                      <el-progress
                        :percentage="progressBarPercentage(row, progressMap[row.ID])"
                        :stroke-width="10"
                        :show-text="false"
                        :indeterminate="progressBarIndeterminate(row, progressMap[row.ID])"
                        :status="progressBarStatus(row, progressMap[row.ID])"
                      />
                    </el-tooltip>
                    <div class="sub">
                      <el-text type="info">{{ progressLine(progressMap[row.ID]) }}</el-text>
                      <el-text v-if="hasTotal(progressMap[row.ID])" type="info">{{ progressPct(progressMap[row.ID]) }}%</el-text>
                      <el-text v-else type="info">未知总量</el-text>
                    </div>
                    <div class="sub">
                      <el-text type="info">{{ progressMap[row.ID]?.speed ?? '0 消息/秒' }}</el-text>
                      <el-text type="info">
                        成功 {{ progressMap[row.ID]?.success_cnt ?? 0 }} / 失败 {{ progressMap[row.ID]?.fail_cnt ?? 0 }}
                        <template v-if="filteredCount(progressMap[row.ID]) > 0">
                          / 过滤 {{ filteredCount(progressMap[row.ID]) }}
                        </template>
                      </el-text>
                    </div>
                    <div v-if="threadLine(progressMap[row.ID])" class="sub">
                      <el-text type="info">{{ threadLine(progressMap[row.ID]) }}</el-text>
                    </div>
                  </template>
                </el-table-column>

                <el-table-column label="配额" width="160">
                  <template #default="{ row }">
                    <div class="quota">
                      <el-text>
                        今日 {{ row.today_count ?? 0 }} / {{ row.daily_limit && row.daily_limit > 0 ? row.daily_limit : '∞' }}
                      </el-text>
                      <el-text type="info">{{ row.run_window || '全天' }}</el-text>
                    </div>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="8">
        <el-card class="bt-card pane-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-terminal-box-line" />
                <span>实时日志</span>
              </div>
              <div class="card-sub">
                <span v-if="selectedTask">#{{ selectedTask.ID }}</span>
                <span v-else>—</span>
              </div>
            </div>
          </template>

          <div class="pane">
            <div v-if="!selectedTask" class="empty-wrap">
              <el-empty description="请选择任务查看日志" />
            </div>

            <div v-else class="log-pane">
              <div class="log-head">
                <div class="log-title">
                  <el-text>#{{ selectedTask.ID }}</el-text>
                  <el-tooltip
                    v-if="statusTooltip(selectedTask)"
                    :content="statusTooltip(selectedTask)"
                    placement="top"
                    :show-after="200"
                  >
                    <el-tag class="ml8 status-tag" :type="statusTagType(selectedTask)">
                      <i class="status-icon" :class="statusIcon(selectedTask)" />
                      <span>{{ statusText(selectedTask) }}</span>
                    </el-tag>
                  </el-tooltip>
                  <el-tag v-else class="ml8 status-tag" :type="statusTagType(selectedTask)">
                    <i class="status-icon" :class="statusIcon(selectedTask)" />
                    <span>{{ statusText(selectedTask) }}</span>
                  </el-tag>
                </div>
                <el-space size="small">
                  <el-button size="small" @click="refreshOne(selectedTask.ID)">
                    <i class="ri-refresh-line" />
                    刷新
                  </el-button>
                  <el-button size="small" @click="copyLogs" :disabled="displayLogs.length === 0">
                    <el-icon><CopyDocument /></el-icon>
                    复制
                  </el-button>
                  <el-button size="small" @click="clearTaskLogs(selectedTask.ID)" :disabled="displayLogs.length === 0">
                    <i class="ri-delete-bin-6-line" />
                    清空
                  </el-button>
                  <el-switch v-model="autoScroll" active-text="自动滚动" />
                </el-space>
              </div>

              <div class="log-meta">
                <el-text type="info">源：{{ selectedTask.source_url }}</el-text>
                <el-text type="info">目标：{{ selectedTask.target_url }}</el-text>
              </div>

              <div v-if="logProgress" class="log-meta">
                <el-text type="info">
                  进度：{{ progressLine(logProgress) }}
                  <template v-if="hasTotal(logProgress)">（{{ progressPct(logProgress) }}%）</template>
                  <template v-else>（未知总量）</template>
                </el-text>
                <el-text v-if="threadLine(logProgress)" type="info">{{ threadLine(logProgress) }}</el-text>
                <el-text type="info">
                  成功 {{ logProgress?.success_cnt ?? 0 }} / 失败 {{ logProgress?.fail_cnt ?? 0 }}
                  <template v-if="filteredCount(logProgress) > 0"> / 过滤 {{ filteredCount(logProgress) }}</template>
                  / 速度 {{ logProgress?.speed ?? '0 消息/秒' }}
                </el-text>
              </div>

              <el-divider />

              <div ref="logBoxRef" class="log-console">
                <div v-if="displayLogs.length === 0" class="log-empty">
                  <span v-if="selectedTask?.status === 3 && String(selectedTask?.last_error || '').trim()">
                    {{ String(selectedTask?.last_error || '').trim() }}
                  </span>
                  <span v-else>暂无日志</span>
                </div>
                <pre v-else class="log-pre"><code>{{ displayLogs.join('\n') }}</code></pre>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="editVisible" title="编辑任务" width="640px" class="bt-dialog" destroy-on-close>
      <div v-loading="editLoading" class="edit-body">
        <el-form ref="editFormRef" :model="editForm" :rules="editRules" label-position="top" class="edit-form">
          <el-form-item label="源频道 (Source)" prop="source_url">
            <el-input v-model="editForm.source_url" placeholder="例如：https://t.me/source 或 @source 或 -100123456789 (私密频道)" />
          </el-form-item>

          <el-form-item label="目标频道 (Target)" prop="target_url">
            <el-input v-model="editForm.target_url" placeholder="例如：@target 或 -100123456789 (私密频道)" />
          </el-form-item>

          <el-divider content-position="left">账号策略</el-divider>

          <el-form-item label="爬虫账号 (Crawler)" prop="session_key">
            <el-select
              v-model="editForm.session_key"
              placeholder="请选择爬虫账号"
              style="width: 100%"
              filterable
              popper-class="tgvive-dark-popper"
            >
              <template #prefix>
                <div class="select-prefix">
                  <el-avatar class="select-avatar" :size="20" :src="selectedCrawlerAccount?.avatar || ''" :icon="UserFilled" />
                </div>
              </template>
              <el-option v-for="a in accounts" :key="a.key" :label="accountLabel(a)" :value="a.key">
                <div class="opt">
                  <div class="opt-left">
                    <el-avatar class="opt-avatar" :size="26" :src="a.avatar" :icon="UserFilled" />
                    <div class="opt-meta">
                      <div class="opt-title">{{ accountLabel(a) }}</div>
                      <div class="opt-sub">{{ accountFileName(a.key) }}</div>
                    </div>
                  </div>
                  <div class="opt-right muted">{{ a.username ? '@' + a.username : '' }}</div>
                </div>
              </el-option>
            </el-select>
            <div class="hint">值为 session_key（对应 sessions/session_*.json）</div>
          </el-form-item>

          <el-form-item label="发布方式 (Publisher)" prop="publish_type">
            <el-radio-group v-model="editForm.publish_type">
              <el-radio-button label="same">同爬虫账号</el-radio-button>
              <el-radio-button label="account">TG账号</el-radio-button>
              <el-radio-button label="bot">Bot</el-radio-button>
            </el-radio-group>
            <div v-if="separatedPublish" class="hint">分开发送将强制使用下载上传（CloneMode=3），转发/复制策略会被禁用</div>
          </el-form-item>

          <el-form-item v-if="editForm.publish_type === 'account'" label="发布账号 (Publisher Account)" prop="publish_session_key">
            <el-select
              v-model="editForm.publish_session_key"
              placeholder="请选择发布账号"
              style="width: 100%"
              filterable
              popper-class="tgvive-dark-popper"
            >
              <template #prefix>
                <div class="select-prefix">
                  <el-avatar class="select-avatar" :size="20" :src="selectedPublishAccount?.avatar || ''" :icon="UserFilled" />
                </div>
              </template>
              <el-option v-for="a in accounts" :key="a.key" :label="accountLabel(a)" :value="a.key">
                <div class="opt">
                  <div class="opt-left">
                    <el-avatar class="opt-avatar" :size="26" :src="a.avatar" :icon="UserFilled" />
                    <div class="opt-meta">
                      <div class="opt-title">{{ accountLabel(a) }}</div>
                      <div class="opt-sub">{{ accountFileName(a.key) }}</div>
                    </div>
                  </div>
                  <div class="opt-right muted">{{ a.username ? '@' + a.username : '' }}</div>
                </div>
              </el-option>
            </el-select>
            <div class="hint">选择一个账号用于发布（与爬虫账号可不同）</div>
          </el-form-item>

          <el-form-item v-if="editForm.publish_type === 'bot'" label="发布 Bot (Publisher Bot)" prop="publish_bot_id">
            <el-select
              v-model="editForm.publish_bot_id"
              placeholder="请选择 Bot"
              style="width: 100%"
              filterable
              popper-class="tgvive-dark-popper"
            >
              <el-option v-for="b in bots" :key="b.id" :label="b.name || b.id" :value="b.id" :disabled="b.disabled">
                <div class="opt">
                  <div class="opt-left">
                    <div class="opt-title">{{ b.name || b.id }}</div>
                    <div class="opt-sub">{{ b.api_base }}</div>
                  </div>
                  <div class="opt-right muted">{{ b.disabled ? '已禁用' : b.token_mask || '' }}</div>
                </div>
              </el-option>
            </el-select>
            <div class="hint">使用 Bot API 发布（需将 Bot 加入目标频道/群并赋权）</div>
          </el-form-item>

          <el-form-item label="策略模版 (Strategy)" prop="strategy_id">
            <el-select
              v-model="editForm.strategy_id"
              placeholder="请选择策略模版"
              style="width: 100%"
              filterable
              popper-class="tgvive-dark-popper"
            >
              <el-option
                v-for="s in strategies"
                :key="s.ID"
                :label="s.name"
                :value="s.ID"
                :disabled="separatedPublish && Number(s.clone_mode || 0) !== 3"
              >
                <div class="opt">
                  <div class="opt-left">
                    <div class="opt-title">{{ s.name }}</div>
                    <div v-if="s.remark" class="opt-sub">{{ s.remark }}</div>
                  </div>
                </div>
              </el-option>
            </el-select>
            <div v-if="separatedPublish" class="hint">提示：分开发送仅支持 CloneMode=3（下载上传）</div>
          </el-form-item>

          <el-form-item label="关键词方案 (Keywords)" prop="keyword_profile_id">
            <el-select
              v-model="editForm.keyword_profile_id"
              placeholder="可选：关键词过滤/替换"
              style="width: 100%"
              filterable
              clearable
              popper-class="tgvive-dark-popper"
            >
              <el-option v-for="k in keywordProfiles" :key="k.ID" :label="k.name" :value="k.ID">
                <div class="opt">
                  <div class="opt-left">
                    <div class="opt-title">{{ k.name }}</div>
                    <div class="opt-sub">
                      <span v-if="k.remark">{{ k.remark }} · </span>
                      屏蔽 {{ k.block_words?.length || 0 }} | 白名单 {{ k.allow_words?.length || 0 }} | 替换
                      {{ k.replace_rules?.length || 0 }}
                    </div>
                  </div>
                </div>
              </el-option>
            </el-select>
            <div class="hint">可留空：仅使用行为策略；选择后将启用关键词过滤/替换</div>
          </el-form-item>
        </el-form>
      </div>

      <template #footer>
        <el-space>
          <el-button :disabled="editSubmitting" @click="editVisible = false">取消</el-button>
          <el-button type="primary" :loading="editSubmitting" @click="submitEdit">保存修改</el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.task-list {
  width: 100%;
}

.pane-card {
  height: calc(100vh - 120px);
  display: flex;
  flex-direction: column;
}

.pane-card :deep(.el-card__body) {
  flex: 1;
  overflow: hidden;
}

.pane {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.table-body {
  flex: 1;
  overflow: hidden;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.progress-tip {
  line-height: 1.6;
}

.tip-line {
  white-space: nowrap;
}

.peers {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.peer {
  display: flex;
  gap: 6px;
}

.sub {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.sub + .sub {
  margin-top: 4px;
}

.quota {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.empty-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.log-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.log-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.log-head :deep(.el-button .el-icon) {
  margin-right: 4px;
}

.log-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ml8 {
  margin-left: 8px;
}

.status-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.status-icon {
  font-size: 14px;
}

.log-meta {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.log-console {
  flex: 1;
  overflow: auto;
  border-radius: 10px;
  border: 1px solid var(--el-border-color-lighter);
  background: #0b1020;
  padding: 12px;
}

html.dark .log-console {
  background: #000;
}

.log-pre {
  margin: 0;
  color: #67c23a;
  font-size: 12px;
  line-height: 1.5;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  white-space: pre-wrap;
  word-break: break-word;
}

.log-empty {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.edit-body {
  padding: 4px 2px 0;
}

.edit-form :deep(.el-form-item) {
  margin-bottom: 14px;
}

:deep(.danger-item) {
  color: var(--el-color-danger);
}

.opt {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
  width: 100%;
}

.opt-left {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
}

.opt-avatar {
  flex-shrink: 0;
}

.opt-avatar :deep(img) {
  object-fit: cover;
}

.opt-meta {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.opt-title {
  font-weight: 700;
  color: var(--el-text-color-primary);
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.opt-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.opt-right {
  flex: none;
  font-size: 12px;
}

.select-prefix {
  display: flex;
  align-items: center;
}

.select-avatar {
  flex-shrink: 0;
  opacity: 0.95;
}

:deep(.select-avatar img) {
  object-fit: cover;
}
</style>
