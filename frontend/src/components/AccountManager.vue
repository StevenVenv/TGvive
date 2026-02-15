<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import {
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
  type TGAccount,
} from '../api'

const accounts = ref<TGAccount[]>([])
const loadingAccounts = ref(false)

async function reloadAccounts() {
  loadingAccounts.value = true
  try {
    accounts.value = await listTGAccounts()
  } catch (err: any) {
    ElMessage.error(err?.message || '加载账号失败')
  } finally {
    loadingAccounts.value = false
  }
}

function fmtTime(unix: number): string {
  if (!unix) return '-'
  return new Date(unix * 1000).toLocaleString()
}

function fmtBytes(size: number): string {
  if (!Number.isFinite(size) || size <= 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  let n = size
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function avatarText(a: TGAccount): string {
  const name = (a.name || '').trim()
  if (name) return name.slice(0, 1).toUpperCase()
  const u = (a.username || '').trim()
  if (u) return u.slice(0, 1).toUpperCase()
  if (a.user_id) return String(a.user_id).slice(-2)
  return 'TG'
}

function accountTitle(a: TGAccount): string {
  return (a.name || (a.username ? `@${a.username}` : '') || (a.user_id ? `ID:${a.user_id}` : '') || a.key).trim()
}

async function removeAccount(a: TGAccount) {
  const title = accountTitle(a)
  try {
    await ElMessageBox.confirm(`确定退出账号 ${title} 吗？\n（将删除本地 sessions/session_*.json，会导致相关任务无法继续运行）`, '确认', {
      confirmButtonText: '退出',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await deleteTGAccount(a.key)
    ElMessage.success('账号已退出')
    await reloadAccounts()
  } catch (err: any) {
    ElMessage.error(err?.message || '退出账号失败')
  }
}

const tab = ref<'qr' | 'code'>('qr')

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

// QR login
const qrSessionId = ref('')
const qrState = ref<QRState | null>(null)
const qrWorking = ref(false)
const qrDialogOpen = ref(false)
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

const qrStatusTagType = computed(() => {
  switch (qrState.value?.status) {
    case 'authorized':
      return 'success'
    case 'scanned':
      return 'warning'
    case 'need_password':
      return 'warning'
    case 'expired':
      return 'warning'
    case 'error':
      return 'danger'
    default:
      return 'info'
  }
})

const qrProgressPct = computed(() => {
  switch (qrState.value?.status) {
    case 'created':
      return 10
    case 'pending':
      return 35
    case 'scanned':
      return 70
    case 'need_password':
      return 85
    case 'authorized':
      return 100
    case 'expired':
    case 'error':
      return 0
    default:
      return 0
  }
})

const qrProgressStatus = computed(() => {
  switch (qrState.value?.status) {
    case 'authorized':
      return 'success'
    case 'error':
      return 'exception'
    case 'expired':
      return 'warning'
    case 'need_password':
      return 'warning'
    default:
      return undefined
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
      qrDialogOpen.value = false
      if (passwordTarget.value === 'qr') closePasswordDialog()
      ElMessage.success('扫码登录成功')
      await reloadAccounts()
    } else if (st.status === 'expired' || st.status === 'error') {
      stopQRPoll()
    }
  } catch {
    // best-effort
  }
}

async function startQR() {
  qrDialogOpen.value = true
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
    qrDialogOpen.value = false
    ElMessage.error(err?.message || '启动扫码登录失败')
  }
}

function onQRDialogClose() {
  stopQRPoll()
}

async function copyText(text: string) {
  text = (text || '').trim()
  if (!text) return

  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
    return
  } catch {
    // fallback
  }

  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', 'true')
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    ta.style.top = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (ok) {
      ElMessage.success('已复制')
    } else {
      ElMessage.error('复制失败')
    }
  } catch {
    ElMessage.error('复制失败')
  }
}

async function copyQRUrl() {
  if (!qrState.value?.url) return
  await copyText(qrState.value.url)
}

function openQRUrl() {
  const url = (qrState.value?.url || '').trim()
  if (!url) return
  window.open(url, '_blank')
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
      if (passwordTarget.value === 'code') closePasswordDialog()
      ElMessage.success('验证码登录成功')
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
  <div class="account-manager">
    <div class="toolbar">
      <el-button type="primary" @click="reloadAccounts" :loading="loadingAccounts">刷新账号</el-button>
      <el-text type="info">共 {{ accounts.length }} 个账号</el-text>
    </div>

    <el-table :data="accounts" v-loading="loadingAccounts" stripe style="width: 100%">
      <el-table-column label="账号" min-width="260">
        <template #default="{ row }">
          <div class="acct-row">
            <el-avatar :size="36" :src="row.avatar">
              {{ avatarText(row) }}
            </el-avatar>
            <div class="acct">
              <el-space>
                <el-text>{{ row.name || (row.username ? `@${row.username}` : '-') }}</el-text>
                <el-tag size="small" type="success">已登录</el-tag>
              </el-space>
              <el-text type="info">
                ID: {{ row.user_id ?? '-' }}<span v-if="row.phone"> · {{ row.phone }}</span>
              </el-text>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="更新时间" width="200">
        <template #default="{ row }">
          <el-text>{{ fmtTime(row.updated_at) }}</el-text>
        </template>
      </el-table-column>
      <el-table-column label="大小" width="140">
        <template #default="{ row }">
          <el-text type="info">{{ fmtBytes(row.size) }}</el-text>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="120" fixed="right">
        <template #default="{ row }">
          <el-button size="small" type="danger" @click="removeAccount(row)">退出</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-divider />

    <el-tabs v-model="tab">
      <el-tab-pane label="扫码登录" name="qr">
        <el-form label-width="110px" class="form">
          <el-form-item>
            <el-space>
              <el-button type="primary" :loading="qrWorking" @click="startQR">扫码登录</el-button>
              <el-text type="info">状态：{{ qrStatusText }}</el-text>
            </el-space>
          </el-form-item>
        </el-form>

        <el-dialog
          v-model="qrDialogOpen"
          title="扫码登录"
          width="420px"
          :close-on-click-modal="false"
          @close="onQRDialogClose"
        >
          <div class="qr-dialog">
            <el-alert type="info" :closable="false" show-icon>
              使用 Telegram 手机端扫码登录（设置 → 设备 → 扫码登录）。二维码过期后可点击「重新生成」。
            </el-alert>

            <div class="qr-status">
              <el-space wrap>
                <el-text type="info">状态：</el-text>
                <el-tag size="small" :type="qrStatusTagType">{{ qrStatusText }}</el-tag>
                <el-progress
                  class="qr-progress"
                  :percentage="qrProgressPct"
                  :status="qrProgressStatus"
                  :stroke-width="10"
                />
                <el-text v-if="qrExpiresLeftText" type="info">剩余：{{ qrExpiresLeftText }}</el-text>
              </el-space>
            </div>

            <div v-if="qrState?.error" class="err">
              <el-text type="danger">{{ qrState.error }}</el-text>
            </div>

            <div class="qr">
              <div class="qr-box">
                <img v-if="qrState?.image" :src="qrState.image" alt="qr" class="qr-img" />
                <div v-else class="qr-wait">
                  <el-text type="info">{{ qrWorking ? '正在生成二维码...' : '暂无二维码' }}</el-text>
                </div>
              </div>

              <div v-if="qrState?.url && !qrState?.image" class="qr-url">
                <el-text type="info">URL：</el-text>
                <el-text>{{ qrState.url }}</el-text>
              </div>

              <el-space wrap class="qr-actions">
                <el-button size="small" @click="copyQRUrl" :disabled="!qrState?.url">复制链接</el-button>
                <el-button size="small" @click="openQRUrl" :disabled="!qrState?.url">打开链接</el-button>
              </el-space>

              <div class="qr-meta">
                <el-text type="info">SessionID：{{ qrState?.session_id || qrSessionId }}</el-text>
                <el-text v-if="qrState?.expires_at" type="info">过期：{{ fmtTime(qrState.expires_at) }}</el-text>
              </div>
            </div>
          </div>

          <template #footer>
            <el-space>
              <el-button @click="qrDialogOpen = false">关闭</el-button>
              <el-button type="warning" @click="stopQRPoll" :disabled="!qrWorking">停止</el-button>
              <el-button type="primary" @click="startQR" :loading="qrWorking" :disabled="qrWorking">重新生成</el-button>
            </el-space>
          </template>
        </el-dialog>
      </el-tab-pane>

      <el-tab-pane label="验证码登录" name="code">
        <el-form label-width="110px" class="form">
          <el-form-item label="手机号">
            <el-input v-model="codePhone" placeholder="例如：+8613800138000" style="max-width: 360px" />
          </el-form-item>

          <el-form-item>
            <el-space>
              <el-button type="primary" :loading="codeWorking" @click="startCode">发送验证码</el-button>
              <el-button @click="stopCodePoll" :disabled="!codeWorking">停止</el-button>
              <el-text type="info">状态：{{ codeStatusText }}</el-text>
            </el-space>
          </el-form-item>

          <el-form-item v-if="codeState?.status === 'need_code'" label="验证码">
            <el-space>
              <el-input v-model="smsCode" placeholder="6 位数字" style="max-width: 200px" />
              <el-button type="success" @click="submitSMSCode">提交</el-button>
            </el-space>
          </el-form-item>
        </el-form>

        <div v-if="codeState?.error" class="err">
          <el-text type="danger">{{ codeState.error }}</el-text>
        </div>

        <el-text v-if="codeSessionId" type="info">SessionID：{{ codeSessionId }}</el-text>
      </el-tab-pane>
    </el-tabs>

    <el-dialog
      v-model="passwordDialogOpen"
      :title="passwordTarget === 'qr' ? '扫码登录二级密码' : '二级密码'"
      width="420px"
      :close-on-click-modal="false"
    >
      <el-input v-model="passwordInput" type="password" show-password placeholder="请输入 Telegram 2FA 密码" />
      <template #footer>
        <el-space>
          <el-button @click="closePasswordDialog">取消</el-button>
          <el-button type="primary" :loading="passwordSubmitting" @click="submit2FAPassword">提交</el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.form {
  max-width: 720px;
}

.acct {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.acct-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.qr-dialog {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.qr-status {
  margin-top: 2px;
}

.qr {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.qr-box {
  width: 260px;
  height: 260px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.qr-wait {
  width: 260px;
  height: 260px;
  border-radius: 10px;
  border: 1px dashed var(--el-border-color);
  background: var(--el-bg-color);
  display: flex;
  align-items: center;
  justify-content: center;
}

.qr-img {
  width: 260px;
  height: 260px;
  border-radius: 10px;
  border: 1px solid var(--el-border-color);
  background: #fff;
}

.qr-actions {
  margin-top: 2px;
}

.qr-progress {
  width: 180px;
}

.qr-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.err {
  margin: 8px 0;
}
</style>
