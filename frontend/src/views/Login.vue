<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { getDevToken, login, type AuthUser } from '../api'
import { useBackendPing } from '../composables/useBackendPing'
import { useTheme } from '../composables/useTheme'
import {
  ensureBackendOrigin,
  getBackendOrigin,
  normalizeBackendOrigin,
  probeBackendOrigin,
  setBackendOrigin,
  suggestLocalBackendOrigin,
} from '../runtime/backend'

const emit = defineEmits<{
  (e: 'logged-in', user: AuthUser): void
}>()

const { theme } = useTheme()

const username = ref('admin')
const password = ref('')
const submitting = ref(false)

const backendInput = ref('')
const backendTesting = ref(false)

const { status: backendStatus, latencyMs } = useBackendPing({ intervalMs: 6000, timeoutMs: 1500 })

const backendLabel = computed(() => {
  const s = String(getBackendOrigin() || '').trim()
  return s ? s : '(同源 / 不覆盖)'
})

const backendBadge = computed(() => {
  if (backendStatus.value === 'online') {
    const ms = latencyMs.value
    return typeof ms === 'number' ? `ONLINE ${ms}ms` : 'ONLINE'
  }
  if (backendStatus.value === 'offline') return 'OFFLINE'
  return 'PING'
})

function initBackendInput() {
  backendInput.value = getBackendOrigin() || suggestLocalBackendOrigin(8081) || ''
}

async function autoDetectBackend() {
  backendTesting.value = true
  try {
    await ensureBackendOrigin()
    initBackendInput()
    ElMessage.success('已自动检测')
  } catch {
    ElMessage.error('自动检测失败')
  } finally {
    backendTesting.value = false
  }
}

async function testAndSaveBackend() {
  const norm = normalizeBackendOrigin(backendInput.value)
  if (!norm) {
    ElMessage.error('后端地址不合法（示例：http://localhost:8081）')
    return
  }

  backendTesting.value = true
  try {
    const ok = await probeBackendOrigin(norm, 1500)
    if (!ok) {
      ElMessage.error('连接失败：请确认后端已启动、端口正确、且后端允许该 Origin 的 CORS')
      return
    }
    setBackendOrigin(norm)
    ElMessage.success('已保存')
  } catch {
    ElMessage.error('连接失败')
  } finally {
    backendTesting.value = false
  }
}

function clearBackendOverride() {
  setBackendOrigin('')
  initBackendInput()
  ElMessage.success('已清除')
}

async function doLogin() {
  if (submitting.value) return
  const u = String(username.value || '').trim()
  const p = String(password.value || '')
  if (!u || !p.trim()) {
    ElMessage.warning('请输入用户名和密码')
    return
  }

  submitting.value = true
  try {
    const res = await login(u, p)
    if (!res?.user?.id) throw new Error('login failed')
    ElMessage.success('登录成功')
    emit('logged-in', res.user)
  } catch (e: any) {
    ElMessage.error(e?.message || '登录失败')
  } finally {
    submitting.value = false
  }
}

async function devLogin() {
  if (submitting.value) return
  const u = String(username.value || '').trim() || 'admin'
  submitting.value = true
  try {
    const res = await getDevToken(u)
    if (!res?.user?.id) throw new Error('dev token failed')
    ElMessage.success('已获取 dev token（仅 debug 模式可用）')
    emit('logged-in', res.user)
  } catch (e: any) {
    ElMessage.error(e?.message || 'dev 登录失败（可能不是 debug 模式）')
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  initBackendInput()
})
</script>

