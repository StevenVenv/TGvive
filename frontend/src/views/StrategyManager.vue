<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance } from 'element-plus'

import { createStrategy, deleteStrategy, getStrategies, updateStrategy, type Strategy } from '../api'

type StrategyForm = {
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

const loading = ref(false)
const saving = ref(false)

const keyword = ref('')
const strategies = ref<Strategy[]>([])

const dialogVisible = ref(false)
const formRef = ref<FormInstance>()

const currentStrategy = reactive<StrategyForm>({
  ID: 0,
  name: '',
  remark: '',

  clone_mode: 3,
  content_types: ['text', 'image', 'video', 'audio', 'file'],

  scope_type: 1,
  scope_value: '',
  history_order: 1,

  keep_reply: false,
  realtime: false,
  clone_comment: false,
  gpu_accel: false,
  change_md5: false,

  delay_min_ms: 1000,
  delay_max_ms: 3000,

  daily_limit: 0,
  run_window: '',
})

const isEdit = computed(() => currentStrategy.ID > 0)
const dialogTitle = computed(() => (isEdit.value ? `编辑策略 #${currentStrategy.ID}` : '新建策略'))

function cloneModeLabel(v: number): string {
  if (v === 1) return '转发'
  if (v === 2) return '发送'
  return '下载上传'
}

function summarizeTags(s: Strategy): string[] {
  const tags: string[] = []
  tags.push(cloneModeLabel(Number(s.clone_mode || 3)))

  const ct = Array.isArray(s.content_types) ? s.content_types : []
  tags.push(ct.length ? `类型:${ct.join(',')}` : '类型:全部')

  if (s.realtime) tags.push('实时')
  if (s.keep_reply) tags.push('保留回复')
  if (s.clone_comment) tags.push('克隆评论')
  if (s.gpu_accel) tags.push('GPU')
  if (s.change_md5) tags.push('改MD5')

  const dmin = Number(s.delay_min_ms ?? 0)
  const dmax = Number(s.delay_max_ms ?? 0)
  if (dmin > 0 || dmax > 0) tags.push(`延时:${dmin}~${Math.max(dmax, dmin)}ms`)

  const limit = Number(s.daily_limit ?? 0)
  if (limit > 0) tags.push(`配额:${limit}/天`)

  const win = (s.run_window || '').trim()
  if (win) tags.push(`窗口:${win}`)

  return tags.slice(0, 8)
}

const filteredStrategies = computed(() => {
  const q = (keyword.value || '').trim().toLowerCase()
  if (!q) return strategies.value
  return strategies.value.filter((s) => {
    const name = (s.name || '').toLowerCase()
    const remark = (s.remark || '').toLowerCase()
    return name.includes(q) || remark.includes(q)
  })
})

function resetCurrent() {
  currentStrategy.ID = 0
  currentStrategy.name = ''
  currentStrategy.remark = ''

  currentStrategy.clone_mode = 3
  currentStrategy.content_types = ['text', 'image', 'video', 'audio', 'file']

  currentStrategy.scope_type = 1
  currentStrategy.scope_value = ''
  currentStrategy.history_order = 1

  currentStrategy.keep_reply = false
  currentStrategy.realtime = false
  currentStrategy.clone_comment = false
  currentStrategy.gpu_accel = false
  currentStrategy.change_md5 = false

  currentStrategy.delay_min_ms = 1000
  currentStrategy.delay_max_ms = 3000

  currentStrategy.daily_limit = 0
  currentStrategy.run_window = ''
}

function fillFromRow(row: Strategy) {
  currentStrategy.ID = Number(row.ID || 0)
  currentStrategy.name = (row.name || '').trim()
  currentStrategy.remark = (row.remark || '').trim()

  currentStrategy.clone_mode = Number(row.clone_mode || 3)
  currentStrategy.content_types = Array.isArray(row.content_types) ? [...row.content_types] : []

  currentStrategy.scope_type = Number(row.scope_type || 1)
  currentStrategy.scope_value = (row.scope_value || '').trim()
  currentStrategy.history_order = Number(row.history_order || 1)

  currentStrategy.keep_reply = Boolean(row.keep_reply)
  currentStrategy.realtime = Boolean(row.realtime)
  currentStrategy.clone_comment = Boolean(row.clone_comment)
  currentStrategy.gpu_accel = Boolean(row.gpu_accel)
  currentStrategy.change_md5 = Boolean(row.change_md5)

  currentStrategy.delay_min_ms = Number(row.delay_min_ms ?? 0)
  currentStrategy.delay_max_ms = Number(row.delay_max_ms ?? 0)

  currentStrategy.daily_limit = Number(row.daily_limit ?? 0)
  currentStrategy.run_window = (row.run_window || '').trim()
}

function openCreate() {
  resetCurrent()
  dialogVisible.value = true
}

function openEdit(row: Strategy) {
  resetCurrent()
  // Deep-copy row fields into currentStrategy (avoid shared references like arrays).
  fillFromRow(row)
  dialogVisible.value = true
}

function validate(): string | null {
  const name = (currentStrategy.name || '').trim()
  if (!name) return '请填写策略名称'
  if (currentStrategy.delay_min_ms < 0 || currentStrategy.delay_max_ms < 0) return '延时不能为负数'
  if (currentStrategy.delay_max_ms < currentStrategy.delay_min_ms) return '最大延时必须大于等于最小延时'
  if (currentStrategy.daily_limit < 0) return '每日配额不能为负数'
  return null
}

function buildPayload(): Partial<Strategy> {
  return {
    name: (currentStrategy.name || '').trim(),
    remark: (currentStrategy.remark || '').trim(),

    clone_mode: currentStrategy.clone_mode,
    content_types: Array.isArray(currentStrategy.content_types) ? currentStrategy.content_types : [],

    scope_type: currentStrategy.scope_type,
    scope_value: (currentStrategy.scope_value || '').trim(),
    history_order: currentStrategy.history_order,

    keep_reply: currentStrategy.keep_reply,
    realtime: currentStrategy.realtime,
    clone_comment: currentStrategy.clone_comment,
    gpu_accel: currentStrategy.gpu_accel,
    change_md5: currentStrategy.change_md5,

    delay_min_ms: Number(currentStrategy.delay_min_ms || 0),
    delay_max_ms: Number(currentStrategy.delay_max_ms || 0),

    daily_limit: Number(currentStrategy.daily_limit || 0),
    run_window: (currentStrategy.run_window || '').trim(),
  }
}

async function reload() {
  loading.value = true
  try {
    strategies.value = await getStrategies()
  } catch (err: any) {
    ElMessage.error(err?.message || '加载策略失败')
    strategies.value = []
  } finally {
    loading.value = false
  }
}

defineExpose({ reload })

async function save() {
  const err = validate()
  if (err) {
    ElMessage.warning(err)
    return
  }

  saving.value = true
  try {
    const payload = buildPayload()
    if (isEdit.value) {
      const updated = await updateStrategy(currentStrategy.ID, payload)
      ElMessage.success(`策略已保存 #${updated.ID}`)
    } else {
      const created = await createStrategy(payload)
      ElMessage.success(`策略已创建 #${created.ID}`)
    }
    dialogVisible.value = false
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function remove(row: Strategy) {
  if (!row?.ID) return

  try {
    await ElMessageBox.confirm(`确定删除策略「${row.name}」吗？`, '确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await deleteStrategy(row.ID)
    ElMessage.success('已删除')
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.message || '删除失败')
  }
}

watch(dialogVisible, (v) => {
  if (!v) {
    formRef.value?.clearValidate?.()
  }
})

onMounted(() => {
  void reload()
})
</script>

<template>
  <div class="strategy-manager">
    <el-card class="bt-card" shadow="never">
      <template #header>
        <div class="card-header">
          <div class="card-title">
            <i class="ri-flow-chart-line" />
            <span>策略管理</span>
          </div>
          <div class="card-sub">Strategy Templates</div>
        </div>
      </template>

      <div class="toolbar">
        <div class="toolbar-left">
          <el-button type="primary" @click="openCreate">
            <i class="ri-add-line" />
            新建策略
          </el-button>
          <el-button @click="reload" :loading="loading">
            <i class="ri-refresh-line" />
            刷新
          </el-button>
        </div>
        <div class="toolbar-right">
          <el-input v-model="keyword" placeholder="搜索策略名称/备注" clearable class="search" />
        </div>
      </div>

      <el-table :data="filteredStrategies" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="ID" label="ID" width="90" />

        <el-table-column label="策略" min-width="260">
          <template #default="{ row }">
            <div class="st-name">{{ row.name }}</div>
            <div v-if="row.remark" class="st-remark">{{ row.remark }}</div>
          </template>
        </el-table-column>

        <el-table-column label="标签" min-width="420">
          <template #default="{ row }">
            <el-space wrap>
              <el-tag v-for="t in summarizeTags(row)" :key="t" size="small" type="info" effect="plain">
                {{ t }}
              </el-tag>
            </el-space>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-space size="small">
              <el-button link type="primary" @click.stop="openEdit(row)">编辑</el-button>
              <el-button link type="danger" @click.stop="remove(row)">删除</el-button>
            </el-space>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="780px" class="bt-dialog" destroy-on-close>
      <div v-loading="saving" class="dialog-body">
        <el-form ref="formRef" :model="currentStrategy" label-position="top" class="form">
          <el-row :gutter="12">
            <el-col :xs="24" :sm="14">
              <el-form-item label="策略名称">
                <el-input v-model="currentStrategy.name" placeholder="例如：极速转发 / 去重+转码" />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="10">
              <el-form-item label="备注（可选）">
                <el-input v-model="currentStrategy.remark" placeholder="用于团队协作/区分用途" />
              </el-form-item>
            </el-col>
          </el-row>

          <el-divider />

          <el-form-item label="克隆模式">
            <el-radio-group v-model="currentStrategy.clone_mode">
              <el-radio :label="1">转发</el-radio>
              <el-radio :label="2">发送</el-radio>
              <el-radio :label="3">下载上传</el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item label="内容类型">
            <el-checkbox-group v-model="currentStrategy.content_types">
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
              <el-form-item label="消息范围">
                <el-select v-model="currentStrategy.scope_type" style="width: 100%" popper-class="tgvive-dark-popper">
                  <el-option :value="1" label="全部" />
                  <el-option :value="2" label="最近 N 条" />
                  <el-option :value="3" label="时间范围" />
                  <el-option :value="4" label="ID 范围" />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="14">
              <el-form-item label="范围参数">
                <el-input
                  v-model="currentStrategy.scope_value"
                  :placeholder="
                    currentStrategy.scope_type === 2
                      ? '例如：100'
                      : currentStrategy.scope_type === 3
                        ? '例如：2025-01-01~2025-01-31'
                        : currentStrategy.scope_type === 4
                          ? '例如：1000-2000'
                          : '可留空'
                  "
                />
              </el-form-item>
            </el-col>
          </el-row>

          <el-form-item label="历史方向">
            <el-radio-group v-model="currentStrategy.history_order">
              <el-radio :label="1">从旧到新</el-radio>
              <el-radio :label="2">从新到旧</el-radio>
            </el-radio-group>
          </el-form-item>

          <el-divider />

          <el-form-item label="随机延时 (ms)">
            <div class="inline">
              <el-input-number v-model="currentStrategy.delay_min_ms" :min="0" :step="100" controls-position="right" />
              <span class="sep">~</span>
              <el-input-number v-model="currentStrategy.delay_max_ms" :min="0" :step="100" controls-position="right" />
            </div>
            <div class="hint">每处理完一条消息后随机 sleep</div>
          </el-form-item>

          <el-row :gutter="12">
            <el-col :xs="24" :sm="10">
              <el-form-item label="每日配额">
                <el-input-number v-model="currentStrategy.daily_limit" :min="0" :step="10" controls-position="right" />
                <div class="hint">0 = 不限制</div>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="14">
              <el-form-item label="运行窗口">
                <el-input v-model="currentStrategy.run_window" placeholder="例如：09:00-18:00（留空=全天）" />
              </el-form-item>
            </el-col>
          </el-row>

          <el-divider />

          <el-form-item label="处理开关">
            <el-space wrap>
              <el-switch v-model="currentStrategy.keep_reply" active-text="保留回复" />
              <el-switch v-model="currentStrategy.realtime" active-text="实时监控" />
              <el-switch v-model="currentStrategy.clone_comment" active-text="克隆评论" />
              <el-switch v-model="currentStrategy.gpu_accel" active-text="GPU 加速" />
              <el-switch v-model="currentStrategy.change_md5" active-text="修改 MD5" />
            </el-space>
          </el-form-item>
        </el-form>
      </div>

      <template #footer>
        <el-space>
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.strategy-manager {
  width: 100%;
}

.bt-card {
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.02);

  :deep(.el-card__header) {
    padding: 12px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
    background: rgba(0, 0, 0, 0.18);
  }

  :deep(.el-card__body) {
    padding: 14px;
  }
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

  i {
    font-size: 16px;
    color: #409eff;
  }
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
  gap: 10px;
  margin-bottom: 12px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.search {
  width: 260px;
}

.st-name {
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.st-remark {
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.dialog-body {
  padding: 6px 2px 0;
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
:global(html.dark) .strategy-manager :deep(.el-input__inner) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .strategy-manager :deep(.el-input__inner::placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .strategy-manager :deep(.el-input__wrapper) {
  background: rgba(255, 255, 255, 0.02);
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08) inset;
}

:global(html.dark) .strategy-manager :deep(.el-select__wrapper) {
  background: rgba(255, 255, 255, 0.02);
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08) inset;
}

:global(html.dark) .strategy-manager :deep(.el-select__selected-item) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .strategy-manager :deep(.el-select__placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .strategy-manager :deep(.el-select__input) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .strategy-manager :deep(.el-select__input::placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .strategy-manager :deep(.el-select__caret) {
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
