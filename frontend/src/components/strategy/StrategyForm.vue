<script setup lang="ts">
import { computed, ref } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'

export type StrategyFormModel = {
  ID: number
  name: string
  remark: string

  clone_mode: number
  content_types: string[]

  scope_type: number
  scope_value: string
  history_order: number

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

type ContentTypeKey = 'text' | 'image' | 'video' | 'audio' | 'file' | 'other'

const contentTypeOptions: Array<{ key: ContentTypeKey; label: string; icon: string }> = [
  { key: 'text', label: '文本', icon: 'ri-file-text-line' },
  { key: 'image', label: '图片', icon: 'ri-image-line' },
  { key: 'video', label: '视频', icon: 'ri-video-line' },
  { key: 'audio', label: '音频', icon: 'ri-volume-up-line' },
  { key: 'file', label: '文件', icon: 'ri-file-3-line' },
  { key: 'other', label: '其他', icon: 'ri-more-line' },
]

const allContentTypeKeys = contentTypeOptions.map((x) => x.key)
const lastCustomContentTypes = ref<ContentTypeKey[]>([])

type ContentPreset = 'common' | 'media' | 'all'

const contentPresets: Record<ContentPreset, ContentTypeKey[]> = {
  common: ['text', 'image', 'video', 'audio', 'file'],
  media: ['image', 'video', 'audio', 'file'],
  all: allContentTypeKeys,
}

function applyContentPreset(preset: ContentPreset) {
  const next = contentPresets[preset]
  form.value.content_types = [...next]
  lastCustomContentTypes.value = [...next]
}

const scopeValuePlaceholder = computed(() => {
  const t = Number(form.value.scope_type || 1)
  if (t === 2) return '例如：100'
  if (t === 3) return '例如：2025-01-01~2025-01-31'
  if (t === 4) return '例如：1000-2000'
  return '可留空'
})

function normalizeContentTypes(input: any): ContentTypeKey[] {
  const raw = Array.isArray(input) ? input : []
  const set = new Set<string>()
  for (const v of raw) {
    const k = String(v || '')
      .trim()
      .toLowerCase()
    if (!k) continue
    set.add(k)
  }
  const out: ContentTypeKey[] = []
  for (const k of allContentTypeKeys) {
    if (set.has(k)) out.push(k)
  }
  return out
}

const contentTypeMode = computed<'all' | 'custom'>({
  get() {
    const cur = Array.isArray(form.value.content_types) ? form.value.content_types : []
    return cur.length === 0 ? 'all' : 'custom'
  },
  set(v) {
    if (v === 'all') {
      const cur = normalizeContentTypes(form.value.content_types)
      if (cur.length) lastCustomContentTypes.value = cur
      form.value.content_types = []
      return
    }
    // custom
    const cur = Array.isArray(form.value.content_types) ? form.value.content_types : []
    if (cur.length === 0) {
      const next = lastCustomContentTypes.value.length ? [...lastCustomContentTypes.value] : [...contentPresets.common]
      form.value.content_types = next
    }
  },
})

const contentTypesSummary = computed(() => {
  if (contentTypeMode.value === 'all') return '当前：全类型（不过滤）'
  const cur = normalizeContentTypes(form.value.content_types)
  return `已选：${cur.length}/${allContentTypeKeys.length}`
})

function isContentTypeChecked(key: ContentTypeKey): boolean {
  const cur = normalizeContentTypes(form.value.content_types)
  return cur.includes(key)
}

function toggleContentType(key: ContentTypeKey, checked: boolean) {
  const cur = normalizeContentTypes(form.value.content_types)
  const set = new Set<ContentTypeKey>(cur)
  if (checked) set.add(key)
  else set.delete(key)

  const next = allContentTypeKeys.filter((k) => set.has(k))
  if (next.length === 0) {
    // keep custom mode stable: require at least one
    return
  }
  form.value.content_types = next
  lastCustomContentTypes.value = next
}

function onContentTypeTagChange(key: ContentTypeKey, ev: any) {
  toggleContentType(key, Boolean(ev))
}

const rules: FormRules = {
  name: [{ required: true, message: '请填写策略名称', trigger: 'blur' }],
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

            <el-col :xs="24">
              <el-form-item label="内容类型" prop="content_types">
                <div class="ct-block">
                  <div class="ct-head">
                    <div class="ct-left">
                      <el-radio-group v-model="contentTypeMode" size="small" class="ct-mode">
                        <el-radio-button label="all">全类型</el-radio-button>
                        <el-radio-button label="custom">自定义</el-radio-button>
                      </el-radio-group>
                      <div class="ct-meta">{{ contentTypesSummary }}</div>
                    </div>

                    <el-space v-if="contentTypeMode === 'custom'" size="small" class="ct-actions">
                      <el-button link type="primary" size="small" @click="applyContentPreset('common')">常用</el-button>
                      <el-button link type="primary" size="small" @click="applyContentPreset('media')">媒体</el-button>
                      <el-button link type="primary" size="small" @click="applyContentPreset('all')">全选</el-button>
                    </el-space>
                  </div>

                  <div v-if="contentTypeMode === 'custom'" class="ct-tags">
                    <el-check-tag
                      v-for="opt in contentTypeOptions"
                      :key="opt.key"
                      :checked="isContentTypeChecked(opt.key)"
                      @change="onContentTypeTagChange(opt.key, $event)"
                    >
                      <span class="ct-tag">
                        <i :class="opt.icon" />
                        {{ opt.label }}
                      </span>
                    </el-check-tag>
                    <div class="hint compact">提示：自定义会过滤未选类型；“全类型”包含其他/未知类型</div>
                  </div>

                  <div v-else class="ct-all">
                    <i class="ri-checkbox-multiple-line" />
                    <span>不做类型过滤（包含 other/未知类型）</span>
                  </div>
                </div>
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
            <span>处理开关</span>
          </div>

          <div class="switch-wrap">
            <el-switch v-model="form.keep_reply" active-text="保留回复" />
            <el-switch v-model="form.realtime" active-text="实时监控" />
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
