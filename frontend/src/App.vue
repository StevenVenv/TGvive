<script setup lang="ts">
import { ref } from 'vue'

import CreateTask from './components/CreateTask.vue'
import TaskList from './components/TaskList.vue'
import AccountManager from './components/AccountManager.vue'
import type { Task } from './api'

const taskListRef = ref<InstanceType<typeof TaskList> | null>(null)
const activeTab = ref<'tasks' | 'accounts'>('tasks')

function onCreated(_: Task) {
  void taskListRef.value?.reloadTasks()
}
</script>

<template>
  <el-container class="app">
    <el-header class="header">
      <div class="brand">
        <el-text tag="b">TGvive Dashboard</el-text>
      </div>

      <div class="actions">
        <CreateTask v-if="activeTab === 'tasks'" @created="onCreated" />
      </div>
    </el-header>

    <el-main class="main">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="任务" name="tasks">
          <TaskList ref="taskListRef" />
        </el-tab-pane>
        <el-tab-pane label="账号" name="accounts">
          <AccountManager />
        </el-tab-pane>
      </el-tabs>
    </el-main>
  </el-container>
</template>

<style scoped>
.app {
  min-height: 100vh;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid #ebeef5;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.main {
  padding: 16px;
}
</style>
