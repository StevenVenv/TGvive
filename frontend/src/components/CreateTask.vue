<script setup lang="ts">
import axios from 'axios'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { UserFilled } from '@element-plus/icons-vue'
import { resolveAPIURL } from '../runtime/backend'

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
  clone_mode?: number
}

type KeywordProfileItem = {
  ID: number
  name: string
  remark?: string
  block_words?: Array<{ content: string; is_regex: boolean } | string>
  allow_words?: Array<{ content: string; is_regex: boolean } | string>
  replace_rules?: Array<{ from: string; to: string }>
}

type TGDialogItem = {
  kind: 'channel' | 'supergroup' | 'group' | string
  title: string
  username?: string
  channel_id?: number
  chat_id?: number
  bot_chat_id: string
  task_value: string
}

const accounts = ref<AccountItem[]>([])
const strategies = ref<StrategyItem[]>([])
const keywordProfiles = ref<KeywordProfileItem[]>([])
type BotItem = {
  id: string
  name: string
  api_base: string
  disabled?: boolean
  token_set: boolean
  token_mask?: string
  updated_at?: number
}
const bots = ref<BotItem[]>([])

const form = reactive({
  source_url: '',
  target_url: '',
  remark: '',
  session_key: '',
  publish_type: 'same' as 'same' | 'account' | 'bot',
  publish_session_key: '',
  publish_bot_id: '',
  strategy_id: 0,
  keyword_profile_id: 0,
})

const selectedAccount = computed(() => accounts.value.find((a) => a.key === form.session_key) || null)
const selectedPublishAccount = computed(() => accounts.value.find((a) => a.key === form.publish_session_key) || null)
const separatedPublish = computed(() => form.publish_type !== 'same')

const rules: FormRules = {
  source_url: [{ required: true, message: '请输入源频道/群组', trigger: 'blur' }],
  target_url: [{ required: true, message: '请输入目标频道/群组', trigger: 'blur' }],
  session_key: [{ required: true, message: '请选择爬虫账号', trigger: 'change' }],
  publish_session_key: [
    {
      validator: (_: any, v: any, cb: any) => {
        if (form.publish_type !== 'account') return cb()
        const s = String(v || '').trim()
        if (s) cb()
        else cb(new Error('请选择发布账号'))
      },
      trigger: 'change',
    },
  ],
  publish_bot_id: [
    {
      validator: (_: any, v: any, cb: any) => {
        if (form.publish_type !== 'bot') return cb()
        const s = String(v || '').trim()
        if (s) cb()
        else cb(new Error('请选择发布 Bot'))
      },
      trigger: 'change',
    },
  ],
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
  form.remark = ''
  form.session_key = ''
  form.publish_type = 'same'
  form.publish_session_key = ''
  form.publish_bot_id = ''
  form.strategy_id = 0
  form.keyword_profile_id = 0
}

async function apiGet<T>(path: string): Promise<T> {
  const res = await axios.get<ApiResponse<T>>(resolveAPIURL(path), {
    withCredentials: true,
  })
  if (res.data.code !== 0) throw new Error(res.data.msg || '请求失败')
  return res.data.data
}

async function apiPost<T>(path: string, body: any): Promise<T> {
  const res = await axios.post<ApiResponse<T>>(resolveAPIURL(path), body, {
    withCredentials: true,
  })
  if (res.data.code !== 0) throw new Error(res.data.msg || '请求失败')
  return res.data.data
}

function kindLabel(kind: string): string {
  if (kind === 'supergroup') return '超级群'
  if (kind === 'group') return '群组'
  return '频道'
}

function peerInfoText(info: TGDialogItem | null): string {
  const title = String(info?.title || '').trim()
  const u = String(info?.username || '').trim()
  const k = kindLabel(String(info?.kind || '').trim())
  if (!title) return k
  return u ? `${k} · ${title} (@${u})` : `${k} · ${title}`
}

const sourcePeerInfo = ref<TGDialogItem | null>(null)
const sourcePeerErr = ref('')
const sourcePeerLoading = ref(false)

const targetPeerInfo = ref<TGDialogItem | null>(null)
const targetPeerErr = ref('')
const targetPeerLoading = ref(false)

async function resolvePeerTitle(sessionKey: string, peer: string): Promise<TGDialogItem> {
  const key = encodeURIComponent(String(sessionKey || '').trim())
  const p = String(peer || '').trim()
  const q = new URLSearchParams({ peer: p })
  return apiGet<TGDialogItem>(`/api/v1/tg/accounts/${key}/resolve?${q.toString()}`)
}

