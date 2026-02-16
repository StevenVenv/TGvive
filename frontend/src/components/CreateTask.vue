<script setup lang="ts">
import axios from 'axios'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { UserFilled } from '@element-plus/icons-vue'

type ApiResponse<T> = {
  code: number
  msg: string
  data: T
}

const emit = defineEmits<{
  (e: 'refresh'): void
}>()

const open = ref(false)
const loading = ref(false)
const submitting = ref(false)
const formRef = ref<FormInstance>()

type AccountItem = {
  key: string
  name?: string
  username?: string
  phone?: string
  avatar?: string
}

type StrategyItem = {
  ID: number
  name: string
  remark?: string
}

const accounts = ref<AccountItem[]>([])
const strategies = ref<StrategyItem[]>([])

const form = reactive({
  source_url: '',
  target_url: '',
  session_key: '',
  strategy_id: 0,
})

const rules: FormRules = {
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

function resetForm() {
  form.source_url = ''
  form.target_url = ''
  form.session_key = ''
  form.strategy_id = 0
}

const tokenStorageKey = 'tgvive_jwt_token'

function getStoredToken(): string {
  try {
    return (localStorage.getItem(tokenStorageKey) || '').trim()
  } catch {
    return ''
  }
}

async function apiGet<T>(path: string): Promise<T> {
  const token = getStoredToken()
  const res = await axios.get<ApiResponse<T>>(path, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (res.data.code !== 0) throw new Error(res.data.msg || '请求失败')
  return res.data.data
}

async function apiPost<T>(path: string, body: any): Promise<T> {
  const token = getStoredToken()
  const res = await axios.post<ApiResponse<T>>(path, body, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (res.data.code !== 0) throw new Error(res.data.msg || '请求失败')
  return res.data.data
}

function accountLabel(a: AccountItem): string {
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

const strategyTip = computed(() => {
  const id = Number(form.strategy_id || 0)
  if (!id) return ''
  const s = strategies.value.find((x) => x.ID === id)
  if (!s) return ''
  const remark = (s.remark || '').trim()
  return remark ? `备注：${remark}` : ''
})

async function loadOptions() {
  loading.value = true
  try {
    const [acc, stg] = await Promise.all([apiGet<AccountItem[]>('/api/v1/accounts'), apiGet<StrategyItem[]>('/api/v1/strategies')])
    accounts.value = acc || []
    strategies.value = stg || []

    const onlyAccount = accounts.value.length === 1 ? accounts.value[0] : undefined
    if (!form.session_key && onlyAccount) form.session_key = onlyAccount.key

    const onlyStrategy = strategies.value.length === 1 ? strategies.value[0] : undefined
    if (!form.strategy_id && onlyStrategy) form.strategy_id = onlyStrategy.ID
  } catch (err: any) {
    ElMessage.error(err?.message || '初始化失败')
    accounts.value = []
    strategies.value = []
  } finally {
    loading.value = false
  }
}

async function submit() {
  const inst = formRef.value
  if (!inst) return

  try {
    const ok = await inst.validate()
    if (!ok) return
  } catch {
    return
  }

  const payload = {
    source_url: (form.source_url || '').trim(),
    target_url: (form.target_url || '').trim(),
    session_key: (form.session_key || '').trim(),
    strategy_id: Number(form.strategy_id || 0),
  }

  if (!payload.source_url || !payload.target_url || !payload.session_key || !payload.strategy_id) {
    ElMessage.warning('请完整填写 4 个字段')
    return
  }

  submitting.value = true
  try {
    const created: any = await apiPost('/api/v1/tasks', payload)
    ElMessage.success(`任务已创建 #${created?.ID ?? ''}`.trim())
    open.value = false
    emit('refresh')
  } catch (err: any) {
    ElMessage.error(err?.message || '创建失败')
  } finally {
    submitting.value = false
  }
}

watch(open, (v) => {
  if (!v) {
    formRef.value?.clearValidate()
    resetForm()
  }
})

onMounted(() => {
  void loadOptions()
})
</script>

<template>
  <el-button type="primary" @click="open = true">新建任务</el-button>

  <el-dialog v-model="open" title="新建搬运任务" width="560px" class="bt-dialog">
    <div v-loading="loading" class="body">
        <el-form ref="formRef" :model="form" :rules="rules" label-position="top" class="form">
        <el-form-item label="源频道 (Source)" prop="source_url">
          <el-input v-model="form.source_url" placeholder="例如：https://t.me/source 或 @source" />
        </el-form-item>

        <el-form-item label="目标频道 (Target)" prop="target_url">
          <el-input v-model="form.target_url" placeholder="@channel_id" />
        </el-form-item>

        <el-form-item label="执行账号 (Account)" prop="session_key">
          <el-select
            v-model="form.session_key"
            placeholder="请选择执行账号"
            style="width: 100%"
            filterable
            popper-class="tgvive-dark-popper"
          >
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
            v-model="form.strategy_id"
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
          <div v-if="strategyTip" class="hint">{{ strategyTip }}</div>
        </el-form-item>
      </el-form>
    </div>

    <template #footer>
      <el-space>
        <el-button @click="open = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">创建</el-button>
      </el-space>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.body {
  padding: 4px 2px 0;
}

.form :deep(.el-form-item) {
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
}

.opt-left {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
}

.opt-avatar {
  flex: none;
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

.bt-dialog {
  :deep(.el-dialog) {
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(255, 255, 255, 0.02);
  }

  :deep(.el-dialog__header) {
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
    margin-right: 0;
  }

  :deep(.el-dialog__footer) {
    border-top: 1px solid rgba(255, 255, 255, 0.06);
  }
}

/* Bug A: ensure select/input text & placeholder readable in dark mode */
:global(html.dark) .bt-dialog :deep(.el-input__inner) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .bt-dialog :deep(.el-input__inner::placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .bt-dialog :deep(.el-input__wrapper) {
  background: rgba(255, 255, 255, 0.02);
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08) inset;
}

:global(html.dark) .bt-dialog :deep(.el-select__wrapper) {
  background: rgba(255, 255, 255, 0.02);
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08) inset;
}

:global(html.dark) .bt-dialog :deep(.el-select__selected-item) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .bt-dialog :deep(.el-select__placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .bt-dialog :deep(.el-select__input) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .bt-dialog :deep(.el-select__input::placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .bt-dialog :deep(.el-select__caret) {
  color: rgba(191, 203, 217, 0.75);
}

:global(html.dark) .tgvive-dark-popper.el-select-dropdown {
  background: rgba(20, 20, 20, 0.98);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

:global(html.dark) .tgvive-dark-popper .el-select-dropdown__item {
  color: rgba(191, 203, 217, 0.92);
  height: auto;
  line-height: normal;
  padding-top: 8px;
  padding-bottom: 8px;
  display: flex;
  align-items: center;
}

:global(html.dark) .tgvive-dark-popper .el-select-dropdown__item.is-hovering {
  background: rgba(64, 158, 255, 0.12);
}

:global(html.dark) .tgvive-dark-popper .el-select-dropdown__item.is-selected {
  color: #409eff;
}
</style>
