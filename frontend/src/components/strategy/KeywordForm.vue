<script setup lang="ts">
import { computed, ref } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'

export type ReplaceRule = {
  from: string
  to: string
}

export type KeywordFormModel = {
  ID: number
  name: string
  remark: string
  block_words: string[]
  allow_words: string[]
  replace_rules: ReplaceRule[]
  use_regex: boolean
}

export type KeywordFormExpose = {
  validate: () => Promise<boolean>
  clearValidate: () => void
}

const props = withDefaults(
  defineProps<{
    modelValue: KeywordFormModel
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
  (e: 'update:modelValue', v: KeywordFormModel): void
  (e: 'submit'): void
  (e: 'cancel'): void
}>()

const formRef = ref<FormInstance>()
const form = computed(() => props.modelValue)
const activePane = ref<'block' | 'allow' | 'replace'>('block')

const rules: FormRules = {
  name: [{ required: true, message: '请填写方案名称', trigger: 'blur' }],
}

function addRule() {
  if (!Array.isArray(form.value.replace_rules)) form.value.replace_rules = []
  form.value.replace_rules.push({ from: '', to: '' })
}

function removeRule(idx: number) {
  const list = Array.isArray(form.value.replace_rules) ? form.value.replace_rules : []
  if (idx < 0 || idx >= list.length) return
  list.splice(idx, 1)
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

defineExpose<KeywordFormExpose>({
  validate,
  clearValidate,
})
</script>

<template>
  <div class="keyword-form">
    <div class="form-scroll" v-loading="loading">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" size="small" class="form">
        <el-row :gutter="12" class="top-row">
          <el-col :xs="24" :sm="12" :md="8">
            <el-form-item label="方案名称" prop="name">
              <el-input v-model="form.name" placeholder="例如：广告过滤 / 招聘白名单" />
            </el-form-item>
          </el-col>

          <el-col :xs="24" :sm="12" :md="12">
            <el-form-item label="备注 (Remark)" prop="remark">
              <el-input v-model="form.remark" placeholder="可选：说明用途/范围" />
            </el-form-item>
          </el-col>

          <el-col :xs="24" :sm="12" :md="4">
            <el-form-item label="匹配模式" prop="use_regex">
              <el-switch v-model="form.use_regex" active-text="Regex" inactive-text="Plain" />
              <div class="hint compact">开启后：屏蔽/白名单/替换按正则执行</div>
            </el-form-item>
          </el-col>
        </el-row>

        <el-tabs v-model="activePane" class="rule-tabs">
          <el-tab-pane name="block">
            <template #label>
              <span class="tab-label">
                <i class="ri-forbid-2-line" />
                <span>屏蔽词</span>
              </span>
            </template>
            <div class="tab-pane">
              <el-form-item prop="block_words">
                <template #label>
                  <span class="label-with-tip">
                    <i class="ri-forbid-2-line" />
                    <span>屏蔽词（不转发）</span>
                  </span>
                </template>
                <el-select
                  v-model="form.block_words"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  collapse-tags
                  collapse-tags-tooltip
                  class="ctrl"
                  placeholder="输入回车生成标签，例如：赌场 / 加微信"
                  popper-class="tgvive-dark-popper"
                />
                <div class="hint">命中任意屏蔽词：该消息将被跳过</div>
              </el-form-item>
            </div>
          </el-tab-pane>

          <el-tab-pane name="allow">
            <template #label>
              <span class="tab-label">
                <i class="ri-shield-check-line" />
                <span>白名单</span>
              </span>
            </template>
            <div class="tab-pane">
              <el-form-item prop="allow_words">
                <template #label>
                  <span class="label-with-tip">
                    <i class="ri-shield-check-line" />
                    <span>白名单（只转发）</span>
                    <el-tooltip
                      content="设置后，只有包含这些词的消息才会被搬运"
                      placement="top"
                      :show-after="200"
                    >
                      <i class="ri-question-line tip-icon" />
                    </el-tooltip>
                  </span>
                </template>
                <el-select
                  v-model="form.allow_words"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  collapse-tags
                  collapse-tags-tooltip
                  class="ctrl"
                  placeholder="可选：输入回车生成标签"
                  popper-class="tgvive-dark-popper"
                />
                <div class="hint">若白名单非空：消息必须命中至少一个词才会搬运</div>
              </el-form-item>
            </div>
          </el-tab-pane>

          <el-tab-pane name="replace">
            <template #label>
              <span class="tab-label">
                <i class="ri-exchange-2-line" />
                <span>替换规则</span>
              </span>
            </template>
            <div class="tab-pane">
              <div class="rules">
                <div class="rules-head">
                  <div class="rules-title">from → to</div>
                  <el-button type="primary" plain size="small" @click="addRule">
                    <i class="ri-add-line" />
                    添加规则
                  </el-button>
                </div>

                <div v-if="(form.replace_rules?.length || 0) === 0" class="rules-empty">
                  <el-text type="info">暂无替换规则（可选）</el-text>
                </div>

                <div v-else class="rules-grid">
                  <div class="rules-row rules-row--head">
                    <div class="cell muted">原文本 (from)</div>
                    <div class="cell muted">新文本 (to)</div>
                    <div class="cell muted">操作</div>
                  </div>

                  <div v-for="(r, idx) in form.replace_rules" :key="idx" class="rules-row">
                    <div class="cell">
                      <el-input v-model="r.from" placeholder="例如：加微信" />
                    </div>
                    <div class="cell">
                      <el-input v-model="r.to" placeholder="例如：**" />
                    </div>
                    <div class="cell action">
                      <el-button link type="danger" @click="removeRule(idx)">删除</el-button>
                    </div>
                  </div>
                </div>

                <div class="hint compact">提示：仅对文字内容（含媒体标题）生效；不会修改图片/视频文件本身</div>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>
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
.keyword-form {
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
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.hint.compact {
  margin-top: 6px;
}

.muted {
  color: var(--el-text-color-secondary);
}

.actions {
  display: flex;
  justify-content: flex-end;
}

.top-row {
  margin-bottom: 4px;
}

.rule-tabs :deep(.el-tabs__header) {
  margin: 0 0 10px;
}

.rule-tabs :deep(.el-tabs__item) {
  height: 34px;
  line-height: 34px;
  padding: 0 12px;
}

.tab-pane {
  border: 1px solid #363637;
  border-radius: 4px;
  background: #1e1e1e;
  padding: 12px;
}

.label-with-tip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.tab-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.tab-label i {
  font-size: 14px;
  color: rgba(191, 203, 217, 0.85);
}

.tip-icon {
  font-size: 14px;
  color: rgba(191, 203, 217, 0.75);
  cursor: help;
}

.ctrl {
  width: 100%;
}

.rules {
  width: 100%;
}

.rules-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}

.rules-title {
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.rules-empty {
  padding: 10px 0;
}

.rules-grid {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.rules-row {
  display: grid;
  grid-template-columns: 1fr 1fr 80px;
  gap: 10px;
  align-items: center;
}

.rules-row--head {
  font-size: 12px;
  padding-bottom: 6px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.action {
  display: flex;
  justify-content: flex-end;
}

/* Fix: select text/placeholder in dark mode */
:global(html.dark) .keyword-form :deep(.el-select__wrapper) {
  background: rgba(255, 255, 255, 0.02);
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08) inset;
}

:global(html.dark) .keyword-form :deep(.el-select__selected-item) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .keyword-form :deep(.el-select__placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .keyword-form :deep(.el-select__input) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .keyword-form :deep(.el-select__input::placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .keyword-form :deep(.el-select__caret) {
  color: rgba(191, 203, 217, 0.75);
}
</style>
