<script setup lang="ts">
import { computed, ref } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import RegexCheatSheet from './RegexCheatSheet.vue'

export type ReplaceRule = {
  from: string
  to: string
}

export type KeywordRule = {
  content: string
  is_regex: boolean
}

export type KeywordFormModel = {
  ID: number
  name: string
  remark: string
  block_words: KeywordRule[]
  allow_words: KeywordRule[]
  replace_rules: ReplaceRule[]
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
const regexHelpVisible = ref(false)

const rules: FormRules = {
  name: [{ required: true, message: '请填写方案名称', trigger: 'blur' }],
}

function addBlockRule() {
  if (!Array.isArray(form.value.block_words)) form.value.block_words = []
  form.value.block_words.push({ content: '', is_regex: false })
}

function removeBlockRule(idx: number) {
  const list = Array.isArray(form.value.block_words) ? form.value.block_words : []
  if (idx < 0 || idx >= list.length) return
  list.splice(idx, 1)
}

function addAllowRule() {
  if (!Array.isArray(form.value.allow_words)) form.value.allow_words = []
  form.value.allow_words.push({ content: '', is_regex: false })
}

function removeAllowRule(idx: number) {
  const list = Array.isArray(form.value.allow_words) ? form.value.allow_words : []
  if (idx < 0 || idx >= list.length) return
  list.splice(idx, 1)
}

function onBlockEnter(idx: number) {
  const list = Array.isArray(form.value.block_words) ? form.value.block_words : []
  if (idx !== list.length - 1) return
  const last = list[idx]
  if (!String(last?.content || '').trim()) return
  addBlockRule()
}

function onAllowEnter(idx: number) {
  const list = Array.isArray(form.value.allow_words) ? form.value.allow_words : []
  if (idx !== list.length - 1) return
  const last = list[idx]
  if (!String(last?.content || '').trim()) return
  addAllowRule()
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

          <el-col :xs="24" :sm="12" :md="16">
            <el-form-item label="备注 (Remark)" prop="remark">
              <el-input v-model="form.remark" placeholder="可选：说明用途/范围" />
            </el-form-item>
          </el-col>
        </el-row>

        <div class="rule-toolbar">
          <div class="rule-toolbar-left">
            <div class="hint compact">提示：每条规则都可以选择“文本/正则”</div>
          </div>
          <el-button type="primary" link class="regex-help-btn" @click="regexHelpVisible = true">
            <i class="ri-question-line" />
            正则帮助
          </el-button>
        </div>

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
                    <span>屏蔽规则（不转发）</span>
                  </span>
                </template>
                <div class="rule-list">
                  <div v-if="(form.block_words?.length || 0) === 0" class="rules-empty">
                    <el-text type="info">暂无屏蔽规则（可选）</el-text>
                  </div>

                  <div v-else class="rule-rows">
                    <div v-for="(r, idx) in form.block_words" :key="idx" class="rule-row">
                      <el-input
                        v-model="r.content"
                        clearable
                        placeholder="输入关键词或正则..."
                        @keyup.enter="onBlockEnter(idx)"
                      />
                      <el-switch
                        v-model="r.is_regex"
                        inline-prompt
                        active-text="正则"
                        inactive-text="文本"
                        style="--el-switch-on-color: #e6a23c"
                      />
                      <el-button link type="danger" class="rule-del" @click="removeBlockRule(idx)">
                        <i class="ri-delete-bin-line" />
                      </el-button>
                    </div>
                  </div>

                  <el-button type="primary" plain size="small" class="add-btn" @click="addBlockRule">
                    <i class="ri-add-line" />
                    添加规则
                  </el-button>
                </div>
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
                    <span>白名单规则（只转发）</span>
                    <el-tooltip
                      content="设置后，只有包含这些词的消息才会被搬运"
                      placement="top"
                      :show-after="200"
                    >
                      <i class="ri-question-line tip-icon" />
                    </el-tooltip>
                  </span>
                </template>
                <div class="rule-list">
                  <div v-if="(form.allow_words?.length || 0) === 0" class="rules-empty">
                    <el-text type="info">暂无白名单规则（可选）</el-text>
                  </div>

                  <div v-else class="rule-rows">
                    <div v-for="(r, idx) in form.allow_words" :key="idx" class="rule-row">
                      <el-input
                        v-model="r.content"
                        clearable
                        placeholder="输入关键词或正则..."
                        @keyup.enter="onAllowEnter(idx)"
                      />
                      <el-switch
                        v-model="r.is_regex"
                        inline-prompt
                        active-text="正则"
                        inactive-text="文本"
                        style="--el-switch-on-color: #e6a23c"
                      />
                      <el-button link type="danger" class="rule-del" @click="removeAllowRule(idx)">
                        <i class="ri-delete-bin-line" />
                      </el-button>
                    </div>
                  </div>

                  <el-button type="primary" plain size="small" class="add-btn" @click="addAllowRule">
                    <i class="ri-add-line" />
                    添加规则
                  </el-button>
                </div>
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

    <RegexCheatSheet v-model="regexHelpVisible" />
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

.rule-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin: 2px 0 8px;
}

.rule-toolbar-left {
  min-width: 0;
}

.regex-help-btn {
  flex: none;
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

.rule-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.rule-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.rule-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.rule-row :deep(.el-input) {
  flex: 1;
}

.rule-row :deep(.el-switch) {
  flex: none;
}

.rule-del {
  flex: none;
}

.add-btn {
  width: 100%;
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
</style>
