<script setup lang="ts">
import { computed, ref } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'

export type ScheduleRule = {
  start: string
  end: string
  limit: number
}

export type StrategyFormModel = {
  ID: number
  name: string
  remark: string

  clone_mode: number
  allowed_types: string[]
  content_types: string[]
  block_file_exts: string[]
  allow_file_exts: string[]

  scope_type: number
  scope_value: string
  history_order: number
  poll_interval: number
  enable_realtime: boolean
  schedule_rules: ScheduleRule[]

  keep_reply: boolean
  realtime: boolean
  clone_comment: boolean
  gpu_accel: boolean
  change_md5: boolean

  delay_min_ms: number
  delay_max_ms: number

  daily_limit: number
  run_window: string
}

export type StrategyFormExpose = {
  validate: () => Promise<boolean>
  clearValidate: () => void
}

const props = withDefaults(
  defineProps<{
    modelValue: StrategyFormModel
    loading?: boolean
    submitText?: string
    cancelText?: string
    showActions?: boolean
  }>(),
  {
    loading: false,
    submitText: '保存',
    cancelText: '取消',
    showActions: true,
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: StrategyFormModel): void
  (e: 'submit'): void
  (e: 'cancel'): void
}>()

const formRef = ref<FormInstance>()
const form = computed(() => props.modelValue)

const enablePush = computed<boolean>({
  get() {
    const v = (form.value as any).enable_realtime
    if (v === undefined || v === null) return Boolean(form.value.realtime)
    return Boolean(v)
  },
  set(v) {
    form.value.enable_realtime = Boolean(v)
    form.value.realtime = Boolean(v) // legacy field for backward compatibility
  },
})

const enablePull = computed<boolean>({
  get() {
    return Number(form.value.poll_interval ?? 0) > 0
  },
  set(v) {
    if (!v) {
      form.value.poll_interval = 0
      return
    }
    const cur = Number(form.value.poll_interval ?? 0)
    if (!Number.isFinite(cur) || cur <= 0) form.value.poll_interval = 60
  },
})

function normalizeScheduleRules(input: any): ScheduleRule[] {
  const raw = Array.isArray(input) ? input : []
  const out: ScheduleRule[] = []
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue
    const start = String((item as any).start ?? '').trim()
    const end = String((item as any).end ?? '').trim()
    const limit = Number((item as any).limit ?? 0)
    if (!start || !end) continue
    if (!Number.isFinite(limit) || limit <= 0) continue
    out.push({ start, end, limit: Math.floor(limit) })
  }
  return out
}

function ensureScheduleRulesArray() {
  if (!Array.isArray(form.value.schedule_rules)) form.value.schedule_rules = []
}

function addScheduleRule() {
  ensureScheduleRulesArray()
  form.value.schedule_rules = [...form.value.schedule_rules, { start: '', end: '', limit: 1 }]
}

function removeScheduleRule(index: number) {
  ensureScheduleRulesArray()
  form.value.schedule_rules = form.value.schedule_rules.filter((_, i) => i !== index)
}

type AllowedTypeKey = 'text' | 'image' | 'video' | 'audio' | 'file'

const allowedTypeOptions: Array<{ key: AllowedTypeKey; label: string; icon: string }> = [
  { key: 'text', label: '文本', icon: 'ri-file-text-line' },
  { key: 'image', label: '图片', icon: 'ri-image-line' },
  { key: 'video', label: '视频', icon: 'ri-video-line' },
  { key: 'audio', label: '音频', icon: 'ri-volume-up-line' },
  { key: 'file', label: '文件', icon: 'ri-file-3-line' },
]

const allowedTypeKeys = allowedTypeOptions.map((x) => x.key)

const scopeValuePlaceholder = computed(() => {
  const t = Number(form.value.scope_type || 1)
  if (t === 2) return '例如：100'
  if (t === 3) return '例如：2025-01-01~2025-01-31'
  if (t === 4) return '例如：1000-2000'
  return '可留空'
})

function normalizeFileExtList(input: any): string[] {
  const raw = Array.isArray(input) ? input : []
  const seen = new Set<string>()
  const out: string[] = []
  for (const v of raw) {
    let k = String(v || '')
      .trim()
      .toLowerCase()
    if (!k) continue
    if (!k.startsWith('.')) k = '.' + k
    if (k === '.') continue
    if (/[ \t\r\n/\\]/.test(k)) continue
    if (seen.has(k)) continue
    seen.add(k)
    out.push(k)
  }
  return out
}

const fileExtSuggestions = ['.zip', '.rar', '.7z', '.apk', '.exe', '.dmg', '.pkg', '.pdf', '.docx', '.xlsx', '.pptx']

