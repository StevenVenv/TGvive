<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { RefreshRight } from '@element-plus/icons-vue'

import { authMe, logout, type AuthUser } from './api'
import AdminLayout from './layouts/AdminLayout.vue'
import { useTheme } from './composables/useTheme'
import type { ActiveView } from './app/navigation'

import CreateTask from './components/CreateTask.vue'
import TaskList from './components/TaskList.vue'
import Dashboard from './views/Dashboard.vue'
import Accounts from './views/Accounts.vue'
import StrategyManager from './views/StrategyManager.vue'
import Settings from './views/Settings.vue'
import Login from './views/Login.vue'

const taskListRef = ref<InstanceType<typeof TaskList> | null>(null)
const accountRef = ref<InstanceType<typeof Accounts> | null>(null)
const dashboardRef = ref<InstanceType<typeof Dashboard> | null>(null)
const strategyRef = ref<InstanceType<typeof StrategyManager> | null>(null)
const settingsRef = ref<InstanceType<typeof Settings> | null>(null)

const storageViewKey = 'tgvive_ui_active_view'
const storageCollapseKey = 'tgvive_ui_sidebar_collapse'

const authChecking = ref(true)
const loggedIn = ref(false)
const me = ref<AuthUser | null>(null)
const loggingOut = ref(false)

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

async function loadMe() {
  authChecking.value = true
  try {
    const res = await authMe()
    if (res?.logged_in && res.user?.id) {
      loggedIn.value = true
      me.value = res.user
    } else {
      loggedIn.value = false
      me.value = null
    }
  } catch {
    loggedIn.value = false
    me.value = null
  } finally {
    authChecking.value = false
  }
}

function onLoggedIn(user: AuthUser) {
  loggedIn.value = true
  me.value = user
}

function onAccountUpdated(user: AuthUser) {
  me.value = user
}

async function doLogout() {
  if (loggingOut.value) return
  loggingOut.value = true
  try {
    await logout()
  } catch {
    // ignore
  } finally {
    loggingOut.value = false
  }
  loggedIn.value = false
  me.value = null
}

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

onMounted(() => {
  void loadMe()
})
</script>

<template>
  <div v-if="authChecking" class="boot">
    <div class="boot-card">
      <div class="boot-logo">TG</div>
      <div class="boot-title">TGvive</div>
      <div class="boot-sub">正在检查登录状态…</div>
    </div>
  </div>

  <Login v-else-if="!loggedIn" @logged-in="onLoggedIn" />

  <AdminLayout v-else v-model:activeView="activeView" v-model:collapsed="collapsed" v-model:theme="theme">
    <template #header-actions>
      <div class="app-header-actions">
        <CreateTask v-if="activeView === 'tasks'" @refresh="onRefreshTasks" />
        <el-button class="refresh-btn" @click="refreshCurrent">
          <el-icon><RefreshRight /></el-icon>
          <span>刷新</span>
        </el-button>
        <div class="userbar">
          <span class="user">{{ me?.username || '' }}</span>
          <el-button size="small" :loading="loggingOut" @click="doLogout">
            <i class="ri-logout-box-r-line" />
            <span>退出</span>
          </el-button>
        </div>
      </div>
    </template>

    <Dashboard v-if="activeView === 'dashboard'" ref="dashboardRef" />
    <TaskList v-if="activeView === 'tasks'" ref="taskListRef" :active="true" />
    <StrategyManager v-if="activeView === 'strategies'" ref="strategyRef" />
    <Accounts v-if="activeView === 'accounts'" ref="accountRef" />
    <Settings v-if="activeView === 'settings_proxy'" ref="settingsRef" :user="me" @account-updated="onAccountUpdated" />
  </AdminLayout>
</template>

<style scoped>
.app-header-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.refresh-btn :deep(.el-icon) {
  margin-right: 6px;
}

.userbar {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 32px;
  padding-left: 4px;
}

.user {
  font-size: 12px;
  font-weight: 700;
  color: var(--el-text-color-regular);
  max-width: 112px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.boot {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.boot-card {
  width: 100%;
  max-width: 420px;
  border-radius: var(--tgv-card-radius, 8px);
  padding: 18px;
  background: rgba(255, 255, 255, 0.86);
  border: 1px solid rgba(0, 0, 0, 0.06);
  box-shadow: 0 16px 44px rgba(0, 0, 0, 0.08);
  backdrop-filter: blur(10px);
  text-align: center;
}

html.dark .boot-card {
  background: rgba(13, 18, 32, 0.85);
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 18px 52px rgba(0, 0, 0, 0.45);
}

.boot-logo {
  width: 42px;
  height: 42px;
  border-radius: 10px;
  margin: 0 auto 10px;
  background: linear-gradient(135deg, var(--el-color-primary), var(--el-color-success));
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 900;
  color: #081020;
  letter-spacing: 0.2px;
}

.boot-title {
  font-size: 16px;
  font-weight: 900;
  letter-spacing: 0.2px;
  color: var(--el-text-color-primary);
}

.boot-sub {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
