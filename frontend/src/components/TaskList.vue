<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { UserFilled } from '@element-plus/icons-vue'

import {
  getKeywordProfiles,
  getStrategies,
  getTaskProgress,
  getTasks,
  listTGAccounts,
  taskAction,
  type KeywordProfile,
  type Strategy,
  type Task,
  type TaskProgress,
  type TGAccount,
} from '../api'
import { deleteTask, updateTask } from '../api/task'

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

const editVisible = ref(false)
const editLoading = ref(false)
const editSubmitting = ref(false)
const editFormRef = ref<FormInstance>()

const accounts = ref<TGAccount[]>([])
const strategies = ref<Strategy[]>([])
const keywordProfiles = ref<KeywordProfile[]>([])

const editForm = reactive({
  id: 0,
  source_url: '',
  target_url: '',
  session_key: '',
  strategy_id: 0,
  keyword_profile_id: 0,
})

let pollTimer: number | undefined
let logTimer: number | undefined

const selectedAccount = computed(() => accounts.value.find((a) => a.key === editForm.session_key) || null)

const editRules: FormRules = {
  source_url: [{ required: true, message: '请输入源频道/群组', trigger: 'blur' }],
  target_url: [{ required: true, message: '请输入目标频道/群组', trigger: 'blur' }],
  session_key: [{ required: true, message: '请选择执行账号', trigger: 'change' }],
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

  if (status === 3) return { type: 'danger', text: '异常', icon: 'ri-error-warning-line' }
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

async function reloadTasks() {
  loading.value = true
  try {
    tasks.value = await getTasks()
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
    const results = await Promise.all(
      ids.map(async (id) => {
        try {
          const p = await getTaskProgress(id)
          return [id, p] as const
        } catch {
          return null
        }
      }),
    )

    const next = { ...progressMap.value }
    for (const item of results) {
      if (!item) continue
      next[item[0]] = item[1]
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
    progressMap.value = { ...progressMap.value, [id]: p }
  } catch {
    // best-effort
  }
}

async function doAction(task: Task, action: 'start' | 'pause' | 'stop') {
  if (task.ID <= 0) return

  if (action === 'stop') {
    try {
      await ElMessageBox.confirm(`确定停止任务 #${task.ID} 吗？`, '确认', {
        confirmButtonText: '停止',
        cancelButtonText: '取消',
        type: 'warning',
      })
    } catch {
      return
    }
  }

  try {
    await taskAction(task.ID, action)
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
    const [acc, stg, kw] = await Promise.all([listTGAccounts(), getStrategies(), getKeywordProfiles()])
    accounts.value = acc || []
    strategies.value = stg || []
    keywordProfiles.value = kw || []
  } catch (err: any) {
    ElMessage.error(err?.message || '加载选项失败')
    accounts.value = []
    strategies.value = []
    keywordProfiles.value = []
  } finally {
    editLoading.value = false
  }
}

function openEdit(task: Task) {
  Object.assign(editForm, {
    id: Number(task.ID || 0),
    source_url: String(task.source_url || '').trim(),
    target_url: String(task.target_url || '').trim(),
    session_key: String(task.session_key || '').trim(),
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
    strategy_id: Number(editForm.strategy_id || 0),
    keyword_profile_id: Number(editForm.keyword_profile_id || 0),
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
    ElMessage.success('已删除')
    if (selectedTaskId.value === id) selectedTaskId.value = 0
    await reloadTasks()
  } catch (err: any) {
    ElMessage.error(err?.message || '删除失败')
  }
}

function selectTask(task: Task) {
  if (!task?.ID) return
  selectedTaskId.value = task.ID
  void refreshOne(task.ID)
}

function stopLogPolling() {
  if (logTimer) {
    window.clearInterval(logTimer)
    logTimer = undefined
  }
}

function startLogPolling() {
  stopLogPolling()
  const id = selectedTaskId.value
  if (id <= 0) return
  logTimer = window.setInterval(() => {
    if (props.active === false) return
    if (selectedTaskId.value !== id) return
    void refreshOne(id)
  }, 1000)
}

function scrollLogsToBottom() {
  const el = logBoxRef.value
  if (!el) return
  el.scrollTop = el.scrollHeight
}

function startPolling() {
  if (pollTimer) window.clearInterval(pollTimer)
  pollTimer = window.setInterval(() => {
    if (props.active === false) return
    if (!autoRefresh.value) return
    void refreshProgress()
  }, 2000)
}

function stopPolling() {
  if (pollTimer) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
}

onMounted(async () => {
  await reloadTasks()
  if (props.active !== false) startPolling()
})

onBeforeUnmount(() => {
  stopPolling()
  stopLogPolling()
})

watch(
  () => props.active,
  (v) => {
    if (v === false) {
      stopPolling()
      stopLogPolling()
      return
    }
    startPolling()
    if (selectedTaskId.value > 0) startLogPolling()
  },
)

watch(selectedTaskId, (id) => {
  if (props.active === false) return
  if (id > 0) startLogPolling()
  else stopLogPolling()
})

watch(
  () => (logProgress.value?.logs?.length ?? 0),
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
                <el-text type="info">点击任务行查看右侧日志</el-text>
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
                    <el-progress :percentage="progressMap[row.ID]?.progress_pct ?? 0" :stroke-width="10" />
                    <div class="sub">
                      <el-text type="info">{{ progressMap[row.ID]?.speed ?? '0 消息/秒' }}</el-text>
                      <el-text type="info">
                        成功 {{ progressMap[row.ID]?.success_cnt ?? 0 }} / 失败 {{ progressMap[row.ID]?.fail_cnt ?? 0 }}
                      </el-text>
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

                <el-table-column label="操作" width="340" fixed="right">
                  <template #default="{ row }">
                    <el-space size="small" wrap>
                      <el-button size="small" type="success" @click.stop="doAction(row, 'start')">启动</el-button>
                      <el-button size="small" type="warning" @click.stop="doAction(row, 'pause')">暂停</el-button>
                      <el-button size="small" type="danger" @click.stop="doAction(row, 'stop')">停止</el-button>
                      <el-button link type="primary" @click.stop="openEdit(row)">
                        <i class="ri-edit-line" />
                        编辑
                      </el-button>
                      <el-button link type="primary" @click.stop="selectTask(row)">日志</el-button>
                      <el-button link type="danger" @click.stop="removeTask(row)">
                        <i class="ri-delete-bin-line" />
                        删除
                      </el-button>
                    </el-space>
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
                  <el-switch v-model="autoScroll" active-text="自动滚动" />
                </el-space>
              </div>

              <div class="log-meta">
                <el-text type="info">源：{{ selectedTask.source_url }}</el-text>
                <el-text type="info">目标：{{ selectedTask.target_url }}</el-text>
              </div>

              <el-divider />

              <div ref="logBoxRef" class="log-console">
                <div v-if="(logProgress?.logs?.length ?? 0) === 0" class="log-empty">暂无日志</div>
                <pre v-else class="log-pre"><code>{{ logProgress?.logs?.join('\n') }}</code></pre>
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
            <el-input v-model="editForm.source_url" placeholder="例如：https://t.me/source 或 @source" />
          </el-form-item>

          <el-form-item label="目标频道 (Target)" prop="target_url">
            <el-input v-model="editForm.target_url" placeholder="@channel_id" />
          </el-form-item>

          <el-form-item label="执行账号 (Account)" prop="session_key">
            <el-select
              v-model="editForm.session_key"
              placeholder="请选择执行账号"
              style="width: 100%"
              filterable
              popper-class="tgvive-dark-popper"
            >
              <template #prefix>
                <div class="select-prefix">
                  <el-avatar class="select-avatar" :size="20" :src="selectedAccount?.avatar || ''" :icon="UserFilled" />
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

          <el-form-item label="策略模版 (Strategy)" prop="strategy_id">
            <el-select
              v-model="editForm.strategy_id"
              placeholder="请选择策略模版"
              style="width: 100%"
              filterable
              popper-class="tgvive-dark-popper"
            >
              <el-option v-for="s in strategies" :key="s.ID" :label="s.name" :value="s.ID">
                <div class="opt">
                  <div class="opt-left">
                    <div class="opt-title">{{ s.name }}</div>
                    <div v-if="s.remark" class="opt-sub">{{ s.remark }}</div>
                  </div>
                </div>
              </el-option>
            </el-select>
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

.bt-card {
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.02);
}

.bt-card :deep(.el-card__header) {
  padding: 12px 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(0, 0, 0, 0.18);
}

.bt-card :deep(.el-card__body) {
  padding: 14px;
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
}

.card-title i {
  font-size: 16px;
  color: #409eff;
}

.card-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
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
  margin-top: 6px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
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
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: #000;
  padding: 12px;
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
  color: rgba(191, 203, 217, 0.6);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.edit-body {
  padding: 4px 2px 0;
}

.edit-form :deep(.el-form-item) {
  margin-bottom: 14px;
}

.hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.muted {
  color: var(--el-text-color-secondary);
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
