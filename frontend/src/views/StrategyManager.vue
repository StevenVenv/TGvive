<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import {
  createKeywordProfile,
  createStrategy,
  deleteKeywordProfile,
  deleteStrategy,
  getKeywordProfiles,
  getStrategies,
  updateKeywordProfile,
  updateStrategy,
  type KeywordProfile,
  type Strategy,
} from '../api'
import KeywordForm, { type KeywordFormExpose, type KeywordFormModel } from '../components/strategy/KeywordForm.vue'
import StrategyForm, { type ScheduleRule, type StrategyFormExpose, type StrategyFormModel } from '../components/strategy/StrategyForm.vue'

type TabKey = 'library' | 'create' | 'keywords'
type DialogMode = 'edit' | 'debug'
type KeywordDialogMode = 'create' | 'edit'

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

const kwLoading = ref(false)
const kwProfiles = ref<KeywordProfile[]>([])
const kwSearch = ref('')

const kwDialogVisible = ref(false)
const kwDialogMode = ref<KeywordDialogMode>('create')
const kwSaving = ref(false)
const kwFormRef = ref<KeywordFormExpose | null>(null)

function emptyModel(): StrategyFormModel {
  return {
    ID: 0,
    name: '',
    remark: '',

    clone_mode: 3,
    // empty = 全类型（不做过滤）
    allowed_types: [],
    content_types: [],

    scope_type: 1,
    scope_value: '',
    history_order: 1,
    poll_interval: 0,
    enable_realtime: false,
    schedule_rules: [],

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

function emptyKeywordModel(): KeywordFormModel {
  return {
    ID: 0,
    name: '',
    remark: '',
    block_words: [],
    allow_words: [],
    replace_rules: [],
  }
}

const kwModel = reactive<KeywordFormModel>(emptyKeywordModel())

function cloneModeLabel(v: number): string {
  if (v === 1) return '转发'
  if (v === 2) return '发送'
  return '下载上传'
}

function summarizeTags(s: Strategy): string[] {
  const tags: string[] = []
  tags.push(cloneModeLabel(Number(s.clone_mode || 3)))

  const at = Array.isArray(s.allowed_types) ? s.allowed_types : []
  const ct = at.length ? at : Array.isArray(s.content_types) ? s.content_types : []
  tags.push(ct.length ? `类型:${ct.join(',')}` : '类型:全部')

  const push = Boolean((s as any).enable_realtime ?? s.realtime)
  if (push) tags.push('监听')
  if (s.keep_reply) tags.push('保留回复')
  if (s.clone_comment) tags.push('克隆评论')
  if (s.gpu_accel) tags.push('GPU')
  if (s.change_md5) tags.push('改MD5')

  const poll = Number(s.poll_interval ?? 0)
  if (poll > 0) tags.push(`轮询:${poll}s`)

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

const filteredKwProfiles = computed(() => {
  const q = (kwSearch.value || '').trim().toLowerCase()
  if (!q) return kwProfiles.value
  return kwProfiles.value.filter((p) => (p.name || '').toLowerCase().includes(q))
})

function normalizeScheduleRules(input: any): ScheduleRule[] {
  if (!input) return []
  let raw: any = input
  if (typeof raw === 'string') {
    try {
      raw = JSON.parse(raw)
    } catch {
      return []
    }
  }
  if (!Array.isArray(raw)) return []

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

function buildPayload(m: StrategyFormModel): Partial<Strategy> {
  const poll = Number(m.poll_interval ?? 0)
  const enable = Boolean((m as any).enable_realtime ?? m.realtime)
  const types = Array.isArray(m.allowed_types) ? m.allowed_types : Array.isArray(m.content_types) ? m.content_types : []
  return {
    name: (m.name || '').trim(),
    remark: (m.remark || '').trim(),

    clone_mode: Number(m.clone_mode || 3),
    allowed_types: types,
    // keep legacy field synced for backward compatibility
    content_types: types,

    scope_type: Number(m.scope_type || 1),
    scope_value: (m.scope_value || '').trim(),
    history_order: Number(m.history_order || 1),
    poll_interval: !Number.isFinite(poll) ? 0 : poll <= 0 ? 0 : Math.max(10, Math.floor(poll)),

    enable_realtime: enable,
    // keep legacy field synced for backward compatibility
    realtime: enable,
    schedule_rules: normalizeScheduleRules((m as any).schedule_rules),

    keep_reply: Boolean(m.keep_reply),
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
  const enable = Boolean((row as any).enable_realtime ?? row.realtime)
  const types =
    Array.isArray(row.allowed_types) && row.allowed_types.length
      ? [...row.allowed_types]
      : Array.isArray(row.content_types)
        ? [...row.content_types]
        : []
  Object.assign(editModel, emptyModel(), {
    ID: Number(row.ID || 0),
    name: (row.name || '').trim(),
    remark: (row.remark || '').trim(),

    clone_mode: Number(row.clone_mode || 3),
    allowed_types: types,
    content_types: types,

    scope_type: Number(row.scope_type || 1),
    scope_value: (row.scope_value || '').trim(),
    history_order: Number(row.history_order || 1),
    poll_interval: Number(row.poll_interval ?? 0),
    enable_realtime: enable,
    schedule_rules: normalizeScheduleRules((row as any).schedule_rules),

    keep_reply: Boolean(row.keep_reply),
    realtime: enable,
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

async function reloadStrategies() {
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

async function reloadKeywords() {
  kwLoading.value = true
  try {
    kwProfiles.value = await getKeywordProfiles()
  } catch (err: any) {
    ElMessage.error(err?.message || '加载关键词策略失败')
    kwProfiles.value = []
  } finally {
    kwLoading.value = false
  }
}

async function reload() {
  await Promise.all([reloadStrategies(), reloadKeywords()])
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
    await reloadStrategies()
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
    await reloadStrategies()
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
    await reloadStrategies()
  } catch (e: any) {
    ElMessage.error(e?.message || '删除失败')
  }
}

function buildKeywordPayload(m: KeywordFormModel): Partial<KeywordProfile> {
  const cleanRules = (arr: any): Array<{ content: string; is_regex: boolean }> => {
    if (!Array.isArray(arr)) return []
    const out: Array<{ content: string; is_regex: boolean }> = []
    const seen = new Set<string>()
    for (const raw of arr) {
      if (typeof raw === 'string') {
        const s = raw.trim()
        if (!s) continue
        const key = s.toLowerCase() + '\x00txt'
        if (seen.has(key)) continue
        seen.add(key)
        out.push({ content: s, is_regex: false })
        continue
      }

      const content = String(raw?.content ?? raw?.Content ?? '').trim()
      if (!content) continue
      const is_regex = Boolean(raw?.is_regex ?? raw?.IsRegex)

      let key = content
      if (!is_regex) key = key.toLowerCase()
      key += is_regex ? '\x00re' : '\x00txt'

      if (seen.has(key)) continue
      seen.add(key)
      out.push({ content, is_regex })
    }
    return out
  }

  const rules = Array.isArray(m.replace_rules) ? m.replace_rules : []
  const replace_rules = rules
    .map((r) => ({ from: String(r?.from || '').trim(), to: String(r?.to || '').trim() }))
    .filter((r) => r.from)

  return {
    name: (m.name || '').trim(),
    remark: (m.remark || '').trim(),
    block_words: cleanRules(m.block_words),
    allow_words: cleanRules(m.allow_words),
    replace_rules,
  }
}

function kwHasRegex(row: KeywordProfile): boolean {
  const has = (v: any): boolean =>
    Array.isArray(v) && v.some((x) => typeof x === 'object' && x && (x.is_regex === true || x.IsRegex === true))
  return has((row as any).block_words) || has((row as any).allow_words)
}

function openKwCreate() {
  kwDialogMode.value = 'create'
  Object.assign(kwModel, emptyKeywordModel())
  kwDialogVisible.value = true
}

function openKwEdit(row: KeywordProfile) {
  kwDialogMode.value = 'edit'

  const toRules = (v: any): Array<{ content: string; is_regex: boolean }> => {
    if (!Array.isArray(v)) return []
    return v
      .map((raw) => {
        if (typeof raw === 'string') {
          const s = raw.trim()
          return s ? { content: s, is_regex: false } : null
        }
        const content = String(raw?.content ?? raw?.Content ?? '').trim()
        if (!content) return null
        return { content, is_regex: Boolean(raw?.is_regex ?? raw?.IsRegex) }
      })
      .filter(Boolean) as Array<{ content: string; is_regex: boolean }>
  }

  Object.assign(kwModel, emptyKeywordModel(), {
    ID: Number(row.ID || 0),
    name: (row.name || '').trim(),
    remark: (row.remark || '').trim(),
    block_words: toRules((row as any).block_words),
    allow_words: toRules((row as any).allow_words),
    replace_rules: Array.isArray(row.replace_rules) ? row.replace_rules.map((r) => ({ from: r.from, to: r.to })) : [],
  })
  kwDialogVisible.value = true
}

const kwDialogTitle = computed(() => {
  const name = (kwModel.name || '').trim()
  if (kwDialogMode.value === 'create') return '新建关键词策略'
  return `编辑关键词策略: ${name || '#' + kwModel.ID}`
})

watch(kwDialogVisible, (v) => {
  if (!v) return
  void nextTick(() => kwFormRef.value?.clearValidate?.())
})

async function submitKw() {
  const ok = await kwFormRef.value?.validate?.()
  if (!ok) return

  const payload = buildKeywordPayload(kwModel)
  if (!payload.name) {
    ElMessage.warning('请填写方案名称')
    return
  }

  kwSaving.value = true
  try {
    if (kwDialogMode.value === 'create') {
      const created = await createKeywordProfile(payload)
      ElMessage.success(`关键词策略已创建 #${created.ID}`)
    } else {
      const id = Number(kwModel.ID || 0)
      if (!id) return
      const updated = await updateKeywordProfile(id, payload)
      ElMessage.success(`关键词策略已保存 #${updated.ID}`)
    }
    kwDialogVisible.value = false
    await reloadKeywords()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    kwSaving.value = false
  }
}

async function removeKw(row?: KeywordProfile) {
  const id = Number(row?.ID || kwModel.ID || 0)
  if (!id) return
  const name = (row?.name || kwModel.name || '').trim()

  try {
    await ElMessageBox.confirm(`确定删除关键词策略「${name || '#' + id}」吗？`, '确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await deleteKeywordProfile(id)
    ElMessage.success('已删除')
    if (kwDialogVisible.value) kwDialogVisible.value = false
    await reloadKeywords()
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
      <el-tab-pane label="行为策略库" name="library">
        <el-card class="bt-card pane-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-flow-chart-line" />
                <span>行为策略库</span>
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
                <el-button @click="reloadStrategies" :loading="loading">
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

      <el-tab-pane label="新建行为策略" name="create">
        <div class="create-wrap">
          <el-card class="bt-card pane-card" shadow="never">
            <template #header>
              <div class="card-header">
                <div class="card-title">
                  <i class="ri-add-circle-line" />
                  <span>创建新行为策略</span>
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
        </div>
      </el-tab-pane>

      <el-tab-pane label="关键词策略" name="keywords">
        <el-card class="bt-card pane-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-filter-3-line" />
                <span>关键词策略</span>
              </div>
              <div class="card-sub">共 {{ kwProfiles.length }} 条</div>
            </div>
          </template>

          <div class="pane">
            <div class="toolbar">
              <div class="toolbar-left">
                <el-button type="primary" @click="openKwCreate">
                  <i class="ri-add-line" />
                  新建关键词策略
                </el-button>
                <el-button @click="reloadKeywords" :loading="kwLoading">
                  <i class="ri-refresh-line" />
                  刷新
                </el-button>
              </div>
              <div class="toolbar-right">
                <el-input v-model="kwSearch" placeholder="搜索关键词策略名称" clearable class="search" />
              </div>
            </div>

            <div class="table-body">
              <el-table :data="filteredKwProfiles" v-loading="kwLoading" stripe height="100%" style="width: 100%">
                <el-table-column prop="ID" label="ID" width="90" />

                <el-table-column label="方案" min-width="260">
                  <template #default="{ row }">
                    <div class="st-name">{{ row.name }}</div>
                    <div v-if="row.remark" class="st-remark">{{ row.remark }}</div>
                    <div class="st-meta">
                      屏蔽 {{ row.block_words?.length || 0 }} | 白名单 {{ row.allow_words?.length || 0 }} | 替换
                      {{ row.replace_rules?.length || 0 }}
                      <span v-if="kwHasRegex(row)"> | Regex</span>
                    </div>
                  </template>
                </el-table-column>

                <el-table-column label="标签" min-width="360">
                  <template #default="{ row }">
                    <el-space wrap>
                      <el-tag size="small" type="info" effect="plain">屏蔽: {{ row.block_words?.length || 0 }}</el-tag>
                      <el-tag size="small" type="info" effect="plain">白名单: {{ row.allow_words?.length || 0 }}</el-tag>
                      <el-tag size="small" type="info" effect="plain">替换: {{ row.replace_rules?.length || 0 }}</el-tag>
                      <el-tag v-if="kwHasRegex(row)" size="small" type="success" effect="plain">Regex</el-tag>
                    </el-space>
                  </template>
                </el-table-column>

                <el-table-column label="操作" width="160" fixed="right">
                  <template #default="{ row }">
                    <el-space size="small">
                      <el-button link type="primary" @click="openKwEdit(row)">编辑</el-button>
                      <el-button link type="danger" @click="removeKw(row)">删除</el-button>
                    </el-space>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </el-card>
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

    <el-dialog v-model="kwDialogVisible" :title="kwDialogTitle" width="960px" class="bt-dialog kw-dialog" destroy-on-close>
      <KeywordForm ref="kwFormRef" v-model="kwModel" :loading="kwSaving" :show-actions="false" />

      <template #footer>
        <el-space>
          <el-button @click="kwDialogVisible = false">取消</el-button>
          <el-button v-if="kwDialogMode === 'edit'" type="danger" :disabled="kwSaving" @click="removeKw()">
            <i class="ri-delete-bin-6-line" />
            删除
          </el-button>
          <el-button type="primary" :loading="kwSaving" @click="submitKw">
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
  --tgvive-green: #20a53a;
  --el-color-primary: var(--tgvive-green);
  --el-color-primary-dark-2: color-mix(in srgb, var(--tgvive-green) 80%, #000);
  --el-color-primary-light-3: color-mix(in srgb, var(--tgvive-green) 70%, #fff);
  --el-color-primary-light-5: color-mix(in srgb, var(--tgvive-green) 50%, #fff);
  --el-color-primary-light-7: color-mix(in srgb, var(--tgvive-green) 30%, #fff);
  --el-color-primary-light-8: color-mix(in srgb, var(--tgvive-green) 20%, #fff);
  --el-color-primary-light-9: color-mix(in srgb, var(--tgvive-green) 10%, #fff);
}

.bt-tabs :deep(.el-tabs__header) {
  margin: 0 0 12px;
}

.bt-card {
  border: 1px solid #363637;
  border-radius: 4px;
  background: #252525;

  :deep(.el-card__header) {
    padding: 12px 14px;
    border-bottom: 1px solid #363637;
    background: #252525;
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

.create-wrap {
  width: 100%;
  max-width: none;
}

.form-host {
  flex: 1;
  overflow: hidden;
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
    color: var(--el-color-primary);
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

.st-meta {
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

.kw-dialog {
  :deep(.el-dialog) {
    max-width: 96vw;
  }
}
</style>
