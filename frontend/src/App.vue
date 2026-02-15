<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Menu as IconMenu, Moon, RefreshRight, Sunny, Tickets, User } from '@element-plus/icons-vue'

import CreateTask from './components/CreateTask.vue'
import TaskList from './components/TaskList.vue'
import AccountManager from './components/AccountManager.vue'
import type { Task } from './api'

const taskListRef = ref<InstanceType<typeof TaskList> | null>(null)
const accountRef = ref<InstanceType<typeof AccountManager> | null>(null)

const storageViewKey = 'tgvive_ui_active_view'
const storageCollapseKey = 'tgvive_ui_sidebar_collapse'
const storageThemeKey = 'tgvive_ui_theme'

const activeView = ref<'tasks' | 'accounts'>((localStorage.getItem(storageViewKey) as any) || 'tasks')
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
    root.style.backgroundColor = v === 'dark' ? '#0b1020' : '#f5f7fa'
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

const pageTitle = computed(() => (activeView.value === 'tasks' ? '任务管理' : '账号管理'))

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
      await accountRef.value?.reloadAccounts()
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
  if (idx === 'tasks' || idx === 'accounts') {
    activeView.value = idx
  }
}
</script>

<template>
  <el-container class="layout">
    <el-aside :width="collapsed ? '64px' : '208px'" class="aside">
      <div class="aside-brand" :class="{ collapsed }">
        <div class="logo">TG</div>
        <div v-if="!collapsed" class="name">TGvive</div>
      </div>

      <el-menu
        class="menu"
        :default-active="activeView"
        :collapse="collapsed"
        background-color="#1f2d3d"
        text-color="#bfcbd9"
        active-text-color="#409eff"
        @select="onMenuSelect"
      >
        <el-menu-item index="tasks">
          <el-icon><Tickets /></el-icon>
          <span>任务管理</span>
        </el-menu-item>
        <el-menu-item index="accounts">
          <el-icon><User /></el-icon>
          <span>账号管理</span>
        </el-menu-item>
      </el-menu>
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
        <div class="content">
          <TaskList v-show="activeView === 'tasks'" ref="taskListRef" :active="activeView === 'tasks'" />
          <AccountManager v-show="activeView === 'accounts'" ref="accountRef" />
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
  background: #1f2d3d;
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
  background: var(--el-bg-color-page);
}

.content {
  max-width: 1200px;
  margin: 0 auto;
}
</style>
