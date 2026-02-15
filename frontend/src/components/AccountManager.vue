<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import {
  getAccountQRStatus,
  getCodeLoginStatus,
  listTGAccounts,
  startAccountQR,
  startCodeLogin,
  submitCode,
  submitPassword,
  type CodeAuthState,
  type QRState,
  type TGAccount,
} from '../api'

const props = defineProps<{
  token: string
}>()

const accounts = ref<TGAccount[]>([])
const loadingAccounts = ref(false)

async function reloadAccounts() {
  if (!props.token.trim()) {
    accounts.value = []
    return
  }

  loadingAccounts.value = true
  try {
    accounts.value = await listTGAccounts(props.token)
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

const tab = ref<'qr' | 'code'>('qr')

// QR login
const qrKey = ref('')
const qrSessionId = ref('')
const qrState = ref<QRState | null>(null)
const qrWorking = ref(false)
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

function stopQRPoll() {
  qrWorking.value = false
  if (qrTimer) {
    window.clearInterval(qrTimer)
    qrTimer = undefined
  }
}

async function pollQROnce() {
  if (!props.token.trim() || !qrSessionId.value) return
  try {
    const st = await getAccountQRStatus(props.token, qrSessionId.value)
    qrState.value = st

    if (st.status === 'authorized') {
      stopQRPoll()
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
  if (!props.token.trim()) {
    ElMessage.warning('请先填写 Token')
    return
  }
  const key = qrKey.value.trim()
  if (!key) {
    ElMessage.warning('请输入 SessionKey')
    return
  }

  qrWorking.value = true
  qrState.value = null
  try {
    const { session_id } = await startAccountQR(props.token, key)
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
const codeKey = ref('')
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

const passwordDialogOpen = ref(false)
const passwordInput = ref('')
const passwordSubmitting = ref(false)

function stopCodePoll() {
  codeWorking.value = false
  if (codeTimer) {
    window.clearInterval(codeTimer)
    codeTimer = undefined
  }
}

async function pollCodeOnce() {
  if (!props.token.trim() || !codeSessionId.value) return
  try {
    const st = await getCodeLoginStatus(props.token, codeSessionId.value)
    codeState.value = st

    if (st.status === 'need_password') {
      passwordDialogOpen.value = true
    }

    if (st.status === 'authorized') {
      stopCodePoll()
      passwordDialogOpen.value = false
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
  if (!props.token.trim()) {
    ElMessage.warning('请先填写 Token')
    return
  }
  const key = codeKey.value.trim()
  const phone = codePhone.value.trim()
  if (!key) {
    ElMessage.warning('请输入 SessionKey')
    return
  }
  if (!phone) {
    ElMessage.warning('请输入手机号（含国家码）')
    return
  }

  codeWorking.value = true
  codeState.value = null
  smsCode.value = ''
  try {
    const { session_id } = await startCodeLogin(props.token, key, phone)
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
  if (!props.token.trim()) return
  if (!codeSessionId.value) return
  const code = smsCode.value.trim()
  if (!code) {
    ElMessage.warning('请输入验证码')
    return
  }
  try {
    await submitCode(props.token, codeSessionId.value, code)
    ElMessage.success('验证码已提交')
    await pollCodeOnce()
  } catch (err: any) {
    ElMessage.error(err?.message || '提交验证码失败')
  }
}

async function submit2FAPassword() {
  if (!props.token.trim()) return
  if (!codeSessionId.value) return
  const p = passwordInput.value.trim()
  if (!p) {
    ElMessage.warning('请输入二级密码')
    return
  }

  passwordSubmitting.value = true
  try {
    await submitPassword(props.token, codeSessionId.value, p)
    ElMessage.success('二级密码已提交')
    passwordDialogOpen.value = false
    passwordInput.value = ''
    await pollCodeOnce()
  } catch (err: any) {
    ElMessage.error(err?.message || '提交二级密码失败')
  } finally {
    passwordSubmitting.value = false
  }
}

watch(
  () => props.token,
  async () => {
    stopQRPoll()
    stopCodePoll()
    qrState.value = null
    codeState.value = null
    await reloadAccounts()
  },
)

onMounted(async () => {
  await reloadAccounts()
})

onBeforeUnmount(() => {
  stopQRPoll()
  stopCodePoll()
})
</script>

<template>
  <div class="account-manager">
    <div class="toolbar">
      <el-button type="primary" @click="reloadAccounts" :loading="loadingAccounts">刷新账号</el-button>
      <el-text type="info">共 {{ accounts.length }} 个账号</el-text>
    </div>

    <el-table :data="accounts" v-loading="loadingAccounts" stripe style="width: 100%">
      <el-table-column prop="key" label="SessionKey" min-width="220" />
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
    </el-table>

    <el-divider />

    <el-tabs v-model="tab">
      <el-tab-pane label="扫码登录" name="qr">
        <el-form label-width="110px" class="form">
          <el-form-item label="SessionKey">
            <el-input v-model="qrKey" placeholder="例如：acc_main / 13800138000" style="max-width: 360px" />
          </el-form-item>

          <el-form-item>
            <el-space>
              <el-button type="primary" :loading="qrWorking" @click="startQR">开始扫码</el-button>
              <el-button @click="stopQRPoll" :disabled="!qrWorking">停止</el-button>
              <el-text type="info">状态：{{ qrStatusText }}</el-text>
            </el-space>
          </el-form-item>
        </el-form>

        <div v-if="qrState?.error" class="err">
          <el-text type="danger">{{ qrState.error }}</el-text>
        </div>

        <div v-if="qrState?.image || qrState?.url" class="qr">
          <img v-if="qrState?.image" :src="qrState.image" alt="qr" class="qr-img" />
          <div v-else class="qr-url">
            <el-text type="info">URL：</el-text>
            <el-text>{{ qrState?.url }}</el-text>
          </div>
          <div class="qr-meta">
            <el-text type="info">SessionID：{{ qrState?.session_id }}</el-text>
            <el-text v-if="qrState?.expires_at" type="info">过期：{{ fmtTime(qrState.expires_at) }}</el-text>
          </div>
        </div>
      </el-tab-pane>

      <el-tab-pane label="验证码登录" name="code">
        <el-form label-width="110px" class="form">
          <el-form-item label="SessionKey">
            <el-input v-model="codeKey" placeholder="例如：acc_main / 13800138000" style="max-width: 360px" />
          </el-form-item>
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

    <el-dialog v-model="passwordDialogOpen" title="二级密码" width="420px" :close-on-click-modal="false">
      <el-input v-model="passwordInput" type="password" show-password placeholder="请输入 Telegram 2FA 密码" />
      <template #footer>
        <el-space>
          <el-button @click="passwordDialogOpen = false">取消</el-button>
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

.qr {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.qr-img {
  width: 260px;
  height: 260px;
  border-radius: 10px;
  border: 1px solid #ebeef5;
  background: #fff;
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