const blockFileExtOptions = computed(() => {
  const set = new Set<string>()
  normalizeFileExtList((form.value as any).block_file_exts).forEach((v) => set.add(v))
  fileExtSuggestions.forEach((v) => set.add(v))
  return Array.from(set)
})

const allowFileExtOptions = computed(() => {
  const set = new Set<string>()
  normalizeFileExtList((form.value as any).allow_file_exts).forEach((v) => set.add(v))
  fileExtSuggestions.forEach((v) => set.add(v))
  return Array.from(set)
})

const blockFileExts = computed<string[]>({
  get() {
    return normalizeFileExtList((form.value as any).block_file_exts)
  },
  set(v) {
    ;(form.value as any).block_file_exts = normalizeFileExtList(v)
  },
})

const allowFileExts = computed<string[]>({
  get() {
    return normalizeFileExtList((form.value as any).allow_file_exts)
  },
  set(v) {
    ;(form.value as any).allow_file_exts = normalizeFileExtList(v)
  },
})

function normalizeAllowedTypes(input: any): AllowedTypeKey[] {
  const raw = Array.isArray(input) ? input : []
  const set = new Set<string>()
  for (const v of raw) {
    const k = String(v || '')
      .trim()
      .toLowerCase()
    if (!k) continue
    set.add(k)
  }
  const out: AllowedTypeKey[] = []
  for (const k of allowedTypeKeys) {
    if (set.has(k)) out.push(k)
  }
  return out
}

const allowedTypes = computed<AllowedTypeKey[]>({
  get() {
    const v = (form.value as any).allowed_types
    if (Array.isArray(v)) return normalizeAllowedTypes(v)
    return normalizeAllowedTypes(form.value.content_types)
  },
  set(v) {
    const next = normalizeAllowedTypes(v)
    ;(form.value as any).allowed_types = next
    form.value.content_types = next
  },
})

const rules: FormRules = {
  name: [{ required: true, message: '请填写策略名称', trigger: 'blur' }],
  poll_interval: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        if (!Number.isFinite(n)) cb(new Error('轮询间隔不合法'))
        else if (n < 0) cb(new Error('轮询间隔不能为负数'))
        else if (n > 0 && n < 10) cb(new Error('轮询间隔最小 10 秒'))
        else cb()
      },
      trigger: 'change',
    },
  ],
  delay_min_ms: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        if (!Number.isFinite(n) || n < 0) cb(new Error('最小延时不能为负数'))
        else cb()
      },
      trigger: 'change',
    },
  ],
  delay_max_ms: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        const min = Number(form.value.delay_min_ms ?? 0)
        if (!Number.isFinite(n) || n < 0) cb(new Error('最大延时不能为负数'))
        else if (n < min) cb(new Error('最大延时必须 ≥ 最小延时'))
        else cb()
      },
      trigger: 'change',
    },
  ],
  daily_limit: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        if (!Number.isFinite(n) || n < 0) cb(new Error('每日配额不能为负数'))
        else cb()
      },
      trigger: 'change',
    },
  ],
}

async function validate(): Promise<boolean> {
  const inst = formRef.value
  if (!inst) return false
  try {
    return await inst.validate()
  } catch {
    return false
  }
}

function clearValidate() {
  formRef.value?.clearValidate()
}

async function submit() {
  // keep legacy + new fields in sync
  form.value.enable_realtime = enablePush.value
  form.value.realtime = enablePush.value

  if (!enablePull.value) {
    form.value.poll_interval = 0
  }
  ;(form.value as any).block_file_exts = normalizeFileExtList((form.value as any).block_file_exts)
  ;(form.value as any).allow_file_exts = normalizeFileExtList((form.value as any).allow_file_exts)
  form.value.schedule_rules = normalizeScheduleRules(form.value.schedule_rules)
  const ok = await validate()
  if (!ok) return
  emit('submit')
}

function cancel() {
  emit('cancel')
}

defineExpose<StrategyFormExpose>({
  validate,
  clearValidate,
})
</script>

