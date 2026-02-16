<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { getTaskProgress, getTasks, taskAction, type Task, type TaskProgress } from '../api'

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

let pollTimer: number | undefined
let logTimer: number | undefined

function statusTagType(task: Task): 'success' | 'warning' | 'danger' | 'info' {
  const p = progressMap.value[task.ID]
  if (p?.status === '异常') return 'danger'
  if (p?.status === '进行中') return 'success'
  if (p?.status === '已暂停') return 'warning'

  switch (task.status) {
    case 1:
      return 'success'
    case 2:
      return 'warning'
    case 3:
      return 'danger'
    default:
      return 'info'
  }
}

function statusText(task: Task): string {
  const p = progressMap.value[task.ID]
  if (p?.status) return p.status
  switch (task.status) {
    case 1:
      return '进行中'
    case 2:
      return '已暂停'
    case 3:
      return '异常'
    default:
      return '已停止'
  }
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

                <el-table-column label="状态" width="120">
                  <template #default="{ row }">
                    <el-tag :type="statusTagType(row)">{{ statusText(row) }}</el-tag>
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

                <el-table-column label="操作" width="260" fixed="right">
                  <template #default="{ row }">
                    <el-space>
                      <el-button size="small" type="success" @click.stop="doAction(row, 'start')">启动</el-button>
                      <el-button size="small" type="warning" @click.stop="doAction(row, 'pause')">暂停</el-button>
                      <el-button size="small" type="danger" @click.stop="doAction(row, 'stop')">停止</el-button>
                      <el-button size="small" @click.stop="selectTask(row)">日志</el-button>
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
                  <el-tag class="ml8" :type="statusTagType(selectedTask)">{{ statusText(selectedTask) }}</el-tag>
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
</style>
