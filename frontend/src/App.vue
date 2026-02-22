<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { RefreshRight } from '@element-plus/icons-vue'

import AdminLayout from './layouts/AdminLayout.vue'
import { useTheme } from './composables/useTheme'
import type { ActiveView } from './app/navigation'

import CreateTask from './components/CreateTask.vue'
import TaskList from './components/TaskList.vue'
import Dashboard from './views/Dashboard.vue'
import Accounts from './views/Accounts.vue'
import StrategyManager from './views/StrategyManager.vue'
import Settings from './views/Settings.vue'

const taskListRef = ref<InstanceType<typeof TaskList> | null>(null)
const accountRef = ref<InstanceType<typeof Accounts> | null>(null)
const dashboardRef = ref<InstanceType<typeof Dashboard> | null>(null)
const strategyRef = ref<InstanceType<typeof StrategyManager> | null>(null)
const settingsRef = ref<InstanceType<typeof Settings> | null>(null)

const storageViewKey = 'tgvive_ui_active_view'
const storageCollapseKey = 'tgvive_ui_sidebar_collapse'

function getInitialView(): ActiveView {
  try {
    const saved = (localStorage.getItem(storageViewKey) || '').trim()
    if (saved === 'dashboard' || saved === 'tasks' || saved === 'strategies' || saved === 'accounts' || saved === 'settings_proxy')
      return saved as ActiveView
  } catch {
    // ignore
  }
  return 'dashboard'
}

function getInitialCollapsed(): boolean {
  try {
    return localStorage.getItem(storageCollapseKey) === '1'
  } catch {
    return false
  }
}

const activeView = ref<ActiveView>(getInitialView())
const collapsed = ref(getInitialCollapsed())

const { theme } = useTheme()

watch(activeView, (v) => {
  try {
    localStorage.setItem(storageViewKey, v)
  } catch {
    // ignore
  }
})

watch(collapsed, (v) => {
  try {
    localStorage.setItem(storageCollapseKey, v ? '1' : '0')
  } catch {
    // ignore
  }
})

function onRefreshTasks() {
  void taskListRef.value?.reloadTasks()
}

async function refreshCurrent() {
  try {
    if (activeView.value === 'tasks') {
      await taskListRef.value?.reloadTasks()
    } else {
      if (activeView.value === 'accounts') await accountRef.value?.reloadAccounts()
      if (activeView.value === 'dashboard') dashboardRef.value?.refresh?.()
      if (activeView.value === 'strategies') await strategyRef.value?.reload?.()
      if (activeView.value === 'settings_proxy') settingsRef.value?.reload?.()
    }
    ElMessage.success('已刷新')
  } catch {
    // ignore
  }
}
</script>

<template>
  <AdminLayout v-model:activeView="activeView" v-model:collapsed="collapsed" v-model:theme="theme">
    <template #header-actions>
      <CreateTask v-if="activeView === 'tasks'" @refresh="onRefreshTasks" />
      <el-button class="refresh-btn" @click="refreshCurrent">
        <el-icon><RefreshRight /></el-icon>
        刷新
      </el-button>
    </template>

    <Dashboard v-if="activeView === 'dashboard'" ref="dashboardRef" />
    <TaskList v-if="activeView === 'tasks'" ref="taskListRef" :active="true" />
    <StrategyManager v-if="activeView === 'strategies'" ref="strategyRef" />
    <Accounts v-if="activeView === 'accounts'" ref="accountRef" />
    <Settings v-if="activeView === 'settings_proxy'" ref="settingsRef" />
  </AdminLayout>
</template>

<style scoped>
.refresh-btn :deep(.el-icon) {
  margin-right: 6px;
}
</style>
