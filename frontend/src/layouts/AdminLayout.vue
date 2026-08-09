<script setup lang="ts">
import { computed } from 'vue'
import { Expand, Fold } from '@element-plus/icons-vue'

import HeaderStatus from '../components/app/HeaderStatus.vue'
import { mainNav, settingsGroup, settingsNav, titleForView, type ActiveView } from '../app/navigation'
import type { UITheme } from '../composables/useTheme'

const props = defineProps<{
  activeView: ActiveView
  collapsed: boolean
  theme: UITheme
}>()

const emit = defineEmits<{
  (e: 'update:activeView', v: ActiveView): void
  (e: 'update:collapsed', v: boolean): void
  (e: 'update:theme', v: UITheme): void
}>()

const pageTitle = computed(() => titleForView(props.activeView))

function toggleSidebar() {
  emit('update:collapsed', !props.collapsed)
}

function onToggleTheme() {
  emit('update:theme', props.theme === 'dark' ? 'light' : 'dark')
}

function onMenuSelect(idx: string) {
  if (idx === 'dashboard' || idx === 'tasks' || idx === 'strategies' || idx === 'accounts' || idx === 'settings_proxy') {
    emit('update:activeView', idx)
  }
}
</script>

<template>
  <el-container class="tgv-layout">
    <el-aside :width="collapsed ? '64px' : '208px'" class="tgv-aside">
      <div class="tgv-brand" :class="{ collapsed }">
        <div class="tgv-logo">TG</div>
        <div v-if="!collapsed" class="tgv-name">TGvive</div>
      </div>

      <div class="tgv-menus">
        <el-menu
          class="tgv-menu"
          :default-active="activeView"
          :collapse="collapsed"
          :collapse-transition="false"
          background-color="transparent"
          text-color="#c0c8d2"
          active-text-color="#409eff"
          @select="onMenuSelect"
        >
          <el-menu-item v-for="item in mainNav" :key="item.key" :index="item.key">
            <el-icon><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
          </el-menu-item>
        </el-menu>

        <el-menu
          class="tgv-menu tgv-menu-bottom"
          :default-active="activeView"
          :collapse="collapsed"
          :collapse-transition="false"
          background-color="transparent"
          text-color="#c0c8d2"
          active-text-color="#409eff"
          :default-openeds="activeView === 'settings_proxy' ? [settingsGroup.key] : []"
          @select="onMenuSelect"
        >
          <el-sub-menu :index="settingsGroup.key">
            <template #title>
              <el-icon><component :is="settingsGroup.icon" /></el-icon>
              <span>{{ settingsGroup.label }}</span>
            </template>
            <el-menu-item v-for="item in settingsNav" :key="item.key" :index="item.key">
              <el-icon><component :is="item.icon" /></el-icon>
              <span>{{ item.label }}</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </div>
    </el-aside>

    <el-container class="tgv-body">
      <el-header class="tgv-header">
        <div class="tgv-header-inner">
          <div class="tgv-header-left">
            <el-button class="tgv-icon-btn" text @click="toggleSidebar">
              <el-icon>
                <Fold v-if="!collapsed" />
                <Expand v-else />
              </el-icon>
            </el-button>
            <div class="tgv-title">
              <div class="tgv-title-main">TG 引擎中枢</div>
              <div class="tgv-title-sub">{{ pageTitle }}</div>
            </div>
          </div>

          <div class="tgv-header-right">
            <slot name="header-actions" />
            <slot name="header-status">
              <HeaderStatus :theme="theme" @toggle-theme="onToggleTheme" />
            </slot>
          </div>
        </div>
      </el-header>

      <el-main class="tgv-main">
        <div class="tgv-main-inner">
          <slot />
        </div>
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.tgv-layout {
  height: 100vh;
  min-width: 0;
  background: var(--tgv-main-bg);
}

.tgv-aside {
  background: var(--tgv-aside-bg);
  color: var(--tgv-aside-text);
  display: flex;
  flex-direction: column;
  border-right: 1px solid rgba(0, 0, 0, 0.12);
  overflow: hidden;
  transition: width 0.18s ease;
}

html.dark .tgv-aside {
  border-right: 1px solid rgba(255, 255, 255, 0.06);
}

.tgv-brand {
  height: 58px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  flex: none;
}

.tgv-brand.collapsed {
  justify-content: center;
  padding: 0;
}

.tgv-logo {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: linear-gradient(135deg, var(--el-color-primary), var(--el-color-success));
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  color: #0b1020;
  letter-spacing: 0.2px;
}

.tgv-name {
  font-size: 14px;
  font-weight: 800;
  letter-spacing: 0.2px;
  color: #ffffff;
}

.tgv-menus {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  min-height: 0;
}

.tgv-menu {
  border-right: none;
  --el-menu-item-height: 52px;
  --el-menu-sub-item-height: 46px;
  --el-menu-hover-bg-color: rgba(255, 255, 255, 0.06);
  --el-menu-active-color: var(--el-color-primary);
  --el-menu-text-color: var(--tgv-aside-text);
  --el-menu-bg-color: transparent;
}

.tgv-menu :deep(.el-menu-item),
.tgv-menu :deep(.el-sub-menu__title) {
  margin: 2px 8px;
  border-radius: 8px;
}

.tgv-menu :deep(.el-menu-item.is-active) {
  background: rgba(64, 158, 255, 0.12);
  font-weight: 700;
}

.tgv-menu-bottom {
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.tgv-body {
  min-width: 0;
  height: 100vh;
  background: var(--tgv-main-bg);
  overflow: auto;
}

.tgv-header {
  padding: 0;
  background: var(--tgv-header-bg);
  box-shadow: var(--tgv-header-shadow);
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.tgv-header-inner {
  height: 100%;
  width: 100%;
  max-width: var(--tgv-content-max-width);
  margin: 0 auto;
  padding: 0 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.tgv-header-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.tgv-icon-btn :deep(.el-icon) {
  font-size: 18px;
}

.tgv-icon-btn {
  width: 32px;
  height: 32px;
  padding: 0;
}

.tgv-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.tgv-title-main {
  font-size: 14px;
  font-weight: 800;
  line-height: 1.1;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tgv-title-sub {
  font-size: 12px;
  line-height: 1.1;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tgv-header-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
  min-width: 0;
}

.tgv-main {
  flex: 0 0 auto;
  width: 100%;
  max-width: var(--tgv-content-max-width);
  margin: 0 auto;
  background: transparent;
  padding: 8px;
  overflow: visible;
}

.tgv-main-inner {
  width: 100%;
  margin: 0;
  min-height: 0;
}

@media (max-width: 768px) {
  .tgv-aside {
    width: 64px !important;
    flex: 0 0 64px !important;
  }

  .tgv-brand {
    justify-content: center;
    padding: 0;
  }

  .tgv-name,
  .tgv-menu :deep(.el-menu-item span),
  .tgv-menu :deep(.el-sub-menu__title span),
  .tgv-menu :deep(.el-sub-menu__icon-arrow) {
    display: none;
  }

  .tgv-menu :deep(.el-menu-item),
  .tgv-menu :deep(.el-sub-menu__title) {
    justify-content: center;
    padding: 0 !important;
  }

  .tgv-header {
    height: auto !important;
    min-height: 60px;
  }

  .tgv-header-inner {
    min-height: 60px;
    align-items: flex-start;
    gap: 8px;
    padding: 8px;
    flex-wrap: wrap;
  }

  .tgv-header-right {
    width: 100%;
    justify-content: flex-start;
  }

  .tgv-main {
    padding: 6px;
  }
}
</style>
