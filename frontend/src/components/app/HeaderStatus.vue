<script setup lang="ts">
import { computed } from 'vue'
import { ElTooltip } from 'element-plus'
import { Moon, Sunny } from '@element-plus/icons-vue'

import { useBackendPing } from '../../composables/useBackendPing'
import { useClock } from '../../composables/useClock'
import type { UITheme } from '../../composables/useTheme'

const props = defineProps<{
  theme: UITheme
}>()

const emit = defineEmits<{
  (e: 'toggleTheme'): void
}>()

const { timeText } = useClock(1000)
const { status, latencyMs, checkedAt } = useBackendPing({ intervalMs: 10_000, timeoutMs: 2000 })

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
      <el-tag size="small" :type="pingTagType">{{ pingText }}</el-tag>
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
</style>