<template>
  <div class="strategy-form">
    <div class="form-scroll" v-loading="loading">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" size="small" class="form">
        <el-row :gutter="12">
          <el-col :xs="24" :sm="12">
            <el-form-item label="策略名称" prop="name">
              <el-input v-model="form.name" placeholder="例如：极速转发 / 去重+转码" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12">
            <el-form-item label="备注（可选）" prop="remark">
              <el-input v-model="form.remark" placeholder="用于团队协作/区分用途" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-card class="panel-card" shadow="never">
          <template #header>
            <div class="panel-head">
              <div class="panel-title">
                <i class="ri-sliders-2-line" />
                <span>基础配置</span>
              </div>
              <div class="panel-sub">Base Config</div>
            </div>
          </template>

          <el-row :gutter="12">
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="克隆模式" prop="clone_mode">
                <el-radio-group v-model="form.clone_mode" class="radio-dense">
                  <el-radio :label="1">转发</el-radio>
                  <el-radio :label="2">发送</el-radio>
                  <el-radio :label="3">下载上传</el-radio>
                </el-radio-group>
              </el-form-item>
            </el-col>

            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="历史方向" prop="history_order">
                <el-radio-group v-model="form.history_order" class="radio-dense">
                  <el-radio :label="1">从旧到新</el-radio>
                  <el-radio :label="2">从新到旧</el-radio>
                </el-radio-group>
              </el-form-item>
            </el-col>

            <el-col :xs="24" :sm="8" :lg="4">
              <el-form-item label="消息范围" prop="scope_type">
                <el-select v-model="form.scope_type" class="ctrl ctrl-sm" popper-class="tgvive-dark-popper">
                  <el-option :value="1" label="全部" />
                  <el-option :value="2" label="最近 N 条" />
                  <el-option :value="3" label="时间范围" />
                  <el-option :value="4" label="ID 范围" />
                </el-select>
              </el-form-item>
            </el-col>

            <el-col :xs="24" :sm="16" :lg="8">
              <el-form-item label="范围参数" prop="scope_value">
                <el-input v-model="form.scope_value" class="ctrl ctrl-md" :placeholder="scopeValuePlaceholder" />
              </el-form-item>
            </el-col>
          </el-row>
        </el-card>

        <el-card class="panel-card" shadow="never">
          <template #header>
            <div class="panel-head">
              <div class="panel-title">
                <i class="ri-shield-flash-line" />
                <span>风控与处理</span>
              </div>
              <div class="panel-sub">Anti-detect & Processing</div>
            </div>
          </template>

          <el-form-item label="允许转发的内容类型" prop="allowed_types">
            <el-checkbox-group v-model="allowedTypes" class="types-group">
              <el-checkbox v-for="opt in allowedTypeOptions" :key="opt.key" :label="opt.key" class="type-item">
                <i :class="opt.icon" />
                <span class="type-label">{{ opt.label }}</span>
              </el-checkbox>
            </el-checkbox-group>
            <div class="hint compact">未选择表示不过滤（全类型，包括未知类型）</div>
          </el-form-item>

          <el-row :gutter="12">
            <el-col :xs="24" :sm="12">
              <el-form-item prop="block_file_exts">
                <template #label>
                  <span class="label-with-icon">
                    <i class="ri-forbid-2-line" />
                    <span>屏蔽文件后缀</span>
                  </span>
                </template>
                <el-select
                  v-model="blockFileExts"
                  class="ctrl"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  reserve-keyword
                  clearable
                  collapse-tags
                  collapse-tags-tooltip
                  placeholder="例如：.apk .zip（回车添加）"
                  popper-class="tgvive-dark-popper"
                >
                  <el-option v-for="opt in blockFileExtOptions" :key="opt" :label="opt" :value="opt" />
                </el-select>
                <div class="hint compact">命中后缀：该文件将被跳过</div>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12">
              <el-form-item prop="allow_file_exts">
                <template #label>
                  <span class="label-with-icon">
                    <i class="ri-shield-check-line" />
                    <span>保留文件后缀（白名单）</span>
                  </span>
                </template>
                <el-select
                  v-model="allowFileExts"
                  class="ctrl"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  reserve-keyword
                  clearable
                  collapse-tags
                  collapse-tags-tooltip
                  placeholder="为空表示不过滤（全放行）"
                  popper-class="tgvive-dark-popper"
                >
                  <el-option v-for="opt in allowFileExtOptions" :key="opt" :label="opt" :value="opt" />
                </el-select>
                <div class="hint compact">设置后，仅保留这些后缀的文件</div>
              </el-form-item>
            </el-col>
          </el-row>
          <div class="hint compact">仅对“文件”类型生效（非图片/音频/视频）。支持 zip 或 .zip，不区分大小写。</div>

          <div class="monitor-box">
            <div class="monitor-pane">
              <el-form-item class="monitor-item">
                <template #label>
                  <div class="monitor-label">
                    <i class="ri-broadcast-line" />
                    <span>实时监听 (Push)</span>
                  </div>
                </template>
                <el-switch v-model="enablePush" inline-prompt active-text="开" inactive-text="关" />
                <div class="hint compact">通过 Telegram 推送事件触发转发</div>
              </el-form-item>
            </div>

            <div class="monitor-pane">
              <el-form-item prop="poll_interval" class="monitor-item">
                <template #label>
                  <div class="monitor-label">
                    <i class="ri-loop-right-line" />
                    <span>定时兜底 (Pull)</span>
                    <el-tooltip effect="dark" placement="top" content="用于兜底：按间隔主动检测最新消息，建议间隔 60 秒以上。">
                      <i class="ri-question-line monitor-tip" />
                    </el-tooltip>
                  </div>
                </template>

                <div class="poll-mode">
                  <el-switch v-model="enablePull" inline-prompt active-text="开" inactive-text="关" />
                  <div v-if="enablePull" class="poll-interval">
                    <el-input-number v-model="form.poll_interval" :min="10" :step="10" controls-position="right" class="poll-input" />
                    <span class="poll-unit">秒</span>
                  </div>
                </div>
              </el-form-item>
            </div>
          </div>

          <el-row :gutter="12">
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="最小延时 (ms)" prop="delay_min_ms">
                <el-input-number v-model="form.delay_min_ms" :min="0" :step="100" controls-position="right" class="ctrl ctrl-num" />
                <div class="hint compact">每条消息处理后随机 sleep</div>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="最大延时 (ms)" prop="delay_max_ms">
                <el-input-number v-model="form.delay_max_ms" :min="0" :step="100" controls-position="right" class="ctrl ctrl-num" />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="每日配额" prop="daily_limit">
                <el-input-number v-model="form.daily_limit" :min="0" :step="10" controls-position="right" class="ctrl ctrl-num" />
                <div class="hint compact">0 = 不限制</div>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="运行窗口" prop="run_window">
                <el-input v-model="form.run_window" class="ctrl ctrl-sm" placeholder="09:00-18:00（留空=全天）" />
              </el-form-item>
            </el-col>
          </el-row>

          <div class="sub-split">
            <span>分时段计划</span>
          </div>

          <el-table :data="form.schedule_rules" size="small" border class="schedule-table" empty-text="未配置">
            <el-table-column label="开始时间" width="150">
              <template #default="{ row }">
                <el-time-picker
                  v-model="row.start"
                  value-format="HH:mm"
                  format="HH:mm"
                  placeholder="HH:mm"
                  class="ctrl ctrl-time"
                  popper-class="tgvive-dark-popper"
                />
              </template>
            </el-table-column>
            <el-table-column label="结束时间" width="150">
              <template #default="{ row }">
                <el-time-picker
                  v-model="row.end"
                  value-format="HH:mm"
                  format="HH:mm"
                  placeholder="HH:mm"
                  class="ctrl ctrl-time"
                  popper-class="tgvive-dark-popper"
                />
              </template>
            </el-table-column>
            <el-table-column label="时段配额" width="160">
              <template #default="{ row }">
                <el-input-number v-model="row.limit" :min="1" :step="1" controls-position="right" class="ctrl ctrl-num" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="90" align="right">
              <template #default="{ $index }">
                <el-button type="danger" link size="small" @click="removeScheduleRule($index)">
                  <i class="ri-delete-bin-line" />
                </el-button>
              </template>
            </el-table-column>
          </el-table>

          <div class="schedule-actions">
            <el-button type="primary" plain size="small" @click="addScheduleRule">
              <i class="ri-add-line" />
              添加时段
            </el-button>
            <div class="hint">示例：10:00-11:00 配额 2；12:00-13:00 配额 4</div>
          </div>

          <div class="sub-split">
            <span>处理开关</span>
          </div>

          <div class="switch-wrap">
            <el-switch v-model="form.keep_reply" active-text="保留回复" />
            <el-switch v-model="form.clone_comment" active-text="克隆评论" />
            <el-switch v-model="form.gpu_accel" active-text="GPU 加速" />
            <el-switch v-model="form.change_md5" active-text="修改 MD5" />
          </div>
        </el-card>
      </el-form>
    </div>

    <div v-if="showActions" class="actions">
      <slot name="actions">
        <el-space>
          <el-button @click="cancel">{{ cancelText }}</el-button>
          <el-button type="primary" @click="submit">{{ submitText }}</el-button>
        </el-space>
      </slot>
    </div>
  </div>
