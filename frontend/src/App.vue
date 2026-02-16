<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Menu as IconMenu, Moon, RefreshRight, Sunny } from '@element-plus/icons-vue'

import CreateTask from './components/CreateTask.vue'
import TaskList from './components/TaskList.vue'
import AccountManager from './components/AccountManager.vue'
import Dashboard from './views/Dashboard.vue'
import Settings from './views/Settings.vue'
import type { Task } from './api'

const taskListRef = ref<InstanceType<typeof TaskList> | null>(null)
const accountRef = ref<InstanceType<typeof AccountManager> | null>(null)
const dashboardRef = ref<InstanceType<typeof Dashboard> | null>(null)
const settingsRef = ref<InstanceType<typeof Settings> | null>(null)

const storageViewKey = 'tgvive_ui_active_view'
const storageCollapseKey = 'tgvive_ui_sidebar_collapse'
const storageThemeKey = 'tgvive_ui_theme'

type ActiveView = 'dashboard' | 'tasks' | 'accounts' | 'settings_proxy'

function getInitialView(): ActiveView {
  try {
    const saved = (localStorage.getItem(storageViewKey) || '').trim()
    if (saved === 'dashboard' || saved === 'tasks' || saved === 'accounts' || saved === 'settings_proxy') return saved
  } catch {
    // ignore
  }
  return 'dashboard'
}

const activeView = ref<ActiveView>(getInitialView())
const collapsed = ref(localStorage.getItem(storageCollapseKey) === '1')

watch(activeView, (v) => localStorage.setItem(storageViewKey, v))
watch(collapsed, (v) => localStorage.setItem(storageCollapseKey, v ? '1' : '0'))

type UITheme = 'light' | 'dark'

function getInitialTheme(): UITheme {
  try {
    const t = (localStorage.getItem(storageThemeKey) || '').trim()
    if (t === 'light' || t === 'dark') return t
  } catch {
    // ignore
  }
  try {
    if (window.matchMedia?.('(prefers-color-scheme: dark)')?.matches) return 'dark'
  } catch {
    // ignore
  }
  return 'light'
}

const theme = ref<UITheme>(getInitialTheme())

function applyTheme(v: UITheme) {
  try {
    const root = document.documentElement
    root.classList.toggle('dark', v === 'dark')
    root.style.backgroundColor = v === 'dark' ? '#1e1e1e' : '#f5f7fa'
  } catch {
    // ignore
  }
}

applyTheme(theme.value)
watch(theme, (v) => {
  try {
    localStorage.setItem(storageThemeKey, v)
  } catch {
    // ignore
  }
  applyTheme(v)
})

const pageTitle = computed(() => {
  switch (activeView.value) {
    case 'dashboard':
      return '系统仪表盘'
    case 'tasks':
      return '任务管理'
    case 'accounts':
      return '账号管理'
    case 'settings_proxy':
      return '网络代理'
  }
})

function onCreated(_: Task) {
  void taskListRef.value?.reloadTasks()
}

function toggleSidebar() {
  collapsed.value = !collapsed.value
}

async function refreshCurrent() {
  try {
    if (activeView.value === 'tasks') {
      await taskListRef.value?.reloadTasks()
    } else {
      if (activeView.value === 'accounts') await accountRef.value?.reloadAccounts()
      if (activeView.value === 'dashboard') dashboardRef.value?.refresh?.()
      if (activeView.value === 'settings_proxy') settingsRef.value?.reload?.()
    }
    ElMessage.success('已刷新')
  } catch {
    // ignore
  }
}

function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
}

function onMenuSelect(idx: string) {
  if (idx === 'dashboard' || idx === 'tasks' || idx === 'accounts' || idx === 'settings_proxy') activeView.value = idx
}
</script>

<template>
  <el-container class="layout">
    <el-aside :width="collapsed ? '64px' : '208px'" class="aside">
      <div class="aside-brand" :class="{ collapsed }">
        <div class="logo">TG</div>
        <div v-if="!collapsed" class="name">TGvive</div>
      </div>

      <div class="aside-menus">
        <el-menu
          class="menu"
          :default-active="activeView"
          :collapse="collapsed"
          background-color="#1e1e1e"
          text-color="#bfcbd9"
          active-text-color="#409eff"
          @select="onMenuSelect"
        >
          <el-menu-item index="dashboard">
            <el-icon><i class="ri-dashboard-3-line" /></el-icon>
            <span>仪表盘</span>
          </el-menu-item>
          <el-menu-item index="tasks">
            <el-icon><i class="ri-todo-line" /></el-icon>
            <span>任务管理</span>
          </el-menu-item>
          <el-menu-item index="accounts">
            <el-icon><i class="ri-user-3-line" /></el-icon>
            <span>账号管理</span>
          </el-menu-item>
        </el-menu>

        <el-menu
          class="menu menu-bottom"
          :default-active="activeView"
          :collapse="collapsed"
          background-color="#1e1e1e"
          text-color="#bfcbd9"
          active-text-color="#409eff"
          :default-openeds="activeView === 'settings_proxy' ? ['settings'] : []"
          @select="onMenuSelect"
        >
          <el-sub-menu index="settings">
            <template #title>
              <el-icon><i class="ri-settings-3-line" /></el-icon>
              <span>系统设置</span>
            </template>
            <el-menu-item index="settings_proxy">
              <el-icon><i class="ri-global-line" /></el-icon>
              <span>网络代理</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </div>
    </el-aside>

    <el-container>
      <el-header class="header">
        <div class="header-left">
          <el-button text class="icon-btn" @click="toggleSidebar">
            <el-icon><IconMenu /></el-icon>
          </el-button>
          <el-text class="title" tag="b">{{ pageTitle }}</el-text>
        </div>

        <div class="header-right">
          <CreateTask v-if="activeView === 'tasks'" @created="onCreated" />
          <el-button class="icon-btn" @click="toggleTheme">
            <el-icon>
              <Moon v-if="theme === 'light'" />
              <Sunny v-else />
            </el-icon>
            {{ theme === 'dark' ? '日间' : '夜间' }}
          </el-button>
          <el-button class="icon-btn" @click="refreshCurrent">
            <el-icon><RefreshRight /></el-icon>
            刷新
          </el-button>
        </div>
      </el-header>

      <el-main class="main">
        <div class="content" :class="{ wide: activeView === 'dashboard' }">
          <Dashboard v-if="activeView === 'dashboard'" ref="dashboardRef" />
          <TaskList v-if="activeView === 'tasks'" ref="taskListRef" :active="true" />
          <AccountManager v-if="activeView === 'accounts'" ref="accountRef" />
          <Settings v-if="activeView === 'settings_proxy'" ref="settingsRef" />
        </div>
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.layout {
  min-height: 100vh;
}

.aside {
  background: #1e1e1e;
  color: #bfcbd9;
  display: flex;
  flex-direction: column;
  border-right: 1px solid rgba(255, 255, 255, 0.06);
}

.aside-brand {
  height: 56px;
  padding: 0 14px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.aside-brand.collapsed {
  justify-content: center;
  padding: 0;
}

.logo {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: linear-gradient(135deg, #409eff, #67c23a);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  color: #0b1020;
}

.name {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.2px;
  color: #fff;
}

.menu {
  border-right: none;
}

.aside-menus {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.menu-bottom {
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.title {
  font-size: 15px;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.icon-btn :deep(.el-icon) {
  margin-right: 4px;
}

.main {
  padding: 16px;
  background: transparent;
}

.content {
  max-width: 1200px;
  margin: 0 auto;
}

.content.wide {
  max-width: 1440px;
}
</style>
