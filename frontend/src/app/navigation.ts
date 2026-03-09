import type { Component } from 'vue'
import { Connection, List, Odometer, Operation, Setting, User } from '@element-plus/icons-vue'

export type ActiveView = 'dashboard' | 'tasks' | 'strategies' | 'accounts' | 'settings_proxy'

export type NavItem = {
  key: ActiveView
  label: string
  icon: Component
}

export const mainNav: NavItem[] = [
  { key: 'dashboard', label: '仪表盘', icon: Odometer },
  { key: 'tasks', label: '任务列表', icon: List },
  { key: 'strategies', label: '策略配置', icon: Operation },
  { key: 'accounts', label: '账号管理', icon: User },
]

export const settingsNav: NavItem[] = [{ key: 'settings_proxy', label: '账号与代理', icon: Connection }]

export const settingsGroup = { key: 'settings', label: '系统设置', icon: Setting } as const

export function titleForView(v: ActiveView): string {
  const all = [...mainNav, ...settingsNav]
  const hit = all.find((x) => x.key === v)
  return hit?.label || 'TGvive'
}
