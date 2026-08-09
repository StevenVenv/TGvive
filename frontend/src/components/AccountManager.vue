<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import {
  checkTGAccountSession,
  checkTGAccountSpamBot,
  deleteTGAccount,
  getAccountQRStatus,
  getCodeLoginStatus,
  listTGAccounts,
  startAccountQR,
  startCodeLogin,
  submitCode,
  submitPassword,
  submitQRPassword,
  type CodeAuthState,
  type QRState,
  type TGAccountSessionCheckResult,
  type TGAccountSpamBotCheckResult,
  type TGAccount,
} from '../api'

type SpamCheckStatus = 'unchecked' | 'checking' | 'ok' | 'restricted' | 'blocked' | 'unknown' | 'error'
type KeepAliveStatus = 'idle' | 'checking' | 'valid' | 'invalid'

type SpamCheckState = { status: SpamCheckStatus; checked_at?: number; text?: string; error?: string }
type KeepAliveState = { status: KeepAliveStatus; checked_at?: number; detail?: Record<string, any>; error?: string }

const accounts = ref<TGAccount[]>([])
const loadingAccounts = ref(false)
const refreshAnim = ref(false)
const refreshPulse = ref(false)

const selectedKey = ref<string>('')
const selectedAccount = computed(() => accounts.value.find((a) => a.key === selectedKey.value) || null)

const spamByKey = ref<Record<string, SpamCheckState>>({})
const keepAliveByKey = ref<Record<string, KeepAliveState>>({})

function fmtTime(unixSeconds?: number): string {
  if (!unixSeconds) return '-'
  return new Date(unixSeconds * 1000).toLocaleString()
}

function avatarText(a: TGAccount): string {
  const name = (a.name || '').trim()
  if (name) return name.slice(0, 1).toUpperCase()
  const u = (a.username || '').trim()
  if (u) return u.slice(0, 1).toUpperCase()
  if (a.user_id) return String(a.user_id).slice(-2)
  return 'TG'
}

function displayName(a: TGAccount): string {
  const name = (a.name || '').trim()
  if (name) return name
  const u = (a.username || '').trim()
  if (u) return '@' + u
  const p = (a.phone || '').trim()
  if (p) return p
  return a.key
}

function sessionFileName(key: string): string {
  key = (key || '').trim()
  if (!key) return '-'
  return `session_${key}.json`
}

function ensureSelection() {
  const k = selectedKey.value
  if (!k) return
  if (!accounts.value.some((a) => a.key === k)) {
    selectedKey.value = ''
  }
}

async function reloadAccounts() {
  loadingAccounts.value = true
  try {
    accounts.value = await listTGAccounts()
    ensureSelection()
  } catch (err: any) {
    ElMessage.error(err?.message || '加载账号失败')
  } finally {
    loadingAccounts.value = false
  }
}

async function handleRefresh() {
  refreshAnim.value = true
  refreshPulse.value = true
  try {
    await reloadAccounts()
    ElMessage.success('已刷新')
  } finally {
    window.setTimeout(() => {
      refreshAnim.value = false
      refreshPulse.value = false
    }, 650)
  }
}

function rowClassName({ row }: { row: TGAccount }) {
  return row.key === selectedKey.value ? 'is-selected' : ''
}

function onRowClick(row: TGAccount) {
  selectedKey.value = row.key
}

function getSpamState(key: string): SpamCheckState {
  return spamByKey.value[key] || { status: 'unchecked' }
}

function getKeepAliveState(key: string): KeepAliveState {
  return keepAliveByKey.value[key] || { status: 'idle' }
}

function normalizeSpamStatus(s: string): SpamCheckStatus {
  const v = String(s || '')
    .trim()
    .toLowerCase()
  if (v === 'ok') return 'ok'
  if (v === 'restricted') return 'restricted'
  if (v === 'blocked') return 'blocked'
  if (v === 'error') return 'error'
  if (v === 'unknown') return 'unknown'
  return 'unknown'
}

function normalizeKeepAlive(res: TGAccountSessionCheckResult): KeepAliveStatus {
  if (!res?.ok) return 'invalid'
  return res.authorized ? 'valid' : 'invalid'
}

function buildKeepAliveDetailFromAPI(a: TGAccount, res: TGAccountSessionCheckResult) {
  return {
    key: a.key,
    session_file: sessionFileName(a.key),
    user_id: res.user_id ?? a.user_id ?? null,
    username: res.username ? '@' + res.username : a.username ? '@' + a.username : null,
    phone: res.phone ?? a.phone ?? null,
    dc_id: res.this_dc ?? null,
    nearest_dc: res.nearest_dc ?? null,
    country: res.country ?? null,
    restricted: Boolean(res.restricted ?? false) ? 'yes' : 'no',
    session_updated_at: fmtTime(a.updated_at),
    meta_updated_at: a.meta_updated_at ? fmtTime(a.meta_updated_at) : null,
    error: res.error ?? null,
  }
}