</template>

<style scoped lang="scss">
.strategy-form {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.form-scroll {
  flex: 1;
  overflow: auto;
  padding-right: 2px;
}

.form {
  width: 100%;
  max-width: 1200px;
  margin: 0 auto;
}

.form :deep(.el-form-item) {
  margin-bottom: 12px;
}

.form :deep(.el-form-item__label) {
  padding: 0 0 6px;
  line-height: 1.15;
  color: #a6a9ad;
}

.hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.hint.compact {
  margin-top: 6px;
}

.label-with-icon {
  display: inline-flex;
  align-items: center;
  gap: 8px;

  i {
    font-size: 14px;
    color: var(--el-color-primary);
  }
}

.radio-dense :deep(.el-radio) {
  margin-right: 12px;
}

.actions {
  display: flex;
  justify-content: flex-end;
}

.panel-card {
  border: 1px solid #363637;
  border-radius: 4px;
  background: #1e1e1e;
  margin-bottom: 12px;

  :deep(.el-card__header) {
    padding: 10px 12px;
    border-bottom: 1px solid #363637;
    background: #252525;
  }

  :deep(.el-card__body) {
    padding: 12px;
  }
}

.panel-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 800;
  color: var(--el-text-color-primary);

  i {
    font-size: 16px;
    color: var(--el-color-primary);
  }
}

