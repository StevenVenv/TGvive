<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElTooltip } from 'element-plus'
import { Moon, Sunny } from '@element-plus/icons-vue'

import { useBackendPing } from '../../composables/useBackendPing'
import { useClock } from '../../composables/useClock'
import type { UITheme } from '../../composables/useTheme'
import {
  ensureBackendOrigin,
  getBackendOrigin,
  normalizeBackendOrigin,
  probeBackendOrigin,
  setBackendOrigin,
  suggestLocalBackendOrigin,
} from '../../runtime/backend'

const props = defineProps<{
  theme: UITheme
}>()

const emit = defineEmits<{
  (e: 'toggleTheme'): void
}>()

const { timeText } = useClock(1000)
const { status, latencyMs, checkedAt } = useBackendPing({ intervalMs: 10_000, timeoutMs: 2000 })

const backendDialogOpen = ref(false)
const backendInput = ref('')
const backendTesting = ref(false)

function backendLabel(origin: string): string {
  const s = String(origin || '').trim()
  return s ? s : '(同源 / 不覆盖)'
}

function openBackendDialog() {
  backendInput.value = getBackendOrigin() || suggestLocalBackendOrigin(8081) || ''
  backendDialogOpen.value = true
}

async function autoDetectBackend() {
  backendTesting.value = true
  try {
    await ensureBackendOrigin()
    window.location.reload()
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
    window.location.reload()
  } catch {
    ElMessage.error('连接失败')
  } finally {
    backendTesting.value = false
  }
}

function clearBackendOverride() {
  setBackendOrigin('')
  window.location.reload()
}

const pingTagType = computed<'success' | 'danger' | 'info'>(() => {
  if (status.value === 'online') return 'success'
  if (status.value === 'offline') return 'danger'
  return 'info'
})

const pingText = computed(() => {
  if (status.value === 'checking') return 'PING'
  if (status.value === 'offline') return 'OFFLINE'
  const ms = latencyMs.value
  if (!ms && ms !== 0) return 'ONLINE'
  return `ONLINE ${ms}ms`
})

const pingTip = computed(() => {
  if (!checkedAt.value) return ''
  try {
    return `Last check: ${new Date(checkedAt.value).toLocaleString()}`
  } catch {
    return ''
  }
})
</script>

<template>
  <div class="header-status">
    <el-tooltip :disabled="!pingTip" :content="pingTip" placement="bottom" :show-after="300">
      <el-tag size="small" :type="pingTagType" class="ping-tag" @click="openBackendDialog">{{ pingText }}</el-tag>
    </el-tooltip>

    <span class="clock">{{ timeText }}</span>

    <el-tooltip :content="props.theme === 'dark' ? '切换到浅色' : '切换到暗色'" placement="bottom" :show-after="200">
      <el-button class="theme-btn" text @click="emit('toggleTheme')">
        <el-icon>
          <Moon v-if="props.theme === 'light'" />
          <Sunny v-else />
        </el-icon>
      </el-button>
    </el-tooltip>

    <el-dialog v-model="backendDialogOpen" title="连接后端" width="460px" class="backend-dialog">
      <div class="backend-body">
        <div class="backend-row">
          <span class="muted">当前：</span>
          <span class="mono">{{ backendLabel(getBackendOrigin()) }}</span>
        </div>
        <el-input v-model="backendInput" placeholder="http://localhost:8081" clearable />
        <div class="backend-hint muted">
          用于“前端与后端不同端口”场景（例如：后端 `:8081`，前端开发服务在 `:5172`）。
        </div>
      </div>
      <template #footer>
        <el-button @click="clearBackendOverride">清除</el-button>
        <el-button :loading="backendTesting" @click="autoDetectBackend">自动检测</el-button>
        <el-button type="primary" :loading="backendTesting" @click="testAndSaveBackend">测试并保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.header-status {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}

.clock {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  letter-spacing: 0.2px;
}

.theme-btn {
  padding: 6px 8px;
}

.ping-tag {
  cursor: pointer;
  user-select: none;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 12px;
}

.backend-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.backend-row {
  font-size: 12px;
}

.backend-hint {
  font-size: 12px;
  line-height: 1.45;
}
</style>
