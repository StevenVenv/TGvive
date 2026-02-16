<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { createStrategy, deleteStrategy, getStrategies, updateStrategy, type Strategy } from '../api'

type StrategyForm = {
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
const selectedId = ref<number>(0)

const isCreate = computed(() => selectedId.value === 0)
const selectedStrategy = computed(() => strategies.value.find((s) => s.ID === selectedId.value) || null)

const form = reactive<StrategyForm>({
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

function resetForm() {
  form.name = ''
  form.remark = ''

  form.clone_mode = 3
  form.content_types = ['text', 'image', 'video', 'audio', 'file']

  form.scope_type = 1
  form.scope_value = ''
  form.history_order = 1

  form.keep_reply = false
  form.realtime = false
  form.clone_comment = false
  form.gpu_accel = false
  form.change_md5 = false

  form.delay_min_ms = 1000
  form.delay_max_ms = 3000

  form.daily_limit = 0
  form.run_window = ''
}

function applyFrom(s: Strategy) {
  form.name = (s.name || '').trim()
  form.remark = (s.remark || '').trim()

  form.clone_mode = Number(s.clone_mode || 3)
  form.content_types = Array.isArray(s.content_types) ? [...s.content_types] : []

  form.scope_type = Number(s.scope_type || 1)
  form.scope_value = (s.scope_value || '').trim()
  form.history_order = Number(s.history_order || 1)

  form.keep_reply = Boolean(s.keep_reply)
  form.realtime = Boolean(s.realtime)
  form.clone_comment = Boolean(s.clone_comment)
  form.gpu_accel = Boolean(s.gpu_accel)
  form.change_md5 = Boolean(s.change_md5)

  form.delay_min_ms = Number(s.delay_min_ms ?? 0)
  form.delay_max_ms = Number(s.delay_max_ms ?? 0)

  form.daily_limit = Number(s.daily_limit ?? 0)
  form.run_window = (s.run_window || '').trim()
}

function selectStrategy(s: Strategy) {
  if (!s?.ID) return
  selectedId.value = s.ID
  applyFrom(s)
}

function startCreate() {
  selectedId.value = 0
  resetForm()
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

async function reload() {
  loading.value = true
  try {
    const list = await getStrategies()
    strategies.value = list

	    const stillExists = selectedId.value > 0 && list.some((s) => s.ID === selectedId.value)
	    if (!stillExists) {
	      if (list.length > 0) {
	        const first = list[0]
	        if (first) selectStrategy(first)
	        else startCreate()
	      } else {
	        startCreate()
	      }
	    } else if (selectedId.value > 0) {
	      const cur = list.find((s) => s.ID === selectedId.value)
      if (cur) applyFrom(cur)
    }
  } catch (err: any) {
    ElMessage.error(err?.message || '加载策略失败')
    strategies.value = []
    startCreate()
  } finally {
    loading.value = false
  }
}

defineExpose({ reload })

function validate(): string | null {
  const name = (form.name || '').trim()
  if (!name) return '请填写策略名称'
  if (form.delay_min_ms < 0 || form.delay_max_ms < 0) return '延时不能为负数'
  if (form.delay_max_ms < form.delay_min_ms) return '最大延时必须大于等于最小延时'
  if (form.daily_limit < 0) return '每日配额不能为负数'
  return null
}

function buildPayload(): Partial<Strategy> {
  return {
    name: (form.name || '').trim(),
    remark: (form.remark || '').trim(),

    clone_mode: form.clone_mode,
    content_types: Array.isArray(form.content_types) ? form.content_types : [],

    scope_type: form.scope_type,
    scope_value: (form.scope_value || '').trim(),
    history_order: form.history_order,

    keep_reply: form.keep_reply,
    realtime: form.realtime,
    clone_comment: form.clone_comment,
    gpu_accel: form.gpu_accel,
    change_md5: form.change_md5,

    delay_min_ms: Number(form.delay_min_ms || 0),
    delay_max_ms: Number(form.delay_max_ms || 0),

    daily_limit: Number(form.daily_limit || 0),
    run_window: (form.run_window || '').trim(),
  }
}

async function save() {
  const err = validate()
  if (err) {
    ElMessage.warning(err)
    return
  }

  saving.value = true
  try {
    const payload = buildPayload()
    if (isCreate.value) {
      const created = await createStrategy(payload)
      ElMessage.success(`策略已创建 #${created.ID}`)
      selectedId.value = created.ID
    } else {
      const updated = await updateStrategy(selectedId.value, payload)
      ElMessage.success(`策略已保存 #${updated.ID}`)
      selectedId.value = updated.ID
    }
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function remove(s?: Strategy | null) {
  const target = s || selectedStrategy.value
  if (!target?.ID) return

  try {
    await ElMessageBox.confirm(`确定删除策略「${target.name}」吗？`, '确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await deleteStrategy(target.ID)
    ElMessage.success('已删除')
    if (selectedId.value === target.ID) selectedId.value = 0
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.message || '删除失败')
  }
}

function resetToSelected() {
  const s = selectedStrategy.value
  if (!s) {
    startCreate()
    return
  }
  applyFrom(s)
}

onMounted(() => {
  void reload()
})
</script>

<template>
  <div class="strategy-manager">
    <el-row :gutter="12">
      <el-col :xs="24" :lg="11">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-flow-chart-line" />
                <span>策略库</span>
              </div>
              <div class="card-sub">Strategy Templates</div>
            </div>
          </template>

          <div class="toolbar">
            <div class="toolbar-left">
              <el-button type="primary" @click="startCreate">
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

          <el-table
            :data="filteredStrategies"
            v-loading="loading"
            stripe
            highlight-current-row
            row-key="ID"
            :current-row-key="selectedId || undefined"
            style="width: 100%"
            @row-click="selectStrategy"
          >
            <el-table-column prop="ID" label="ID" width="80" />

            <el-table-column label="策略" min-width="220">
              <template #default="{ row }">
                <div class="st-name">{{ row.name }}</div>
                <div v-if="row.remark" class="st-remark">{{ row.remark }}</div>
              </template>
            </el-table-column>

            <el-table-column label="标签" min-width="360">
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
                <el-space>
                  <el-button size="small" @click.stop="selectStrategy(row)">编辑</el-button>
                  <el-button size="small" type="danger" @click.stop="remove(row)">删除</el-button>
                </el-space>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="13">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-edit-box-line" />
                <span>策略编辑器</span>
              </div>
              <div class="card-sub">
                <span v-if="isCreate">New</span>
                <span v-else>#{{ selectedId }}</span>
              </div>
            </div>
          </template>

          <div class="editor-top">
            <div class="editor-left">
              <div class="editor-title">{{ form.name?.trim() ? form.name : '未命名策略' }}</div>
              <div class="editor-sub muted">
                <span>{{ cloneModeLabel(form.clone_mode) }}</span>
                <span class="dot">·</span>
                <span>{{ form.realtime ? '实时监控' : '历史搬运' }}</span>
                <span class="dot">·</span>
                <span>{{ form.gpu_accel ? 'GPU 开启' : 'GPU 关闭' }}</span>
              </div>
            </div>
            <div class="editor-right">
              <el-space>
                <el-button @click="resetToSelected">
                  <i class="ri-arrow-go-back-line" />
                  重置
                </el-button>
                <el-button v-if="!isCreate" type="danger" @click="remove()">
                  <i class="ri-delete-bin-6-line" />
                  删除
                </el-button>
                <el-button type="primary" :loading="saving" @click="save">
                  <i class="ri-save-3-line" />
                  保存
                </el-button>
              </el-space>
            </div>
          </div>

          <div class="split-line" />

          <el-form label-position="top" class="form">
            <el-row :gutter="12">
              <el-col :xs="24" :sm="14">
                <el-form-item label="策略名称">
                  <el-input v-model="form.name" placeholder="例如：极速转发 / 去重+转码" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="10">
                <el-form-item label="备注（可选）">
                  <el-input v-model="form.remark" placeholder="用于团队协作/区分用途" />
                </el-form-item>
              </el-col>
            </el-row>

            <el-divider />

            <el-form-item label="克隆模式">
              <el-radio-group v-model="form.clone_mode">
                <el-radio :label="1">转发</el-radio>
                <el-radio :label="2">发送</el-radio>
                <el-radio :label="3">下载上传</el-radio>
              </el-radio-group>
            </el-form-item>

            <el-form-item label="内容类型">
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
                <el-form-item label="消息范围">
                  <el-select v-model="form.scope_type" style="width: 100%">
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
                    v-model="form.scope_value"
                    :placeholder="
                      form.scope_type === 2
                        ? '例如：100'
                        : form.scope_type === 3
                          ? '例如：2025-01-01~2025-01-31'
                          : form.scope_type === 4
                            ? '例如：1000-2000'
                            : '可留空'
                    "
                  />
                </el-form-item>
              </el-col>
            </el-row>

            <el-form-item label="历史方向">
              <el-radio-group v-model="form.history_order">
                <el-radio :label="1">从旧到新</el-radio>
                <el-radio :label="2">从新到旧</el-radio>
              </el-radio-group>
            </el-form-item>

            <el-divider />

            <el-form-item label="随机延时 (ms)">
              <div class="inline">
                <el-input-number v-model="form.delay_min_ms" :min="0" :step="100" controls-position="right" />
                <span class="sep">~</span>
                <el-input-number v-model="form.delay_max_ms" :min="0" :step="100" controls-position="right" />
              </div>
              <div class="hint">每处理完一条消息后随机 sleep</div>
            </el-form-item>

            <el-row :gutter="12">
              <el-col :xs="24" :sm="10">
                <el-form-item label="每日配额">
                  <el-input-number v-model="form.daily_limit" :min="0" :step="10" controls-position="right" />
                  <div class="hint">0 = 不限制</div>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="14">
                <el-form-item label="运行窗口">
                  <el-input v-model="form.run_window" placeholder="例如：09:00-18:00（留空=全天）" />
                </el-form-item>
              </el-col>
            </el-row>

            <el-divider />

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
        </el-card>
      </el-col>
    </el-row>
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

.muted {
  color: var(--el-text-color-secondary);
}

.dot {
  opacity: 0.7;
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
  width: 240px;
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

.editor-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.editor-left {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.editor-title {
  font-weight: 800;
  letter-spacing: 0.2px;
}

.editor-sub {
  font-size: 12px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}

.split-line {
  height: 1px;
  background: rgba(255, 255, 255, 0.06);
  margin: 12px 0;
}

.inline {
  display: flex;
  align-items: center;
}

.sep {
  padding: 0 8px;
  color: #909399;
}

.hint {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
</style>
