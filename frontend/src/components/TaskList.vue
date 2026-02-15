<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { getTaskProgress, getTasks, taskAction, type Task, type TaskProgress } from '../api'

const props = defineProps<{
  token: string
}>()

const tasks = ref<Task[]>([])
const loading = ref(false)
const progressMap = ref<Record<number, TaskProgress>>({})
const autoRefresh = ref(true)

const logDrawerOpen = ref(false)
const logTask = ref<Task | null>(null)
const logProgress = computed(() => (logTask.value ? progressMap.value[logTask.value.ID] : undefined))

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
  if (!props.token.trim()) {
    tasks.value = []
    progressMap.value = {}
    return
  }

  loading.value = true
  try {
    tasks.value = await getTasks(props.token)
    await refreshProgress()
  } catch (err: any) {
    ElMessage.error(err?.message || '加载任务失败')
  } finally {
    loading.value = false
  }
}

async function refreshProgress() {
  if (!props.token.trim()) return
  if (tasks.value.length === 0) return

  const ids = tasks.value.map((t) => t.ID).filter((id) => id > 0)
  if (ids.length === 0) return

  try {
    const results = await Promise.all(
      ids.map(async (id) => {
        try {
          const p = await getTaskProgress(props.token, id)
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
  if (!props.token.trim() || id <= 0) return
  try {
    const p = await getTaskProgress(props.token, id)
    progressMap.value = { ...progressMap.value, [id]: p }
  } catch {
    // best-effort
  }
}

async function doAction(task: Task, action: 'start' | 'pause' | 'stop') {
  if (!props.token.trim()) {
    ElMessage.warning('请先填写 Token')
    return
  }
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
    await taskAction(props.token, task.ID, action)
    await reloadTasks()
    await refreshOne(task.ID)
    ElMessage.success('指令已发送')
  } catch (err: any) {
    ElMessage.error(err?.message || '操作失败')
  }
}

function openLogs(task: Task) {
  logTask.value = task
  logDrawerOpen.value = true
  void refreshOne(task.ID)

  if (logTimer) window.clearInterval(logTimer)
  logTimer = window.setInterval(() => {
    if (!logDrawerOpen.value || !logTask.value) return
    void refreshOne(logTask.value.ID)
  }, 1000)
}

function closeLogs() {
  logDrawerOpen.value = false
  if (logTimer) {
    window.clearInterval(logTimer)
    logTimer = undefined
  }
}

function startPolling() {
  if (pollTimer) window.clearInterval(pollTimer)
  pollTimer = window.setInterval(() => {
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

watch(
  () => props.token,
  async () => {
    await reloadTasks()
  },
)

onMounted(async () => {
  await reloadTasks()
  startPolling()
})

onBeforeUnmount(() => {
  stopPolling()
  closeLogs()
})

defineExpose({
  reloadTasks,
})
</script>

<template>
  <div class="task-list">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button type="primary" @click="reloadTasks" :loading="loading">刷新</el-button>
        <el-switch v-model="autoRefresh" active-text="自动刷新" inactive-text="手动" />
      </div>
      <div class="toolbar-right">
        <el-text type="info">共 {{ tasks.length }} 个任务</el-text>
      </div>
    </div>

    <el-table :data="tasks" v-loading="loading" stripe style="width: 100%">
      <el-table-column prop="ID" label="ID" width="80" />

      <el-table-column label="源 / 目标" min-width="340">
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
            <el-button size="small" type="success" @click="doAction(row, 'start')">启动</el-button>
            <el-button size="small" type="warning" @click="doAction(row, 'pause')">暂停</el-button>
            <el-button size="small" type="danger" @click="doAction(row, 'stop')">停止</el-button>
            <el-button size="small" @click="openLogs(row)">日志</el-button>
          </el-space>
        </template>
      </el-table-column>
    </el-table>

    <el-drawer v-model="logDrawerOpen" title="实时日志" size="40%" @close="closeLogs">
      <template #default>
        <div v-if="!logTask" class="empty">请选择一个任务</div>
        <div v-else>
          <div class="drawer-head">
            <div class="drawer-title">
              <el-text>#{{ logTask.ID }}</el-text>
              <el-tag class="ml8" :type="statusTagType(logTask)">{{ statusText(logTask) }}</el-tag>
            </div>
            <el-button size="small" @click="refreshOne(logTask.ID)">刷新</el-button>
          </div>

          <div class="drawer-meta">
            <el-text type="info">源：{{ logTask.source_url }}</el-text>
            <el-text type="info">目标：{{ logTask.target_url }}</el-text>
          </div>

          <el-divider />

          <div class="logs">
            <div v-if="(logProgress?.logs?.length ?? 0) === 0" class="empty">暂无日志</div>
            <pre v-else class="log-pre"><code>{{ logProgress?.logs?.join('\n') }}</code></pre>
          </div>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped>
.task-list {
  width: 100%;
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

.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.drawer-title {
  display: flex;
  align-items: center;
}

.ml8 {
  margin-left: 8px;
}

.drawer-meta {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.logs {
  height: calc(100vh - 260px);
  overflow: auto;
}

.log-pre {
  margin: 0;
  padding: 12px;
  background: #0b1020;
  color: #d4d7dd;
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.empty {
  padding: 12px;
  color: #909399;
}
</style>

