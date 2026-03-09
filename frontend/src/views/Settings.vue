<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getProxyConfig, testProxyConfig, updateAccountSettings, updateProxyConfig, type AuthUser } from '../api'

type ProxyType = 'http' | 'socks5'
type ProxyConfig = {
  enabled: boolean
  type: ProxyType
  host: string
  port: number
  username?: string
  password?: string
}

const props = defineProps<{
  user?: AuthUser | null
}>()

const emit = defineEmits<{
  (e: 'account-updated', user: AuthUser): void
}>()

const proxyForm = reactive<ProxyConfig>({
  enabled: false,
  type: 'http',
  host: '127.0.0.1',
  port: 7890,
  username: '',
  password: '',
})

const accountForm = reactive({
  username: '',
  currentPassword: '',
  newPassword: '',
  confirmPassword: '',
})

const testing = ref(false)
const saving = ref(false)
const accountSaving = ref(false)
const lastPing = ref<number | null>(null)
const passwordSet = ref(false)
const passwordDirty = ref(false)
const suppressDirty = ref(false)

const endpoint = computed(() => {
  const host = (proxyForm.host || '').trim()
  const port = Number(proxyForm.port || 0)
  if (!host || !port) return '-'
  return `${host}:${port}`
})

const passwordPlaceholder = computed(() => (passwordSet.value ? '已设置（留空不修改）' : '可留空'))

const currentUsername = computed(() => String(props.user?.username || '').trim())

function syncAccountForm() {
  accountForm.username = currentUsername.value
}

function resetAccountForm() {
  syncAccountForm()
  accountForm.currentPassword = ''
  accountForm.newPassword = ''
  accountForm.confirmPassword = ''
}

function loadProxyConfig() {
  return getProxyConfig()
    .then((cfg) => {
      proxyForm.enabled = !!cfg?.enabled
      if (cfg?.type === 'http' || cfg?.type === 'socks5') proxyForm.type = cfg.type
      if (typeof cfg?.host === 'string') proxyForm.host = cfg.host
      if (typeof cfg?.port === 'number' && Number.isFinite(cfg.port)) proxyForm.port = cfg.port
      if (typeof cfg?.username === 'string') proxyForm.username = cfg.username

      passwordSet.value = !!cfg?.password_set
      suppressDirty.value = true
      proxyForm.password = ''
      suppressDirty.value = false
      passwordDirty.value = false
    })
    .catch(() => {
      // ignore
    })
}

function reload() {
  void loadProxyConfig()
  resetAccountForm()
  lastPing.value = null
}

defineExpose({ reload })

function validateProxy(): string | null {
  if (!proxyForm.enabled) return null
  const host = (proxyForm.host || '').trim()
  const port = Number(proxyForm.port || 0)
  if (!host) return '请填写代理服务器地址'
  if (!port || port < 1 || port > 65535) return '请填写有效代理端口 (1-65535)'
  return null
}

function validateAccount(): string | null {
  const username = String(accountForm.username || '').trim()
  const current = String(currentUsername.value || '').trim()
  const currentPassword = String(accountForm.currentPassword || '')
  const newPassword = String(accountForm.newPassword || '')
  const confirmPassword = String(accountForm.confirmPassword || '')

  if (!username) return '用户名不能为空'
  if (username === current && !newPassword) return '没有可保存的账号变更'
  if (!currentPassword.trim()) return '请输入当前密码'
  if (newPassword && newPassword.trim().length < 6) return '新密码长度不能少于 6 位'
  if (newPassword !== confirmPassword) return '两次输入的新密码不一致'
  return null
}

async function testConnection() {
  const err = validateProxy()
  if (err) {
    ElMessage.warning(err)
    return
  }
  if (!proxyForm.enabled) {
    lastPing.value = null
    ElMessage.info('当前为直连模式，无需测试代理')
    return
  }

  testing.value = true
  try {
    const res = await testProxyConfig({
      enabled: true,
      type: proxyForm.type,
      host: (proxyForm.host || '').trim(),
      port: Number(proxyForm.port || 0),
      username: (proxyForm.username || '').trim(),
      password: String(proxyForm.password || ''),
      timeout_ms: 8000,
    })
    lastPing.value = Math.max(0, Number(res?.ping_ms || 0) || 0)
    ElMessage.success(`连接成功：${endpoint.value} (${lastPing.value}ms)`)
  } catch {
    ElMessage.error('测试失败（请确认代理可用或鉴权信息正确）')
  } finally {
    testing.value = false
  }
}

async function saveProxyConfig() {
  const err = validateProxy()
  if (err) {
    ElMessage.warning(err)
    return
  }

  saving.value = true
  try {
    const payload: any = {
      enabled: proxyForm.enabled,
      type: proxyForm.type,
      host: (proxyForm.host || '').trim(),
      port: Number(proxyForm.port || 0),
      username: (proxyForm.username || '').trim(),
    }
    if (passwordDirty.value) payload.password = String(proxyForm.password || '')

    const saved = await updateProxyConfig(payload)
    passwordSet.value = !!saved?.password_set
    suppressDirty.value = true
    proxyForm.password = ''
    suppressDirty.value = false
    passwordDirty.value = false
    ElMessage.success('代理设置已保存并热生效')
  } catch {
    ElMessage.error('代理设置保存失败')
  } finally {
    saving.value = false
  }
}

