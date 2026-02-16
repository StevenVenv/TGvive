<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { createStrategy, deleteStrategy, getStrategies, updateStrategy, type Strategy } from '../api'
import StrategyForm, { type StrategyFormExpose, type StrategyFormModel } from '../components/strategy/StrategyForm.vue'

type TabKey = 'library' | 'create'
type DialogMode = 'edit' | 'debug'

const activeTab = ref<TabKey>('library')

const loading = ref(false)
const strategies = ref<Strategy[]>([])
const keyword = ref('')

const createSaving = ref(false)
const editSaving = ref(false)

const dialogVisible = ref(false)
const dialogMode = ref<DialogMode>('edit')

const createFormRef = ref<StrategyFormExpose | null>(null)
const editFormRef = ref<StrategyFormExpose | null>(null)

function emptyModel(): StrategyFormModel {
  return {
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
  }
}

const createModel = reactive<StrategyFormModel>(emptyModel())
const editModel = reactive<StrategyFormModel>(emptyModel())

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

function buildPayload(m: StrategyFormModel): Partial<Strategy> {
  return {
    name: (m.name || '').trim(),
    remark: (m.remark || '').trim(),

    clone_mode: Number(m.clone_mode || 3),
    content_types: Array.isArray(m.content_types) ? m.content_types : [],

    scope_type: Number(m.scope_type || 1),
    scope_value: (m.scope_value || '').trim(),
    history_order: Number(m.history_order || 1),

    keep_reply: Boolean(m.keep_reply),
    realtime: Boolean(m.realtime),
    clone_comment: Boolean(m.clone_comment),
    gpu_accel: Boolean(m.gpu_accel),
    change_md5: Boolean(m.change_md5),

    delay_min_ms: Number(m.delay_min_ms || 0),
    delay_max_ms: Number(m.delay_max_ms || 0),

    daily_limit: Number(m.daily_limit || 0),
    run_window: (m.run_window || '').trim(),
  }
}

function fillEdit(row: Strategy) {
  Object.assign(editModel, emptyModel(), {
    ID: Number(row.ID || 0),
    name: (row.name || '').trim(),
    remark: (row.remark || '').trim(),

    clone_mode: Number(row.clone_mode || 3),
    content_types: Array.isArray(row.content_types) ? [...row.content_types] : [],

    scope_type: Number(row.scope_type || 1),
    scope_value: (row.scope_value || '').trim(),
    history_order: Number(row.history_order || 1),

    keep_reply: Boolean(row.keep_reply),
    realtime: Boolean(row.realtime),
    clone_comment: Boolean(row.clone_comment),
    gpu_accel: Boolean(row.gpu_accel),
    change_md5: Boolean(row.change_md5),

    delay_min_ms: Number(row.delay_min_ms ?? 0),
    delay_max_ms: Number(row.delay_max_ms ?? 0),

    daily_limit: Number(row.daily_limit ?? 0),
    run_window: (row.run_window || '').trim(),
  })
}