<template>
  <div class="login-page" :class="{ dark: theme === 'dark' }">
    <div class="login-card">
      <div class="brand">
        <div class="logo">TG</div>
        <div class="meta">
          <div class="title">TGvive</div>
          <div class="sub">Web 控制台登录</div>
        </div>
      </div>

      <div class="backend">
        <div class="backend-head">
          <div class="backend-title">
            <span>后端</span>
            <span class="badge" :class="backendStatus">{{ backendBadge }}</span>
          </div>
          <div class="backend-cur muted">当前：{{ backendLabel }}</div>
        </div>

        <el-input v-model="backendInput" placeholder="http://localhost:8081" clearable />
        <div class="backend-actions">
          <el-button size="small" @click="clearBackendOverride">清除</el-button>
          <el-button size="small" :loading="backendTesting" @click="autoDetectBackend">自动检测</el-button>
          <el-button size="small" type="primary" :loading="backendTesting" @click="testAndSaveBackend">测试并保存</el-button>
        </div>
        <div class="hint muted">前后端不同端口时（例如 python 起前端），需要在这里设置后端地址。</div>
      </div>

      <el-form @submit.prevent>
        <el-form-item label="用户名">
          <el-input v-model="username" autocomplete="username" @keyup.enter="doLogin" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="password" type="password" show-password autocomplete="current-password" @keyup.enter="doLogin" />
        </el-form-item>

        <div class="actions">
          <el-button type="primary" :loading="submitting" @click="doLogin">登录</el-button>
          <el-button :loading="submitting" @click="devLogin">Dev 登录</el-button>
        </div>
      </el-form>

      <div class="foot muted">
        <div>提示：首次启动会在后端日志输出 admin 初始密码（仅 debug）。</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: radial-gradient(1200px 600px at 30% 10%, rgba(64, 158, 255, 0.16), transparent 60%),
    radial-gradient(1000px 600px at 80% 80%, rgba(103, 194, 58, 0.14), transparent 55%),
    linear-gradient(180deg, rgba(255, 255, 255, 1), rgba(245, 247, 250, 1));
}

html.dark .login-page,
.login-page.dark {
  background: radial-gradient(1200px 600px at 30% 10%, rgba(64, 158, 255, 0.22), transparent 60%),
    radial-gradient(1000px 600px at 80% 80%, rgba(103, 194, 58, 0.18), transparent 55%),
    linear-gradient(180deg, #0b1020, #070b14);
}

.login-card {
  width: 100%;
  max-width: 460px;
  border-radius: 16px;
  padding: 18px 18px 14px;
  background: rgba(255, 255, 255, 0.86);
  border: 1px solid rgba(0, 0, 0, 0.06);
  box-shadow: 0 16px 44px rgba(0, 0, 0, 0.08);
  backdrop-filter: blur(10px);
}

html.dark .login-card,
.login-page.dark .login-card {
  background: rgba(13, 18, 32, 0.85);
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 18px 52px rgba(0, 0, 0, 0.45);
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}

.logo {
  width: 42px;
  height: 42px;
  border-radius: 14px;
  background: linear-gradient(135deg, #409eff, #67c23a);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 900;
  color: #081020;
  letter-spacing: 0.2px;
}

.meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.title {
  font-size: 16px;
  font-weight: 900;
  letter-spacing: 0.2px;
  color: var(--el-text-color-primary);
}

.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.backend {
  padding: 12px;
  border-radius: 12px;
  border: 1px dashed rgba(0, 0, 0, 0.12);
  margin: 10px 0 14px;
}

html.dark .backend,
.login-page.dark .backend {
  border: 1px dashed rgba(255, 255, 255, 0.18);
}

.backend-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
}

.backend-title {
  font-size: 12px;
  font-weight: 800;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.backend-cur {
  font-size: 12px;
}

.badge {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 999px;
  border: 1px solid rgba(0, 0, 0, 0.12);
}

.badge.online {
  border-color: rgba(103, 194, 58, 0.45);
  color: #67c23a;
}

.badge.offline {
  border-color: rgba(245, 108, 108, 0.45);
  color: #f56c6c;
}

.badge.checking {
  border-color: rgba(144, 147, 153, 0.45);
  color: var(--el-text-color-secondary);
}

.backend-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
  margin-top: 4px;
}

.hint {
  font-size: 12px;
  margin-top: 8px;
}

.foot {
  margin-top: 12px;
  font-size: 12px;
  line-height: 1.45;
}

.muted {
  color: var(--el-text-color-secondary);
}
</style>