async function saveAccount() {
  const err = validateAccount()
  if (err) {
    ElMessage.warning(err)
    return
  }

  accountSaving.value = true
  try {
    const user = await updateAccountSettings({
      username: String(accountForm.username || '').trim(),
      current_password: String(accountForm.currentPassword || ''),
      new_password: String(accountForm.newPassword || ''),
    })
    emit('account-updated', user)
    resetAccountForm()
    accountForm.username = user.username
    ElMessage.success('账号信息已更新')
  } catch (err: any) {
    ElMessage.error(String(err?.message || '账号信息保存失败'))
  } finally {
    accountSaving.value = false
  }
}

onMounted(() => {
  syncAccountForm()
  void loadProxyConfig()
})

watch(
  () => props.user?.username,
  () => {
    syncAccountForm()
  },
  { immediate: true },
)

watch(
  () => proxyForm.password,
  () => {
    if (suppressDirty.value) return
    passwordDirty.value = true
  },
)
</script>

<template>
  <div class="settings">
    <el-row :gutter="12">
      <el-col :xs="24" :xl="12">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-shield-user-line" />
                <span>账号安全</span>
              </div>
              <div class="card-sub">Account Security</div>
            </div>
          </template>

          <div class="status">
            <div class="status-left">
              <div class="status-title">当前登录账号</div>
              <div class="status-sub muted">{{ currentUsername || '-' }}</div>
            </div>
          </div>

          <div class="split-line" />

          <el-form label-position="top" class="form">
            <el-row :gutter="12">
              <el-col :xs="24" :sm="12">
                <el-form-item label="用户名">
                  <el-input v-model="accountForm.username" placeholder="请输入用户名" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="当前密码">
                  <el-input v-model="accountForm.currentPassword" type="password" show-password placeholder="用于确认修改" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="新密码">
                  <el-input v-model="accountForm.newPassword" type="password" show-password placeholder="留空则不修改密码" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="确认新密码">
                  <el-input v-model="accountForm.confirmPassword" type="password" show-password placeholder="再次输入新密码" />
                </el-form-item>
              </el-col>
            </el-row>

            <div class="actions">
              <el-button class="btn" @click="resetAccountForm">
                <i class="ri-eraser-line" />
                <span>重置</span>
              </el-button>
              <el-button type="primary" class="btn primary" @click="saveAccount" :loading="accountSaving">
                <i class="ri-save-3-line" />
                <span>保存账号</span>
              </el-button>
            </div>
          </el-form>
        </el-card>
      </el-col>

      <el-col :xs="24" :xl="12">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-route-line" />
                <span>网络代理</span>
              </div>
              <div class="card-sub">Network Proxy</div>
            </div>
          </template>

          <div class="status">
            <div class="status-left">
              <div class="status-title">程序网络代理</div>
              <div class="status-sub muted">
                当前：<b>{{ proxyForm.enabled ? proxyForm.type.toUpperCase() : 'DIRECT' }}</b>
                <span class="dot">·</span>
                <span>{{ proxyForm.enabled ? endpoint : '直连模式' }}</span>
                <span v-if="lastPing !== null" class="dot">·</span>
                <span v-if="lastPing !== null">{{ lastPing }}ms</span>
              </div>
            </div>
            <div class="status-right">
              <el-switch v-model="proxyForm.enabled" active-text="代理开启" inactive-text="直连" />
            </div>
          </div>

          <div class="split-line" />

          <el-form label-position="top" class="form">
            <el-row :gutter="12">
              <el-col :xs="24" :sm="12">
                <el-form-item label="代理类型">
                  <el-select v-model="proxyForm.type" placeholder="选择代理类型" style="width: 100%">
                    <el-option label="HTTP" value="http" />
                    <el-option label="SOCKS5" value="socks5" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="服务器地址 (Host)">
                  <el-input v-model="proxyForm.host" placeholder="例如：127.0.0.1" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="端口 (Port)">
                  <el-input v-model.number="proxyForm.port" placeholder="例如：7890" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="鉴权（可选）用户名">
                  <el-input v-model="proxyForm.username" placeholder="可留空" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="鉴权（可选）密码">
                  <el-input v-model="proxyForm.password" type="password" show-password :placeholder="passwordPlaceholder" />
                </el-form-item>
              </el-col>
            </el-row>

            <div class="actions">
              <el-button class="btn" @click="testConnection" :loading="testing">
                <i class="ri-pulse-line" />
                <span>测试连接</span>
              </el-button>
              <el-button type="primary" class="btn primary" @click="saveProxyConfig" :loading="saving">
                <i class="ri-save-3-line" />
                <span>保存代理</span>
              </el-button>
            </div>
          </el-form>
        </el-card>
      </el-col>

      <el-col :xs="24">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-information-line" />
                <span>说明</span>
              </div>
              <div class="card-sub">Notes</div>
            </div>
          </template>

          <div class="note">
            <div class="note-item">
              <div class="k muted">默认账号</div>
              <div class="v">数据库为空时，系统会自动创建默认账号 `admin / 123456`。首次登录后请立即在上方修改。</div>
            </div>
            <div class="note-item">
              <div class="k muted">Telegram 会话目录</div>
              <div class="v">账号会话文件固定保存在 `./sessions/`，不再通过 config.yaml 配置。</div>
            </div>
            <div class="note-item">
              <div class="k muted">代理设置</div>
              <div class="v">代理保存到后端运行时存储并立即热生效，不会写回 config.yaml。</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped lang="scss">
.settings {
  width: 100%;
}

.status {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.status-title {
  font-weight: 800;
}

.status-sub {
  margin-top: 4px;
  font-size: 12px;
}

.dot {
  margin: 0 6px;
  opacity: 0.7;
}

.actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.note {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}

.note-item {
  padding: 12px;
  border-radius: 12px;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-light);
}

.note-item .k {
  font-size: 12px;
}

.note-item .v {
  margin-top: 6px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
}
</style>