let sourceResolveTimer = 0
let targetResolveTimer = 0
let sourceResolveSeq = 0
let targetResolveSeq = 0

function scheduleResolveSource() {
  if (sourceResolveTimer) window.clearTimeout(sourceResolveTimer)
  sourceResolveTimer = window.setTimeout(() => {
    void resolveSourceNow()
  }, 550)
}

function scheduleResolveTarget() {
  if (targetResolveTimer) window.clearTimeout(targetResolveTimer)
  targetResolveTimer = window.setTimeout(() => {
    void resolveTargetNow()
  }, 550)
}

async function resolveSourceNow() {
  if (!open.value) return
  const key = String(form.session_key || '').trim()
  const peer = String(form.source_url || '').trim()
  if (!key || !peer) {
    sourcePeerInfo.value = null
    sourcePeerErr.value = ''
    sourcePeerLoading.value = false
    return
  }

  const seq = ++sourceResolveSeq
  sourcePeerLoading.value = true
  sourcePeerErr.value = ''
  try {
    const info = await resolvePeerTitle(key, peer)
    if (seq !== sourceResolveSeq) return
    sourcePeerInfo.value = info
  } catch (err: any) {
    if (seq !== sourceResolveSeq) return
    sourcePeerInfo.value = null
    sourcePeerErr.value = String(err?.message || '解析失败')
  } finally {
    if (seq === sourceResolveSeq) sourcePeerLoading.value = false
  }
}

async function resolveTargetNow() {
  if (!open.value) return
  const key = String(form.session_key || '').trim()
  const peer = String(form.target_url || '').trim()
  if (!key || !peer) {
    targetPeerInfo.value = null
    targetPeerErr.value = ''
    targetPeerLoading.value = false
    return
  }

  const seq = ++targetResolveSeq
  targetPeerLoading.value = true
  targetPeerErr.value = ''
  try {
    const info = await resolvePeerTitle(key, peer)
    if (seq !== targetResolveSeq) return
    targetPeerInfo.value = info
  } catch (err: any) {
    if (seq !== targetResolveSeq) return
    targetPeerInfo.value = null
    targetPeerErr.value = String(err?.message || '解析失败')
  } finally {
    if (seq === targetResolveSeq) targetPeerLoading.value = false
  }
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

function kwHasRegex(k: KeywordProfileItem): boolean {
  const has = (v: any): boolean =>
    Array.isArray(v) && v.some((x) => typeof x === 'object' && x && (x.is_regex === true || x.IsRegex === true))
  return has(k.block_words) || has(k.allow_words)
}

async function loadOptions() {
  loading.value = true
  try {
    const [acc, stg, kw] = await Promise.all([
      apiGet<AccountItem[]>('/api/v1/accounts'),
      apiGet<StrategyItem[]>('/api/v1/strategies'),
      apiGet<KeywordProfileItem[]>('/api/v1/keyword-profiles'),
    ])
    accounts.value = acc || []
    strategies.value = stg || []
    keywordProfiles.value = kw || []
    bots.value = (await apiGet<BotItem[]>('/api/v1/tg/bots')) || []

    const onlyAccount = accounts.value.length === 1 ? accounts.value[0] : undefined
    if (!form.session_key && onlyAccount) form.session_key = onlyAccount.key

    const onlyStrategy = strategies.value.length === 1 ? strategies.value[0] : undefined
    if (!form.strategy_id && onlyStrategy) form.strategy_id = onlyStrategy.ID

    const onlyKw = keywordProfiles.value.length === 1 ? keywordProfiles.value[0] : undefined
    if (!form.keyword_profile_id && onlyKw) form.keyword_profile_id = onlyKw.ID
  } catch (err: any) {
    ElMessage.error(err?.message || '初始化失败')
    accounts.value = []
    strategies.value = []
    keywordProfiles.value = []
    bots.value = []
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
    remark: (form.remark || '').trim(),
    session_key: (form.session_key || '').trim(),
    publish_type: form.publish_type,
    publish_session_key: (form.publish_session_key || '').trim(),
    publish_bot_id: (form.publish_bot_id || '').trim(),
    strategy_id: Number(form.strategy_id || 0),
    keyword_profile_id: Number(form.keyword_profile_id || 0),
  }

  if (!payload.source_url || !payload.target_url || !payload.session_key || !payload.strategy_id) {
    ElMessage.warning('请完整填写 4 个字段')
    return
  }

  if (payload.publish_type === 'account' && !payload.publish_session_key) {
    ElMessage.warning('请选择发布账号')
    return
  }
  if (payload.publish_type === 'bot' && !payload.publish_bot_id) {
    ElMessage.warning('请选择发布 Bot')
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
    sourcePeerInfo.value = null
    sourcePeerErr.value = ''
    sourcePeerLoading.value = false
    targetPeerInfo.value = null
    targetPeerErr.value = ''
    targetPeerLoading.value = false
    sourceResolveSeq++
    targetResolveSeq++
    if (sourceResolveTimer) window.clearTimeout(sourceResolveTimer)
    if (targetResolveTimer) window.clearTimeout(targetResolveTimer)
    sourceResolveTimer = 0
    targetResolveTimer = 0
  }
})

