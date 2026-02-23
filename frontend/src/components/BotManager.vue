<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { addTGBot, deleteTGBot, listTGBots, testTGBot, updateTGBot, type TGBot, type TGBotTestResult } from '../api'

type CheckStatus = 'idle' | 'checking' | 'ok' | 'fail'
type CheckState = { status: CheckStatus; checked_at?: number; result?: TGBotTestResult }

const bots = ref<TGBot[]>([])
const loading = ref(false)
const refreshAnim = ref(false)
const refreshPulse = ref(false)

const checkByID = ref<Record<string, CheckState>>({})

function fmtTime(unixSeconds?: number): string {
  if (!unixSeconds) return '-'
  return new Date(unixSeconds * 1000).toLocaleString()
}

function getCheckState(id: string): CheckState {
  return checkByID.value[id] || { status: 'idle' }
}

function statusType(st: CheckState): 'info' | 'success' | 'danger' | 'warning' {
  switch (st.status) {
    case 'ok':
      return 'success'
    case 'fail':
      return 'danger'
    case 'checking':
      return 'warning'
    default:
      return 'info'
  }
}

const botCountText = computed(() => `${bots.value.length} 个 Bot`)

async function reloadBots() {
  loading.value = true
  try {
    bots.value = await listTGBots()
  } catch (err: any) {
    ElMessage.error(err?.message || '加载 Bot 列表失败')
  } finally {
    loading.value = false
  }
}

async function handleRefresh() {
  refreshAnim.value = true
  refreshPulse.value = true
  try {
    await reloadBots()
    ElMessage.success('已刷新')
  } finally {
    window.setTimeout(() => {
      refreshAnim.value = false
      refreshPulse.value = false
    }, 650)
  }
}

defineExpose({ reloadBots })

// Add/Edit dialog
const dialogOpen = ref(false)
const dialogMode = ref<'add' | 'edit'>('add')
const editingID = ref<string>('')

const formName = ref('')
const formToken = ref('')
const formAPIBase = ref('https://api.telegram.org')
const formDisabled = ref(false)

const tokenDirty = ref(false)
const tokenPlaceholder = computed(() => (dialogMode.value === 'edit' ? '已设置（留空不修改）' : '必填'))

function markTokenDirty() {
  tokenDirty.value = true
}

function resetDialog() {
  dialogMode.value = 'add'
  editingID.value = ''
  formName.value = ''
  formToken.value = ''
  formAPIBase.value = 'https://api.telegram.org'
  formDisabled.value = false
  tokenDirty.value = false
}

function openAdd() {
  resetDialog()
  dialogMode.value = 'add'
  dialogOpen.value = true
}

function openEdit(b: TGBot) {
  resetDialog()
  dialogMode.value = 'edit'
  editingID.value = b.id
  formName.value = b.name || ''
  formAPIBase.value = b.api_base || 'https://api.telegram.org'
  formDisabled.value = !!b.disabled
  dialogOpen.value = true
}

async function saveBot() {
  const name = (formName.value || '').trim()
  const apiBase = (formAPIBase.value || '').trim()
  const token = (formToken.value || '').trim()

  if (dialogMode.value === 'add') {
    if (!token) {
      ElMessage.warning('请输入 Bot Token')
      return
    }
    try {
      await addTGBot({ name, token, api_base: apiBase })
      ElMessage.success('已添加')
      dialogOpen.value = false
      await reloadBots()
    } catch (err: any) {
      ElMessage.error(err?.message || '添加失败')
    }
    return
  }

  const id = editingID.value
  if (!id) return

  const payload: any = {
    name,
    api_base: apiBase,
    disabled: !!formDisabled.value,
  }
  if (tokenDirty.value && token) payload.token = token

  try {
    await updateTGBot(id, payload)
    ElMessage.success('已保存')
    dialogOpen.value = false
    await reloadBots()
  } catch (err: any) {
    ElMessage.error(err?.message || '保存失败')
  }
}

