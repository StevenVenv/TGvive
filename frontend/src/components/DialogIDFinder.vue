<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import { listTGAccountDialogs, listTGAccounts, type TGAccount, type TGDialogItem } from '../api'

const accounts = ref<TGAccount[]>([])
const loadingAccounts = ref(false)

const selectedKey = ref('')
const selectedAccount = computed(() => accounts.value.find((a) => a.key === selectedKey.value) || null)
const selectedLabel = computed(() => {
  const a = selectedAccount.value
  if (a) return accountLabel(a)
  const k = (selectedKey.value || '').trim()
  return k || '-'
})

const limit = ref(800)
const query = ref('')

const dialogs = ref<TGDialogItem[]>([])
const loadingDialogs = ref(false)

function accountLabel(a: TGAccount): string {
  const name = (a.name || '').trim()
  if (name) return name
  const u = (a.username || '').trim()
  if (u) return '@' + u
  const p = (a.phone || '').trim()
  if (p) return p
  return a.key
}

function kindTagType(kind: string): 'info' | 'success' | 'warning' {
  if (kind === 'supergroup') return 'success'
  if (kind === 'group') return 'warning'
  return 'info'
}

function kindLabel(kind: string): string {
  if (kind === 'supergroup') return '超级群'
  if (kind === 'group') return '群组'
  return '频道'
}

async function copyText(text: string) {
  const s = (text || '').trim()
  if (!s) return
  try {
    await navigator.clipboard.writeText(s)
    ElMessage.success('已复制')
    return
  } catch {
    // fallback below
  }

  try {
    const ta = document.createElement('textarea')
    ta.value = s
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    ta.style.top = '0'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (ok) ElMessage.success('已复制')
    else ElMessage.warning('复制失败')
  } catch {
    ElMessage.warning('复制失败')
  }
}

async function reloadAccounts() {
  loadingAccounts.value = true
  try {
    accounts.value = await listTGAccounts()
    if (!selectedKey.value && accounts.value.length > 0) {
      const first = accounts.value[0]
      if (first) selectedKey.value = first.key
    }
  } catch (err: any) {
    ElMessage.error(err?.message || '加载账号失败')
  } finally {
    loadingAccounts.value = false
  }
}

async function loadDialogs() {
  const key = (selectedKey.value || '').trim()
  if (!key) return

  loadingDialogs.value = true
  try {
    const lim = Math.max(50, Math.min(5000, Math.floor(Number(limit.value || 0) || 800)))
    dialogs.value = await listTGAccountDialogs(key, lim)
    ElMessage.success(`已加载 ${dialogs.value.length} 条`)
  } catch (err: any) {
    dialogs.value = []
    ElMessage.error(err?.message || '加载对话列表失败')
  } finally {
    loadingDialogs.value = false
  }
}

const filteredDialogs = computed(() => {
  const q = (query.value || '').trim().toLowerCase()
  if (!q) return dialogs.value
  return dialogs.value.filter((d) => {
    const t = String(d.title || '').toLowerCase()
    const u = String(d.username || '').toLowerCase()
    const b = String(d.bot_chat_id || '').toLowerCase()
    const v = String(d.task_value || '').toLowerCase()
    return t.includes(q) || u.includes(q) || b.includes(q) || v.includes(q)
  })
})

function describeSelection(): string {
  const a = selectedAccount.value
  if (!a) return ''
  const u = (a.username || '').trim()
  return u ? `(${a.key} / @${u})` : `(${a.key})`
}

defineExpose({
  reload: async () => {
    await reloadAccounts()
    if (selectedKey.value) await loadDialogs()
  },
})

watch(selectedKey, () => {
  dialogs.value = []
})

onMounted(async () => {
  await reloadAccounts()
})
</script>

<template>
  <div class="dialogs-page">
    <div class="toolbar">
      <el-select v-model="selectedKey" placeholder="选择账号" filterable style="width: 240px" :disabled="loadingAccounts">
        <el-option v-for="a in accounts" :key="a.key" :label="accountLabel(a)" :value="a.key" />
      </el-select>

      <el-input v-model="query" placeholder="搜索：名称 / @username / -100..." clearable style="width: 260px" />

      <el-input-number v-model="limit" :min="50" :max="5000" controls-position="right" style="width: 150px" />

      <el-button type="primary" :disabled="!selectedKey" :loading="loadingDialogs" @click="loadDialogs">加载</el-button>
      <el-button :loading="loadingAccounts" @click="reloadAccounts">刷新账号</el-button>
    </div>

    <div class="hint muted">
      <div>提示：私密频道/群没有 @username，可直接复制 “Bot Chat ID”（例如 -100123...）粘贴到任务的源/目标。</div>
      <div v-if="selectedKey">当前账号：{{ selectedLabel }} {{ describeSelection() }}</div>
    </div>

    <el-table :data="filteredDialogs" v-loading="loadingDialogs" size="small" stripe class="dlg-table">
      <el-table-column label="类型" width="110">
        <template #default="{ row }">
          <el-tag :type="kindTagType(row.kind)" size="small">{{ kindLabel(row.kind) }}</el-tag>
        </template>
      </el-table-column>

      <el-table-column label="名称" min-width="260">
        <template #default="{ row }">
          <div class="title">{{ row.title }}</div>
          <div v-if="row.username" class="muted mono">@{{ row.username }}</div>
        </template>
      </el-table-column>

      <el-table-column label="推荐填写" min-width="240">
        <template #default="{ row }">
          <div class="mono val">{{ row.task_value }}</div>
          <el-button link size="small" @click="copyText(row.task_value)">复制</el-button>
        </template>
      </el-table-column>

      <el-table-column label="Bot Chat ID" min-width="240">
        <template #default="{ row }">
          <div class="mono val">{{ row.bot_chat_id }}</div>
          <el-button link size="small" @click="copyText(row.bot_chat_id)">复制</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.dialogs-page {
  width: 100%;
}

.toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  margin-bottom: 10px;
}

.hint {
  margin-bottom: 10px;
  line-height: 1.6;
}

.muted {
  opacity: 0.72;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.title {
  font-weight: 600;
}

.val {
  word-break: break-all;
}

.dlg-table :deep(.el-table__cell) {
  vertical-align: top;
}
</style>