onMounted(() => {
  void loadOptions()
})

watch(
  () => [open.value, form.session_key, form.source_url],
  () => {
    if (!open.value) return
    scheduleResolveSource()
  },
)

watch(
  () => [open.value, form.session_key, form.target_url],
  () => {
    if (!open.value) return
    scheduleResolveTarget()
  },
)

watch(
  () => form.publish_type,
  () => {
    if (form.publish_type === 'same') {
      form.publish_session_key = ''
      form.publish_bot_id = ''
      return
    }
    if (form.publish_type === 'account') {
      form.publish_bot_id = ''
    }
    if (form.publish_type === 'bot') {
      form.publish_session_key = ''
    }

    // 分开发送强制 CloneMode=3：禁用非 3 的策略
    const cur = strategies.value.find((s) => s.ID === Number(form.strategy_id || 0))
    const cm = Number(cur?.clone_mode || 0)
    if (cm && cm !== 3) {
      form.strategy_id = 0
    }
  },
)
</script>

<template>
  <el-button type="primary" class="create-task-btn" @click="open = true">
    <i class="ri-add-line" />
    <span>新建任务</span>
  </el-button>

  <el-dialog v-model="open" title="新建转发任务" width="660px" class="bt-dialog">
    <div v-loading="loading" class="body">
        <el-form ref="formRef" :model="form" :rules="rules" label-position="top" class="form">
        <el-form-item label="源频道 (Source)" prop="source_url">
          <el-input v-model="form.source_url" placeholder="ID 链接 @user" />
          <div v-if="sourcePeerLoading" class="hint muted">解析中...</div>
          <div v-else-if="sourcePeerInfo" class="hint muted">已解析：{{ peerInfoText(sourcePeerInfo) }}</div>
          <div v-else-if="sourcePeerErr" class="hint err">{{ sourcePeerErr }}</div>
        </el-form-item>

        <el-form-item label="目标频道 (Target)" prop="target_url">
          <el-input v-model="form.target_url" placeholder="ID 链接 @user" />
          <div v-if="targetPeerLoading" class="hint muted">解析中...</div>
          <div v-else-if="targetPeerInfo" class="hint muted">已解析：{{ peerInfoText(targetPeerInfo) }}</div>
          <div v-else-if="targetPeerErr" class="hint err">{{ targetPeerErr }}</div>
        </el-form-item>

        <el-form-item label="任务备注（可选）">
          <el-input v-model="form.remark" placeholder="可选：用于区分任务用途/来源" maxlength="255" show-word-limit />
        </el-form-item>

        <el-divider content-position="left">账号策略</el-divider>

        <el-form-item label="爬虫账号 (Crawler)" prop="session_key">
          <el-select
            v-model="form.session_key"
            placeholder="请选择爬虫账号"
            style="width: 100%"
            filterable
            popper-class="tgvive-dark-popper"
          >
            <template #prefix>
              <div class="select-prefix">
                <el-avatar class="select-avatar" :size="20" :src="selectedAccount?.avatar || ''" :icon="UserFilled" />
              </div>
            </template>
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

        <el-form-item label="发布方式 (Publisher)" prop="publish_type">
          <el-radio-group v-model="form.publish_type">
            <el-radio-button label="same">同爬虫账号</el-radio-button>
            <el-radio-button label="account">TG账号</el-radio-button>
            <el-radio-button label="bot">Bot</el-radio-button>
          </el-radio-group>
          <div v-if="separatedPublish" class="hint">分开发送将强制使用下载上传（CloneMode=3），转发/复制策略会被禁用</div>
        </el-form-item>

        <el-form-item v-if="form.publish_type === 'account'" label="发布账号 (Publisher Account)" prop="publish_session_key">
          <el-select
            v-model="form.publish_session_key"
            placeholder="请选择发布账号"
            style="width: 100%"
            filterable
            popper-class="tgvive-dark-popper"
          >
            <template #prefix>
              <div class="select-prefix">
                <el-avatar class="select-avatar" :size="20" :src="selectedPublishAccount?.avatar || ''" :icon="UserFilled" />
              </div>
            </template>
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
          <div class="hint">选择一个账号用于发布（与爬虫账号可不同）</div>
        </el-form-item>

        <el-form-item v-if="form.publish_type === 'bot'" label="发布 Bot (Publisher Bot)" prop="publish_bot_id">
          <el-select
            v-model="form.publish_bot_id"
            placeholder="请选择 Bot"
            style="width: 100%"
            filterable
            popper-class="tgvive-dark-popper"
          >
            <el-option v-for="b in bots" :key="b.id" :label="b.name || b.id" :value="b.id" :disabled="b.disabled">
              <div class="opt">
                <div class="opt-left">
                  <div class="opt-title">{{ b.name || b.id }}</div>
                  <div class="opt-sub">{{ b.api_base }}</div>
                </div>
                <div class="opt-right muted">{{ b.disabled ? '已禁用' : b.token_mask || '' }}</div>
              </div>
            </el-option>
          </el-select>
          <div class="hint">使用 Bot API 发布（需将 Bot 加入目标频道/群并赋权）</div>
        </el-form-item>

        <el-form-item label="策略模版 (Strategy)" prop="strategy_id">
          <el-select
            v-model="form.strategy_id"
            placeholder="请选择策略模版"
            style="width: 100%"
            filterable
            popper-class="tgvive-dark-popper"
          >
            <el-option
              v-for="s in strategies"
              :key="s.ID"
              :label="s.name"
              :value="s.ID"
              :disabled="separatedPublish && Number(s.clone_mode || 0) !== 3"
            >
              <div class="opt">
                <div class="opt-left">
                  <div class="opt-title">{{ s.name }}</div>
                  <div v-if="s.remark" class="opt-sub">{{ s.remark }}</div>
                </div>
              </div>
            </el-option>
          </el-select>
          <div v-if="strategyTip" class="hint">{{ strategyTip }}</div>
          <div v-if="separatedPublish" class="hint">提示：分开发送仅支持 CloneMode=3（下载上传）</div>
        </el-form-item>

        <el-form-item label="关键词方案 (Keywords)" prop="keyword_profile_id">
          <el-select
            v-model="form.keyword_profile_id"
            placeholder="可选：关键词过滤/替换"
            style="width: 100%"
            filterable
            clearable
            popper-class="tgvive-dark-popper"
          >
            <el-option v-for="k in keywordProfiles" :key="k.ID" :label="k.name" :value="k.ID">
              <div class="opt">
                <div class="opt-left">
                  <div class="opt-title">{{ k.name }}</div>
                  <div class="opt-sub">
                    <span v-if="k.remark">{{ k.remark }} · </span>
                    屏蔽 {{ k.block_words?.length || 0 }} | 白名单 {{ k.allow_words?.length || 0 }} | 替换 {{ k.replace_rules?.length || 0 }}
                    <span v-if="kwHasRegex(k)"> | Regex</span>
                  </div>
                </div>
              </div>
            </el-option>
          </el-select>
          <div class="hint">可留空：仅使用行为策略；选择后将启用关键词过滤/替换</div>
        </el-form-item>
      </el-form>
    </div>

    <template #footer>
      <el-space>
        <el-button @click="open = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">
          <i class="ri-add-line" />
          <span>创建</span>
        </el-button>
      </el-space>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.body {
  padding: 4px 2px 0;
}

.create-task-btn {
  display: inline-flex;
  align-items: center;
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
  width: 100%;
}

.opt-left {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
}

.opt-avatar {
  flex-shrink: 0;
}

.opt-avatar :deep(img) {
  object-fit: cover;
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

.select-prefix {
  display: flex;
  align-items: center;
}

.select-avatar {
  flex-shrink: 0;
  opacity: 0.95;
}

:deep(.select-avatar img) {
  object-fit: cover;
}

</style>