async function confirmAndDelete(b: TGBot) {
  try {
    await ElMessageBox.confirm(`确定删除 Bot「${(b.name || b.id).trim()}」吗？\n（将从后端移除 Token 配置）`, '确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await deleteTGBot(b.id)
    ElMessage.success('已删除')
    await reloadBots()
  } catch (err: any) {
    ElMessage.error(err?.message || '删除失败')
  }
}

async function runTest(b: TGBot) {
  const id = b.id
  const cur = getCheckState(id)
  if (cur.status === 'checking') return

  checkByID.value = { ...checkByID.value, [id]: { status: 'checking' } }
  try {
    const res = await testTGBot(id, { timeout_ms: 8000 })
    checkByID.value = {
      ...checkByID.value,
      [id]: { status: res.ok ? 'ok' : 'fail', checked_at: Math.floor(Date.now() / 1000), result: res },
    }
    if (res.ok) {
      ElMessage.success(`检测通过：@${res.username || '-'} (${res.latency_ms || 0}ms)`)
    } else {
      const reason = (res.description || res.error || '检测失败').trim()
      ElMessage.warning(reason)
    }
  } catch (err: any) {
    checkByID.value = {
      ...checkByID.value,
      [id]: { status: 'fail', checked_at: Math.floor(Date.now() / 1000), result: { ok: false, error: err?.message || '检测失败' } },
    }
    ElMessage.error(err?.message || '检测失败')
  }
}

onMounted(async () => {
  await reloadBots()
})
</script>

<template>
  <div class="bot-page">
    <div class="header">
      <div class="header-left">
        <div class="title">
          <i class="ri-robot-2-line" />
          <span>Bot 管理</span>
        </div>
        <div class="sub">
          <span class="muted">共 {{ botCountText }}</span>
          <span v-if="loading" class="muted">· 加载中...</span>
        </div>
      </div>

      <div class="header-right">
        <el-button class="btn" @click="handleRefresh" :disabled="loading">
          <i class="ri-refresh-line" :class="{ spinning: refreshAnim }" />
          <span>刷新</span>
        </el-button>
        <el-button type="primary" class="btn primary" @click="openAdd">
          <i class="ri-add-line" />
          <span>添加 Bot</span>
        </el-button>
      </div>
    </div>

    <div class="table-card" :class="{ pulse: refreshPulse }">
      <el-table class="bot-table" :data="bots" v-loading="loading" size="small" stripe row-key="id">
        <el-table-column label="Bot" min-width="220">
          <template #default="{ row }">
            <div class="bot-info">
              <div class="bot-name">{{ (row.name || '').trim() || '-' }}</div>
              <div class="muted mono small">ID: {{ row.id }}</div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="Token" min-width="180">
          <template #default="{ row }">
            <div class="mono token">{{ row.token_mask || (row.token_set ? '***' : '-') }}</div>
          </template>
        </el-table-column>

        <el-table-column label="Bot API" min-width="240">
          <template #default="{ row }">
            <div class="mono api">{{ (row.api_base || '').trim() || 'https://api.telegram.org' }}</div>
          </template>
        </el-table-column>

        <el-table-column label="检测" min-width="240">
          <template #default="{ row }">
            <div class="check">
              <el-tag :type="statusType(getCheckState(row.id))" size="small" effect="light">
                <template v-if="getCheckState(row.id).status === 'idle'">未检测</template>
                <template v-else-if="getCheckState(row.id).status === 'checking'">检测中...</template>
                <template v-else-if="getCheckState(row.id).status === 'ok'">OK</template>
                <template v-else>FAIL</template>
              </el-tag>
              <span v-if="getCheckState(row.id).checked_at" class="muted mono small">· {{ fmtTime(getCheckState(row.id).checked_at) }}</span>
              <span v-if="getCheckState(row.id).result?.username" class="muted mono small">· @{{ getCheckState(row.id).result?.username }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="260" fixed="right">
          <template #default="{ row }">
            <div class="actions" @click.stop>
              <el-button class="btn mini" @click.stop="runTest(row)" :loading="getCheckState(row.id).status === 'checking'">
                <i class="ri-pulse-line" />
                <span>检测</span>
              </el-button>
              <el-button class="btn mini" @click.stop="openEdit(row)">
                <i class="ri-edit-2-line" />
                <span>编辑</span>
              </el-button>
              <el-button class="btn mini danger" @click.stop="confirmAndDelete(row)">
                <i class="ri-delete-bin-line" />
                <span>删除</span>
              </el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dialogOpen" :title="dialogMode === 'add' ? '添加 Bot' : '编辑 Bot'" width="560px" :close-on-click-modal="false">
      <el-form label-position="top">
        <el-form-item label="名称（可选）">
          <el-input v-model="formName" placeholder="例如：发布机器人 A" />
        </el-form-item>

        <el-form-item label="Bot Token">
          <el-input
            v-model="formToken"
            type="password"
            show-password
            :placeholder="tokenPlaceholder"
            @input="markTokenDirty"
          />
          <div class="muted small tip">建议只在这里输入一次，列表仅展示脱敏 Token。</div>
        </el-form-item>

        <el-form-item label="Bot API Base（可自建）">
          <el-input v-model="formAPIBase" placeholder="https://api.telegram.org 或 http://127.0.0.1:8081" />
          <div class="muted small tip">用于适配自建 Telegram Bot API Server，留空则默认官方。</div>
        </el-form-item>

        <el-form-item label="状态">
          <el-switch v-model="formDisabled" active-text="禁用" inactive-text="启用" :active-value="true" :inactive-value="false" />
        </el-form-item>
      </el-form>

      <template #footer>
        <div class="row">
          <el-button class="btn" @click="dialogOpen = false">取消</el-button>
          <el-button type="primary" class="btn primary" @click="saveBot">
            <i class="ri-save-3-line" />
            <span>保存</span>
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.bot-page {
  background: var(--am-bg, #f5f7fa);
  border: 1px solid var(--am-border, rgba(0, 0, 0, 0.08));
  border-radius: 14px;
  padding: 14px;
  color: var(--am-text, rgba(0, 0, 0, 0.88));
}

.header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 700;

  i {
    font-size: 18px;
  }
}

.sub {
  display: flex;
  gap: 6px;
  align-items: center;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;

  i {
    font-size: 16px;
  }
}

.mini {
  padding: 0 10px;
}

.muted {
  color: var(--am-muted, rgba(0, 0, 0, 0.6));
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.small {
  font-size: 12px;
}

.spinning {
  animation: spin 0.9s linear infinite;
}

.table-card {
  background: var(--am-panel, #ffffff);
  border: 1px solid var(--am-border, rgba(0, 0, 0, 0.08));
  border-radius: 14px;
  overflow: hidden;
  transition: box-shadow 0.35s ease, border-color 0.35s ease;

  &.pulse {
    border-color: rgba(64, 158, 255, 0.35);
    box-shadow: 0 0 0 1px rgba(64, 158, 255, 0.12), 0 12px 32px rgba(0, 0, 0, 0.18);
  }
}

.bot-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.bot-name {
  font-weight: 650;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.token,
.api {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.check {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.actions {
  display: flex;
  align-items: center;
  gap: 6px;
  row-gap: 6px;
  justify-content: flex-end;
  flex-wrap: wrap;
}

.danger {
  color: #ff4d4f;
}

.row {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.tip {
  margin-top: 6px;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
</style>