.panel-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.ct-block {
  width: 100%;
}

.ct-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.ct-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.ct-mode {
  flex: none;
}

.ct-actions {
  flex: none;
}

.ct-meta {
  font-size: 12px;
  color: #a6a9ad;
  white-space: nowrap;
}

.ct-tags {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px 10px;
  align-items: center;
}

.ct-all {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
  color: #a6a9ad;
  font-size: 12px;
}

.ct-tags :deep(.el-check-tag) {
  border-radius: 4px;
  border: 1px solid #363637;
  background: #252525;
  color: #a6a9ad;
  padding: 6px 10px;
  height: 30px;
  line-height: 18px;
  display: inline-flex;
  align-items: center;
  transition: background 0.12s ease, border-color 0.12s ease, color 0.12s ease;
}

.ct-tags :deep(.el-check-tag.is-checked) {
  border-color: var(--el-color-primary);
  background: color-mix(in srgb, var(--el-color-primary) 18%, #252525);
  color: #ffffff;
}

.ct-tags :deep(.el-check-tag:hover) {
  border-color: color-mix(in srgb, #363637 60%, #ffffff);
}

.ct-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.ctrl {
  width: 100%;
}

@media (min-width: 768px) {
  .ctrl-sm {
    width: min(100%, 220px);
  }
  .ctrl-num {
    width: min(100%, 180px);
  }
  .ctrl-md {
    width: min(100%, 320px);
  }
}

.sub-split {
  margin: 10px 0 10px;
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.sub-split::before,
.sub-split::after {
  content: '';
  height: 1px;
  flex: 1;
  background: rgba(255, 255, 255, 0.06);
}

.monitor-box {
  border: 1px solid #363637;
  border-radius: 4px;
  background: #252525;
  padding: 12px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-bottom: 12px;
}

@media (max-width: 768px) {
  .monitor-box {
    grid-template-columns: 1fr;
  }
}

.monitor-box :deep(.el-form-item) {
  margin-bottom: 0;
}

.monitor-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;

  i {
    font-size: 14px;
    color: var(--el-color-primary);
  }
}

.monitor-tip {
  font-size: 14px;
  color: rgba(191, 203, 217, 0.7);
  cursor: pointer;
}

.monitor-tip:hover {
  color: rgba(255, 255, 255, 0.92);
}

.poll-mode {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}

.poll-interval {
  display: flex;
  align-items: center;
  margin-left: 15px;
}

.poll-input {
  width: 150px;
}

.poll-unit {
  margin-left: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.schedule-table {
  width: 100%;
}

.schedule-actions {
  margin-top: 10px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.schedule-actions .hint {
  margin-top: 0;
}

.switch-wrap {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  flex-wrap: wrap;
  gap: 12px 20px;
}

.switch-wrap :deep(.el-switch__label) {
  color: rgba(191, 203, 217, 0.9);
}

.switch-wrap :deep(.el-switch__label.is-active) {
  color: rgba(255, 255, 255, 0.92);
}

/* Fix: select text/placeholder in dark mode */
:global(html.dark) .strategy-form :deep(.el-select__wrapper) {
  background: rgba(255, 255, 255, 0.02);
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08) inset;
}

:global(html.dark) .strategy-form :deep(.el-select__selected-item) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .strategy-form :deep(.el-select__placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .strategy-form :deep(.el-select__input) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .strategy-form :deep(.el-select__input::placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .strategy-form :deep(.el-select__caret) {
  color: rgba(191, 203, 217, 0.75);
}
</style>