async function triggerSpamCheck(a: TGAccount, autoUnblock = false) {
  const key = a.key
  const cur = getSpamState(key)
  if (cur.status === 'checking') return

  spamByKey.value = { ...spamByKey.value, [key]: { status: 'checking' } }
  try {
    const r: TGAccountSpamBotCheckResult = await checkTGAccountSpamBot(key, { timeout_ms: 12_000, auto_unblock: autoUnblock })
    const st = normalizeSpamStatus(r?.status || '')
    spamByKey.value = {
      ...spamByKey.value,
      [key]: {
        status: st,
        checked_at: Number(r?.checked_at || 0) || Math.floor(Date.now() / 1000),
        text: String(r?.text || '').trim() || undefined,
        error: String(r?.error || '').trim() || undefined,
      },
    }
    if (r?.error) ElMessage.warning(r.error)
  } catch (e: any) {
    spamByKey.value = {
      ...spamByKey.value,
      [key]: { status: 'error', checked_at: Math.floor(Date.now() / 1000), error: e?.message || '检测失败' },
    }
    ElMessage.error(e?.message || '检测失败')
  }
}

async function checkKeepAliveForSelected() {
  const a = selectedAccount.value
  if (!a) return

  const key = a.key
  const cur = getKeepAliveState(key)
  if (cur.status === 'checking') return

  keepAliveByKey.value = { ...keepAliveByKey.value, [key]: { status: 'checking' } }
  try {
    const r: TGAccountSessionCheckResult = await checkTGAccountSession(key, { timeout_ms: 8_000 })
    const st = normalizeKeepAlive(r)
    const detail = buildKeepAliveDetailFromAPI(a, r)
    keepAliveByKey.value = {
      ...keepAliveByKey.value,
      [key]: {
        status: st,
        checked_at: Number(r?.checked_at || 0) || Math.floor(Date.now() / 1000),
        detail,
        error: String(r?.error || '').trim() || undefined,
      },
    }
    if (r?.error) ElMessage.warning(r.error)
    else ElMessage.success('检测完成')
  } catch (e: any) {
    keepAliveByKey.value = {
      ...keepAliveByKey.value,
      [key]: {
        status: 'invalid',
        checked_at: Math.floor(Date.now() / 1000),
        error: e?.message || '检测失败',
        detail: {
          key: a.key,
          session_file: sessionFileName(a.key),
          session_updated_at: fmtTime(a.updated_at),
          meta_updated_at: a.meta_updated_at ? fmtTime(a.meta_updated_at) : null,
          error: e?.message || '检测失败',
        },
      },
    }
    ElMessage.error(e?.message || '检测失败')
  }
}