function startCreate() {
  activeTab.value = 'create'
  Object.assign(createModel, emptyModel())
  void nextTick(() => createFormRef.value?.clearValidate?.())
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

async function submitCreate() {
  const ok = await createFormRef.value?.validate?.()
  if (!ok) return

  createSaving.value = true
  try {
    const created = await createStrategy(buildPayload(createModel))
    ElMessage.success(`策略已创建 #${created.ID}`)
    activeTab.value = 'library'
    await reload()
    Object.assign(createModel, emptyModel())
    void nextTick(() => createFormRef.value?.clearValidate?.())
  } catch (e: any) {
    ElMessage.error(e?.message || '创建失败')
  } finally {
    createSaving.value = false
  }
}

function resetCreate() {
  Object.assign(createModel, emptyModel())
  void nextTick(() => createFormRef.value?.clearValidate?.())
}

function openDialog(row: Strategy, mode: DialogMode = 'edit') {
  dialogMode.value = mode
  fillEdit(row)
  dialogVisible.value = true
}

const dialogTitle = computed(() => {
  const name = (editModel.name || '').trim()
  if (dialogMode.value === 'debug') return `调试策略: ${name || '#' + editModel.ID}`
  return `编辑策略: ${name || '#' + editModel.ID}`
})

watch(dialogVisible, (v) => {
  if (!v) return
  void nextTick(() => editFormRef.value?.clearValidate?.())
})

async function submitEdit() {
  const id = Number(editModel.ID || 0)
  if (!id) return
  const ok = await editFormRef.value?.validate?.()
  if (!ok) return

  editSaving.value = true
  try {
    const updated = await updateStrategy(id, buildPayload(editModel))
    ElMessage.success(`策略已保存 #${updated.ID}`)
    dialogVisible.value = false
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    editSaving.value = false
  }
}

async function remove(row?: Strategy) {
  const id = Number(row?.ID || editModel.ID || 0)
  if (!id) return
  const name = (row?.name || editModel.name || '').trim()

  try {
    await ElMessageBox.confirm(`确定删除策略「${name || '#' + id}」吗？`, '确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await deleteStrategy(id)
    ElMessage.success('已删除')
    if (dialogVisible.value) dialogVisible.value = false
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.message || '删除失败')
  }
}

onMounted(() => {
  void reload()
})
</script>

<template>
  <div class="strategy-manager">
    <el-tabs v-model="activeTab" class="bt-tabs">
      <el-tab-pane label="策略库" name="library">
        <el-card class="bt-card pane-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-flow-chart-line" />
                <span>策略库</span>
              </div>
              <div class="card-sub">共 {{ strategies.length }} 条</div>
            </div>
          </template>

          <div class="pane">
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

            <div class="table-body">
              <el-table :data="filteredStrategies" v-loading="loading" stripe height="100%" style="width: 100%">
                <el-table-column prop="ID" label="ID" width="90" />

                <el-table-column label="策略" min-width="240">
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

                <el-table-column label="操作" width="190" fixed="right">
                  <template #default="{ row }">
                    <el-space size="small">
                      <el-button link type="primary" @click="openDialog(row, 'edit')">编辑</el-button>
                      <el-button link type="success" @click="openDialog(row, 'debug')">调试</el-button>
                      <el-button link type="danger" @click="remove(row)">删除</el-button>
                    </el-space>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </el-card>
      </el-tab-pane>

      <el-tab-pane label="新建策略" name="create">
        <el-row :gutter="12">
          <el-col :xs="24" :lg="14">
            <el-card class="bt-card pane-card" shadow="never">
              <template #header>
                <div class="card-header">
                  <div class="card-title">
                    <i class="ri-add-circle-line" />
                    <span>创建新策略模版</span>
                  </div>
                  <div class="card-sub">Create New</div>
                </div>
              </template>

              <div class="pane form-host">
                <StrategyForm ref="createFormRef" v-model="createModel" :loading="createSaving">
                  <template #actions>
                    <el-space>
                      <el-button @click="resetCreate">
                        <i class="ri-refresh-line" />
                        重置
                      </el-button>
                      <el-button type="primary" :loading="createSaving" @click="submitCreate">
                        <i class="ri-add-line" />
                        创建
                      </el-button>
                    </el-space>
                  </template>
                </StrategyForm>
              </div>
            </el-card>
          </el-col>

          <el-col :xs="24" :lg="10">
            <el-card class="bt-card pane-card" shadow="never">
              <template #header>
                <div class="card-header">
                  <div class="card-title">
                    <i class="ri-bug-line" />
                    <span>调试 / 预览</span>
                  </div>
                  <div class="card-sub">Reserved</div>
                </div>
              </template>

              <div class="pane preview">
                <el-empty description="预留区域（未来接入调试面板 / 预览）" />
              </div>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="860px" class="bt-dialog" destroy-on-close>
      <div v-if="dialogMode === 'debug'" class="debug-tip">
        <el-text type="info">调试面板预留：后续可在此处展示策略命中情况、消息预览、过滤命中等。</el-text>
      </div>

      <StrategyForm ref="editFormRef" v-model="editModel" :loading="editSaving" :show-actions="false" />

      <template #footer>
        <el-space>
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="danger" :disabled="editSaving" @click="remove()">
            <i class="ri-delete-bin-6-line" />
            删除
          </el-button>
          <el-button type="primary" :loading="editSaving" @click="submitEdit">
            <i class="ri-save-3-line" />
            保存
          </el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.strategy-manager {
  width: 100%;
}

.bt-tabs :deep(.el-tabs__header) {
  margin: 0 0 12px;
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

.pane-card {
  display: flex;
  flex-direction: column;

  :deep(.el-card__body) {
    flex: 1;
    overflow: hidden;
  }
}

@media (min-width: 1200px) {
  .pane-card {
    height: calc(100vh - 160px);
  }
}

.pane {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.form-host {
  flex: 1;
  overflow: hidden;
}

.preview {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.table-body {
  flex: 1;
  overflow: hidden;
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

.debug-tip {
  margin-bottom: 10px;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(0, 0, 0, 0.18);
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
</style>
