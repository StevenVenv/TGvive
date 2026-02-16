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

const scopeValuePlaceholder = computed(() => {
  const t = Number(form.value.scope_type || 1)
  if (t === 2) return '例如：100'
  if (t === 3) return '例如：2025-01-01~2025-01-31'
  if (t === 4) return '例如：1000-2000'
  return '可留空'
})

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
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" class="form">
        <el-divider content-position="left">基础设置</el-divider>

        <el-row :gutter="12">
          <el-col :xs="24" :sm="14">
            <el-form-item label="策略名称" prop="name">
              <el-input v-model="form.name" placeholder="例如：极速转发 / 去重+转码" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="10">
            <el-form-item label="备注（可选）" prop="remark">
              <el-input v-model="form.remark" placeholder="用于团队协作/区分用途" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="克隆模式" prop="clone_mode">
          <el-radio-group v-model="form.clone_mode">
            <el-radio :label="1">转发</el-radio>
            <el-radio :label="2">发送</el-radio>
            <el-radio :label="3">下载上传</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="内容类型" prop="content_types">
          <el-checkbox-group v-model="form.content_types">
            <el-checkbox label="text">文本</el-checkbox>
            <el-checkbox label="image">图片</el-checkbox>
            <el-checkbox label="video">视频</el-checkbox>
            <el-checkbox label="audio">语音/音频</el-checkbox>
            <el-checkbox label="file">文件/贴纸</el-checkbox>
          </el-checkbox-group>
          <div class="hint">不选=全类型</div>
        </el-form-item>

        <el-row :gutter="12">
          <el-col :xs="24" :sm="10">
            <el-form-item label="消息范围" prop="scope_type">
              <el-select v-model="form.scope_type" style="width: 100%" popper-class="tgvive-dark-popper">
                <el-option :value="1" label="全部" />
                <el-option :value="2" label="最近 N 条" />
                <el-option :value="3" label="时间范围" />
                <el-option :value="4" label="ID 范围" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="14">
            <el-form-item label="范围参数" prop="scope_value">
              <el-input v-model="form.scope_value" :placeholder="scopeValuePlaceholder" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="历史方向" prop="history_order">
          <el-radio-group v-model="form.history_order">
            <el-radio :label="1">从旧到新</el-radio>
            <el-radio :label="2">从新到旧</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-divider content-position="left">风控设置</el-divider>

        <el-form-item label="随机延时 (ms)" prop="delay_max_ms">
          <div class="inline">
            <el-input-number v-model="form.delay_min_ms" :min="0" :step="100" controls-position="right" />
            <span class="sep">~</span>
            <el-input-number v-model="form.delay_max_ms" :min="0" :step="100" controls-position="right" />
          </div>
          <div class="hint">每处理完一条消息后随机 sleep</div>
        </el-form-item>

        <el-row :gutter="12">
          <el-col :xs="24" :sm="10">
            <el-form-item label="每日配额" prop="daily_limit">
              <el-input-number v-model="form.daily_limit" :min="0" :step="10" controls-position="right" />
              <div class="hint">0 = 不限制</div>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="14">
            <el-form-item label="运行窗口" prop="run_window">
              <el-input v-model="form.run_window" placeholder="例如：09:00-18:00（留空=全天）" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-divider content-position="left">处理设置</el-divider>

        <el-form-item label="处理开关">
          <el-space wrap>
            <el-switch v-model="form.keep_reply" active-text="保留回复" />
            <el-switch v-model="form.realtime" active-text="实时监控" />
            <el-switch v-model="form.clone_comment" active-text="克隆评论" />
            <el-switch v-model="form.gpu_accel" active-text="GPU 加速" />
            <el-switch v-model="form.change_md5" active-text="修改 MD5" />
          </el-space>
        </el-form-item>
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
  padding-right: 4px;
}

.form :deep(.el-form-item) {
  margin-bottom: 14px;
}

.hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.inline {
  display: flex;
  align-items: center;
}

.sep {
  padding: 0 8px;
  color: #909399;
}

.actions {
  display: flex;
  justify-content: flex-end;
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