async function confirmAndRemove(a: TGAccount, title: string) {
  try {
    await ElMessageBox.confirm(`确定${title}「${displayName(a)}」吗？\n（将删除本地 session 文件，相关任务会受影响）`, '确认', {
      confirmButtonText: title,
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return false
  }

  try {
    await deleteTGAccount(a.key)
    ElMessage.success(`${title}成功`)
    await reloadAccounts()
    return true
  } catch (err: any) {
    ElMessage.error(err?.message || `${title}失败`)
    return false
  }
}

async function deleteAccount(a: TGAccount) {
  await confirmAndRemove(a, '删除')
}

async function logoutAccount(a: TGAccount) {
  await confirmAndRemove(a, '退出')
}

// Add account dialog
const addDialogOpen = ref(false)
const addTab = ref<'qr' | 'code'>('qr')

function openAddDialog() {
  addTab.value = 'qr'
  addDialogOpen.value = true
}

// QR login
const qrSessionId = ref('')
const qrState = ref<QRState | null>(null)
const qrWorking = ref(false)
const qrNow = ref(Date.now())
let qrTimer: number | undefined

const qrStatusText = computed(() => {
  const st = qrState.value
  if (!st) return '未开始'
  switch (st.status) {
    case 'created':
      return '已创建'
    case 'pending':
      return '等待扫码'
    case 'scanned':
      return '已扫码，等待确认'
    case 'need_password':
      return '需要二级密码'
    case 'authorized':
      return '已登录'
    case 'expired':
      return '已过期'
    case 'error':
      return '异常'
    default:
      return st.status
  }
})

const qrExpiresLeftText = computed(() => {
  const exp = qrState.value?.expires_at
  if (!exp) return ''
  const sec = exp - Math.floor(qrNow.value / 1000)
  if (sec <= 0) return '已过期'
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}:${String(s).padStart(2, '0')}`
})

function stopQRPoll() {
  qrWorking.value = false
  if (qrTimer) {
    window.clearInterval(qrTimer)
    qrTimer = undefined
  }
}

async function pollQROnce() {
  if (!qrSessionId.value) return
  qrNow.value = Date.now()
  try {
    const st = await getAccountQRStatus(qrSessionId.value)
    qrState.value = st

    if (st.status === 'need_password') {
      openPasswordDialog('qr')
    }

    if (st.status === 'authorized') {
      stopQRPoll()
      closePasswordDialog()
      ElMessage.success('扫码登录成功')
      addDialogOpen.value = false
      await reloadAccounts()
    } else if (st.status === 'expired' || st.status === 'error') {
      stopQRPoll()
    }
  } catch {
    // best-effort
  }
}

async function startQR() {
  qrWorking.value = true
  qrState.value = null
  qrNow.value = Date.now()
  try {
    const { session_id } = await startAccountQR()
    qrSessionId.value = session_id
    await pollQROnce()
    if (qrTimer) window.clearInterval(qrTimer)
    qrTimer = window.setInterval(pollQROnce, 1000)
  } catch (err: any) {
    qrWorking.value = false
    ElMessage.error(err?.message || '启动扫码登录失败')
  }
}

// Code login
const codePhone = ref('')
const codeSessionId = ref('')
const codeState = ref<CodeAuthState | null>(null)
const smsCode = ref('')
const codeWorking = ref(false)
let codeTimer: number | undefined

const codeStatusText = computed(() => {
  const st = codeState.value
  if (!st) return '未开始'
  switch (st.status) {
    case 'created':
      return '已创建'
    case 'need_code':
      return '等待验证码'
    case 'need_password':
      return '需要二级密码'
    case 'authorized':
      return '已登录'
    case 'expired':
      return '已过期'
    case 'error':
      return '异常'
    default:
      return st.status
  }
})

function stopCodePoll() {
  codeWorking.value = false
  if (codeTimer) {
    window.clearInterval(codeTimer)
    codeTimer = undefined
  }
}

async function pollCodeOnce() {
  if (!codeSessionId.value) return
  try {
    const st = await getCodeLoginStatus(codeSessionId.value)
    codeState.value = st

    if (st.status === 'need_password') {
      openPasswordDialog('code')
    }

    if (st.status === 'authorized') {
      stopCodePoll()
      closePasswordDialog()
      ElMessage.success('验证码登录成功')
      addDialogOpen.value = false
      await reloadAccounts()
    } else if (st.status === 'expired' || st.status === 'error') {
      stopCodePoll()
    }
  } catch {
    // best-effort
  }
}

async function startCode() {
  const phone = codePhone.value.trim()
  if (!phone) {
    ElMessage.warning('请输入手机号（含国家码）')
    return
  }

  codeWorking.value = true
  codeState.value = null
  smsCode.value = ''
  try {
    const { session_id } = await startCodeLogin(phone)
    codeSessionId.value = session_id
    await pollCodeOnce()
    if (codeTimer) window.clearInterval(codeTimer)
    codeTimer = window.setInterval(pollCodeOnce, 1000)
    ElMessage.success('验证码已发送，请查收')
  } catch (err: any) {
    codeWorking.value = false
    ElMessage.error(err?.message || '发送验证码失败')
  }
}

async function submitSMSCode() {
  if (!codeSessionId.value) return
  const code = smsCode.value.trim()
  if (!code) {
    ElMessage.warning('请输入验证码')
    return
  }
  try {
    await submitCode(codeSessionId.value, code)
    ElMessage.success('验证码已提交')
    await pollCodeOnce()
  } catch (err: any) {
    ElMessage.error(err?.message || '提交验证码失败')
  }
}

// 2FA
type PasswordTarget = 'qr' | 'code'
const passwordDialogOpen = ref(false)
const passwordTarget = ref<PasswordTarget>('code')
const passwordInput = ref('')
const passwordSubmitting = ref(false)

function openPasswordDialog(target: PasswordTarget) {
  passwordTarget.value = target
  passwordDialogOpen.value = true
}

function closePasswordDialog() {
  passwordDialogOpen.value = false
  passwordInput.value = ''
}

async function submit2FAPassword() {
  const target = passwordTarget.value
  const sid = target === 'code' ? codeSessionId.value : qrSessionId.value
  if (!sid) return

  const p = passwordInput.value.trim()
  if (!p) {
    ElMessage.warning('请输入二级密码')
    return
  }

  passwordSubmitting.value = true
  try {
    if (target === 'code') {
      await submitPassword(sid, p)
    } else {
      await submitQRPassword(sid, p)
    }
    ElMessage.success('二级密码已提交')
    passwordInput.value = ''
    if (target === 'code') {
      await pollCodeOnce()
    } else {
      await pollQROnce()
    }
  } catch (err: any) {
    ElMessage.error(err?.message || '提交二级密码失败')
  } finally {
    passwordSubmitting.value = false
  }
}

function resetAddDialogState() {
  // QR
  stopQRPoll()
  qrSessionId.value = ''
  qrState.value = null
  qrWorking.value = false

  // Code
  stopCodePoll()
  codePhone.value = ''
  codeSessionId.value = ''
  codeState.value = null
  smsCode.value = ''
  codeWorking.value = false

  closePasswordDialog()
  addTab.value = 'qr'
}

watch(addDialogOpen, (v) => {
  if (!v) resetAddDialogState()
})

onMounted(async () => {
  await reloadAccounts()
})

onBeforeUnmount(() => {
  stopQRPoll()
  stopCodePoll()
})

defineExpose({
  reloadAccounts,
})
</script>

<template>
  <div class="account-page">
    <div class="header">
      <div class="header-left">
        <div class="title">
          <i class="ri-account-circle-line" />
          <span>账号管理</span>
        </div>
        <div class="sub">
          <span class="muted">共 {{ accounts.length }} 个账号</span>
          <span v-if="loadingAccounts" class="muted">· 加载中...</span>
        </div>
      </div>

      <div class="header-right">
        <el-button class="btn" @click="handleRefresh" :disabled="loadingAccounts">
          <i class="ri-refresh-line" :class="{ spinning: refreshAnim }" />
          <span>刷新</span>
        </el-button>
        <el-button type="primary" class="btn primary" @click="openAddDialog">
          <i class="ri-add-line" />
          <span>添加账号</span>
        </el-button>
      </div>
    </div>

    <div class="table-card" :class="{ pulse: refreshPulse }">
      <el-table
        class="acct-table"
        :data="accounts"
        v-loading="loadingAccounts"
        size="small"
        stripe
        row-key="key"
        :row-class-name="rowClassName"
        @row-click="onRowClick"
      >
        <el-table-column label="账号信息" min-width="280">
          <template #default="{ row }">
            <div class="acct-info">
              <el-avatar :size="34" :src="row.avatar" class="avatar">
                {{ avatarText(row) }}
              </el-avatar>
              <div class="meta">
                <div class="meta-top">
                  <span class="name">{{ displayName(row) }}</span>
                  <span v-if="row.phone" class="chip mono">{{ row.phone }}</span>
                </div>
                <div class="meta-sub">
                  <span class="muted mono">ID: {{ row.user_id ?? '-' }}</span>
                  <span v-if="row.username" class="muted mono">@{{ row.username }}</span>
                </div>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="Session ID" min-width="220">
          <template #default="{ row }">
            <div class="mono session">{{ sessionFileName(row.key) }}</div>
          </template>
        </el-table-column>

        <el-table-column label="SpamBot" min-width="200">
          <template #default="{ row }">
            <div class="spam" @click.stop="triggerSpamCheck(row)">
              <template v-if="getSpamState(row.key).status === 'unchecked'">
                <i class="ri-question-line muted-icon" />
                <span class="muted">未检测</span>
                <el-button link size="small" class="spam-btn" @click.stop="triggerSpamCheck(row)">
                  <i class="ri-robot-line" />
                  检测
                </el-button>
              </template>

              <template v-else-if="getSpamState(row.key).status === 'checking'">
                <i class="ri-loader-4-line spinning" />
                <span class="muted">检测中...</span>
              </template>

              <template v-else-if="getSpamState(row.key).status === 'ok'">
                <i class="ri-check-double-line ok" />
                <span class="ok">正常</span>
                <span v-if="getSpamState(row.key).checked_at" class="muted mono small">
                  · {{ fmtTime(getSpamState(row.key).checked_at) }}
                </span>
              </template>

              <template v-else-if="getSpamState(row.key).status === 'restricted'">
                <i class="ri-spam-line bad" />
                <span class="bad">受限</span>
                <span v-if="getSpamState(row.key).checked_at" class="muted mono small">
                  · {{ fmtTime(getSpamState(row.key).checked_at) }}
                </span>
              </template>

              <template v-else-if="getSpamState(row.key).status === 'blocked'">
                <i class="ri-forbid-2-line bad" />
                <span class="bad">被屏蔽</span>
                <el-button link size="small" class="spam-btn" @click.stop="triggerSpamCheck(row, true)">
                  <i class="ri-shield-check-line" />
                  <span>解除并检测</span>
                </el-button>
              </template>

              <template v-else-if="getSpamState(row.key).status === 'error'">
                <i class="ri-error-warning-line bad" />
                <span class="bad">错误</span>
                <el-button link size="small" class="spam-btn" @click.stop="triggerSpamCheck(row)">
                  <i class="ri-refresh-line" />
                  <span>重试</span>
                </el-button>
              </template>

              <template v-else>
                <i class="ri-question-line muted-icon" />
                <span class="muted">未知</span>
                <el-button link size="small" class="spam-btn" @click.stop="triggerSpamCheck(row)">
                  <i class="ri-refresh-line" />
                  <span>重试</span>
                </el-button>
              </template>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <template v-if="getKeepAliveState(row.key).status === 'valid'">
              <div class="state online">
                <i class="ri-checkbox-circle-fill" />
                <span>在线</span>
              </div>
            </template>
            <template v-else-if="getKeepAliveState(row.key).status === 'invalid'">
              <div class="state offline">
                <i class="ri-close-circle-fill" />
                <span>离线</span>
              </div>
            </template>
            <template v-else-if="getKeepAliveState(row.key).status === 'checking'">
              <div class="state unknown">
                <i class="ri-loader-4-line spinning" />
                <span>检测中</span>
              </div>
            </template>
            <template v-else>
              <div class="state unknown">
                <i class="ri-question-fill" />
                <span>未检测</span>
              </div>
            </template>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <div class="actions" @click.stop>
              <el-tooltip content="删除" placement="top">
                <el-button text class="icon-btn danger" @click.stop="deleteAccount(row)">
                  <i class="ri-delete-bin-line" />
                </el-button>
              </el-tooltip>
              <el-tooltip content="退出" placement="top">
                <el-button text class="icon-btn" @click.stop="logoutAccount(row)">
                  <i class="ri-logout-box-r-line" />
                </el-button>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="detail-card">
      <div v-if="!selectedAccount" class="empty">
        <i class="ri-layout-row-line" />
        <div class="empty-title">请点击列表选择一个账号</div>
        <div class="empty-sub">下方将显示该账号的 Session 检测与详细信息</div>
      </div>

      <div v-else class="detail">
        <div class="detail-head">
          <div class="detail-acct">
            <el-avatar :size="40" :src="selectedAccount.avatar" class="avatar">
              {{ avatarText(selectedAccount) }}
            </el-avatar>
            <div class="meta">
              <div class="meta-top">
                <span class="name">{{ displayName(selectedAccount) }}</span>
                <span class="chip mono">{{ sessionFileName(selectedAccount.key) }}</span>
              </div>
              <div class="meta-sub">
                <span class="muted mono">Key: {{ selectedAccount.key }}</span>
                <span class="muted mono" v-if="selectedAccount.user_id">· UID: {{ selectedAccount.user_id }}</span>
              </div>
            </div>
          </div>

          <el-button class="btn keepalive-btn" @click="checkKeepAliveForSelected">
            <i class="ri-pulse-line" />
            检测 Session 有效性
          </el-button>
        </div>

        <div
          class="keepalive"
          :class="{
            valid: getKeepAliveState(selectedAccount.key).status === 'valid',
            invalid: getKeepAliveState(selectedAccount.key).status === 'invalid',
            checking: getKeepAliveState(selectedAccount.key).status === 'checking',
          }"
        >
          <template v-if="getKeepAliveState(selectedAccount.key).status === 'idle'">
            <i class="ri-pulse-line" />
            <div class="ka-text">
              <div class="ka-title">Session 未检测</div>
              <div class="ka-sub">点击上方按钮开始检测</div>
            </div>
          </template>
          <template v-else-if="getKeepAliveState(selectedAccount.key).status === 'checking'">
            <i class="ri-loader-4-line spinning" />
            <div class="ka-text">
              <div class="ka-title">检测中...</div>
              <div class="ka-sub">正在请求后端，请稍候</div>
            </div>
          </template>
          <template v-else-if="getKeepAliveState(selectedAccount.key).status === 'valid'">
            <i class="ri-signal-wifi-fill" />
            <div class="ka-text">
              <div class="ka-title">Session 有效</div>
              <div class="ka-sub">
                最近检测：{{ fmtTime(getKeepAliveState(selectedAccount.key).checked_at) }}
              </div>
            </div>
          </template>
          <template v-else>
            <i class="ri-alert-line" />
            <div class="ka-text">
              <div class="ka-title">Session 失效</div>
              <div class="ka-sub">
                最近检测：{{ fmtTime(getKeepAliveState(selectedAccount.key).checked_at) }}
                <span v-if="getKeepAliveState(selectedAccount.key).error"> · {{ getKeepAliveState(selectedAccount.key).error }}</span>
              </div>
            </div>
          </template>
        </div>

        <el-descriptions
          v-if="getKeepAliveState(selectedAccount.key).detail"
          class="desc"
          :column="2"
          size="small"
          border
        >
          <el-descriptions-item label="用户ID" label-align="right">
            <span class="mono">{{ getKeepAliveState(selectedAccount.key).detail?.user_id ?? '-' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="DC" label-align="right">
            <span class="mono">
              {{ getKeepAliveState(selectedAccount.key).detail?.dc_id ?? '-' }}
              <template v-if="getKeepAliveState(selectedAccount.key).detail?.nearest_dc">
                / {{ getKeepAliveState(selectedAccount.key).detail?.nearest_dc }}
              </template>
              <template v-if="getKeepAliveState(selectedAccount.key).detail?.country">
                ({{ getKeepAliveState(selectedAccount.key).detail?.country }})
              </template>
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="限制" label-align="right">
            <span class="mono">{{ getKeepAliveState(selectedAccount.key).detail?.restricted ?? '-' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="Session 更新时间" label-align="right">
            <span class="mono">{{ getKeepAliveState(selectedAccount.key).detail?.session_updated_at ?? '-' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="Meta 更新时间" label-align="right">
            <span class="mono">{{ getKeepAliveState(selectedAccount.key).detail?.meta_updated_at ?? '-' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="文件" label-align="right">
            <span class="mono">{{ getKeepAliveState(selectedAccount.key).detail?.session_file ?? '-' }}</span>
          </el-descriptions-item>
        </el-descriptions>
      </div>
    </div>

    <el-dialog v-model="addDialogOpen" title="添加账号" width="720px" class="bt-dialog" :close-on-click-modal="false">
      <el-tabs v-model="addTab">
        <el-tab-pane label="扫码登录" name="qr">
          <div class="add-box">
            <div class="add-tip">
              <i class="ri-qr-code-line" />
              <span>使用 Telegram 手机端扫码登录（设置 → 设备 → 扫码登录）</span>
            </div>

            <div class="qr-row">
              <div class="qr-box">
                <img v-if="qrState?.image" :src="qrState.image" alt="qr" class="qr-img" />
                <div v-else class="qr-wait">
                  <span class="muted">{{ qrWorking ? '正在生成二维码...' : '暂无二维码' }}</span>
                </div>
              </div>

              <div class="qr-side">
                <div class="kv">
                  <span class="k">状态</span>
                  <span class="v">{{ qrStatusText }}</span>
                </div>
                <div class="kv" v-if="qrExpiresLeftText">
                  <span class="k">剩余</span>
                  <span class="v mono">{{ qrExpiresLeftText }}</span>
                </div>
                <div class="kv" v-if="qrSessionId">
                  <span class="k">SessionID</span>
                  <span class="v mono">{{ qrSessionId }}</span>
                </div>

                <div v-if="qrState?.error" class="err">
                  <i class="ri-error-warning-line" />
                  <span>{{ qrState.error }}</span>
                </div>

                <div class="qr-actions">
                  <el-button type="primary" class="btn primary" :loading="qrWorking" @click="startQR">
                    <i class="ri-qr-code-line" />
                    生成二维码
                  </el-button>
                  <el-button class="btn" :disabled="!qrWorking" @click="stopQRPoll">
                    <i class="ri-stop-circle-line" />
                    停止
                  </el-button>
                </div>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane label="验证码登录" name="code">
          <div class="add-box">
            <div class="add-tip">
              <i class="ri-message-3-line" />
              <span>输入手机号发送验证码；如需二级密码会自动弹窗提示</span>
            </div>

            <el-form label-width="110px" class="code-form">
              <el-form-item label="手机号">
                <el-input v-model="codePhone" placeholder="例如：+8613800138000" style="max-width: 360px" />
              </el-form-item>

              <el-form-item>
                <div class="row">
                  <el-button type="primary" class="btn primary" :loading="codeWorking" @click="startCode">
                    <i class="ri-send-plane-line" />
                    发送验证码
                  </el-button>
                  <el-button class="btn" :disabled="!codeWorking" @click="stopCodePoll">
                    <i class="ri-stop-circle-line" />
                    停止
                  </el-button>
                  <span class="muted">状态：{{ codeStatusText }}</span>
                </div>
              </el-form-item>

              <el-form-item v-if="codeState?.status === 'need_code'" label="验证码">
                <div class="row">
                  <el-input v-model="smsCode" placeholder="6 位数字" style="max-width: 220px" />
                  <el-button type="success" class="btn" @click="submitSMSCode">
                    <i class="ri-check-line" />
                    提交
                  </el-button>
                </div>
              </el-form-item>
            </el-form>

            <div v-if="codeState?.error" class="err">
              <i class="ri-error-warning-line" />
              <span>{{ codeState.error }}</span>
            </div>

            <div v-if="codeSessionId" class="muted mono small">SessionID：{{ codeSessionId }}</div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>

    <el-dialog
      v-model="passwordDialogOpen"
      :title="passwordTarget === 'qr' ? '扫码登录二级密码' : '二级密码'"
      width="420px"
      class="bt-dialog"
      :close-on-click-modal="false"
    >
      <el-input v-model="passwordInput" type="password" show-password placeholder="请输入 Telegram 2FA 密码" />
      <template #footer>
        <div class="row">
          <el-button class="btn" @click="closePasswordDialog">取消</el-button>
          <el-button type="primary" class="btn primary" :loading="passwordSubmitting" @click="submit2FAPassword">
            <i class="ri-lock-password-line" />
            提交
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
:global(:root) {
  --am-bg: transparent;
  --am-panel: var(--tgv-panel-bg);
  --am-panel2: var(--tgv-panel-soft-bg);
  --am-border: var(--tgv-border);
  --am-text: var(--el-text-color-primary);
  --am-muted: var(--el-text-color-secondary);
  --am-muted2: var(--tgv-text-soft);
  --am-ok: var(--el-color-success);
  --am-bad: var(--el-color-danger);
  --am-input-bg: var(--tgv-panel-soft-bg);
  --am-mask: rgba(255, 255, 255, 0.68);
  --am-hover: color-mix(in srgb, var(--el-color-primary) 8%, transparent);
  --am-selected: color-mix(in srgb, var(--el-color-primary) 12%, transparent);
  --am-shadow: rgba(0, 21, 41, 0.08);
  --am-chip-bg: var(--tgv-panel-muted-bg);
  --am-chip-border: var(--tgv-border-soft);
  --am-chip-text: var(--el-text-color-regular);
  --am-qr-bg: var(--tgv-panel-soft-bg);
  --am-qr-border: var(--tgv-border);
  --am-danger-hover-bg: color-mix(in srgb, var(--el-color-danger) 10%, transparent);
}

:global(html.dark) {
  --am-bg: transparent;
  --am-panel: var(--tgv-panel-bg);
  --am-panel2: var(--tgv-panel-soft-bg);
  --am-border: var(--tgv-border);
  --am-text: var(--el-text-color-primary);
  --am-muted: var(--el-text-color-secondary);
  --am-muted2: var(--tgv-text-soft);
  --am-ok: var(--el-color-success);
  --am-bad: var(--el-color-danger);
  --am-input-bg: var(--tgv-panel-soft-bg);
  --am-mask: rgba(0, 0, 0, 0.35);
  --am-hover: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
  --am-selected: color-mix(in srgb, var(--el-color-primary) 14%, transparent);
  --am-shadow: rgba(0, 0, 0, 0.35);
  --am-chip-bg: var(--tgv-panel-muted-bg);
  --am-chip-border: var(--tgv-border-soft);
  --am-chip-text: var(--el-text-color-regular);
  --am-qr-bg: var(--tgv-panel-soft-bg);
  --am-qr-border: var(--tgv-border);
  --am-danger-hover-bg: color-mix(in srgb, var(--el-color-danger) 12%, transparent);
}

.account-page {
  background: var(--am-bg);
  border: 0;
  border-radius: 0;
  padding: 0;
  color: var(--am-text);
}

.header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 700;

  i {
    font-size: 18px;
    color: var(--am-text);
  }
}

.sub {
  display: flex;
  gap: 6px;
  align-items: center;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;

  i {
    font-size: 16px;
  }
}

.primary i {
  color: currentColor;
}

.muted {
  color: var(--am-muted);
}

.muted-icon {
  color: var(--am-muted2);
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.small {
  font-size: 12px;
}

.spinning {
  animation: spin 0.9s linear infinite;
}

.table-card {
  background: var(--am-panel);
  border: 1px solid var(--am-border);
  border-radius: var(--tgv-card-radius);
  overflow: hidden;
  transition: box-shadow 0.35s ease, border-color 0.35s ease;

  &.pulse {
    border-color: rgba(64, 158, 255, 0.35);
    box-shadow: 0 0 0 1px rgba(64, 158, 255, 0.12), 0 12px 32px var(--am-shadow);
  }
}

.acct-info {
  display: flex;
  align-items: center;
  gap: 10px;
}

.meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.meta-top {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.name {
  font-weight: 650;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.chip {
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--am-chip-border);
  color: var(--am-chip-text);
  background: var(--am-chip-bg);
  flex: none;
}

.meta-sub {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

.session {
  color: var(--am-text);
  opacity: 0.85;
}

.spam {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  user-select: none;

  .spam-btn {
    margin-left: 4px;
    padding: 0;

    :deep(.el-button) {
      padding: 0;
    }

    i {
      margin-right: 4px;
    }
  }
}

.ok {
  color: var(--am-ok);
}

.bad {
  color: var(--am-bad);
}

.state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;

  &.online {
    color: var(--am-ok);
  }
  &.offline {
    color: var(--am-bad);
  }
  &.unknown {
    color: var(--el-text-color-secondary);
  }
}

.actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.icon-btn {
  padding: 6px 8px;
  color: var(--am-chip-text);

  &:hover {
    color: var(--am-text);
    background: var(--am-hover);
  }

  &.danger {
    color: var(--am-bad);
    opacity: 0.85;
  }
  &.danger:hover {
    background: var(--am-danger-hover-bg);
    color: var(--am-bad);
    opacity: 1;
  }
}

.detail-card {
  margin-top: 12px;
  background: var(--am-panel);
  border: 1px solid var(--am-border);
  border-radius: var(--tgv-card-radius);
  padding: 14px;
}

.empty {
  min-height: 140px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 8px;
  text-align: center;
  color: var(--am-muted);

  i {
    font-size: 22px;
    color: var(--am-muted2);
  }
}

.empty-title {
  font-weight: 650;
}

.empty-sub {
  color: var(--am-muted2);
  font-size: 12px;
}

.detail {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.detail-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.detail-acct {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 260px;
}

.keepalive-btn {
  background: var(--am-chip-bg);
  border: 1px solid var(--am-border);
  color: var(--am-text);
}

.keepalive {
  display: flex;
  align-items: center;
  gap: 10px;
  border-radius: var(--tgv-card-radius);
  padding: 12px 12px;
  border: 1px solid var(--am-border);
  background: var(--am-panel2);

  i {
    font-size: 20px;
  }

  &.valid {
    border-color: rgba(103, 194, 58, 0.35);
    background: rgba(103, 194, 58, 0.12);
    i {
      color: var(--am-ok);
    }
  }

  &.invalid {
    border-color: rgba(255, 77, 79, 0.38);
    background: rgba(255, 77, 79, 0.12);
    i {
      color: var(--am-bad);
    }
  }
}

.ka-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.ka-title {
  font-weight: 700;
}

.ka-sub {
  font-size: 12px;
  opacity: 0.85;
}

.desc {
  :deep(.el-descriptions__cell) {
    background: var(--am-panel2);
    border-color: var(--am-border);
  }
  :deep(.el-descriptions__label) {
    color: var(--am-muted);
  }
  :deep(.el-descriptions__content) {
    color: var(--am-text);
  }
}

.add-box {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.add-tip {
  display: flex;
  gap: 8px;
  align-items: center;
  color: var(--am-muted);
  background: var(--am-panel2);
  border: 1px solid var(--am-border);
  border-radius: var(--tgv-card-radius);
  padding: 10px 12px;

  i {
    font-size: 18px;
    color: var(--am-muted);
  }
}

.qr-row {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 16px;
  align-items: start;
}

.qr-box {
  width: 260px;
  height: 260px;
  border-radius: var(--tgv-card-radius);
  border: 1px solid var(--am-qr-border);
  background: var(--am-qr-bg);
  display: flex;
  align-items: center;
  justify-content: center;
}

.qr-img {
  width: 248px;
  height: 248px;
  border-radius: 12px;
  background: #fff;
}

.qr-wait {
  padding: 12px;
  text-align: center;
}

.qr-side {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.kv {
  display: flex;
  gap: 10px;
  align-items: baseline;

  .k {
    width: 72px;
    color: var(--am-muted2);
    font-size: 12px;
  }
  .v {
    color: var(--am-text);
  }
}

.qr-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 4px;
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.code-form {
  margin-top: 6px;
}

.err {
  display: flex;
  gap: 6px;
  align-items: center;
  color: rgba(255, 77, 79, 0.95);
  background: rgba(255, 77, 79, 0.08);
  border: 1px solid rgba(255, 77, 79, 0.18);
  border-radius: 10px;
  padding: 10px 12px;
}

:deep(.el-dialog) {
  background: var(--am-panel);
  border: 1px solid var(--am-border);
}

:deep(.el-dialog__title) {
  color: var(--am-text);
}

:deep(.el-tabs__item) {
  color: var(--am-muted);
}

:deep(.el-tabs__item.is-active) {
  color: var(--am-text);
}

:deep(.el-form-item__label) {
  color: var(--am-muted);
}

:deep(.el-input__wrapper) {
  background: var(--am-input-bg);
  box-shadow: 0 0 0 1px var(--am-border) inset;
}

:deep(.el-input__inner) {
  color: var(--am-text);
}

:deep(.el-input__inner::placeholder) {
  color: var(--am-muted2);
}

:deep(.acct-table) {
  --el-table-bg-color: transparent;
  --el-table-tr-bg-color: transparent;
  --el-table-header-bg-color: var(--am-panel2);
  --el-table-border-color: var(--am-border);
  --el-table-text-color: var(--am-text);
  --el-table-header-text-color: var(--am-muted);
  --el-table-row-hover-bg-color: var(--am-hover);
}

:deep(.el-table) {
  background: transparent;
}

:deep(.el-table th.el-table__cell) {
  background: var(--am-panel2);
}

:deep(.el-table td.el-table__cell) {
  border-bottom: 1px solid var(--am-border);
}

:deep(.el-table__row.is-selected > td.el-table__cell) {
  background: var(--am-selected);
}

:deep(.el-loading-mask) {
  background: var(--am-mask);
}

@keyframes spin {
  0% {
    transform: rotate(0);
  }
  100% {
    transform: rotate(360deg);
  }
}
</style>
