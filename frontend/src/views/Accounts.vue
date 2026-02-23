<script setup lang="ts">
import { ref, watch } from 'vue'

import BotManager from '../components/BotManager.vue'
import TGAccountManager from '../components/AccountManager.vue'
import DialogIDFinder from '../components/DialogIDFinder.vue'

type AccountsTab = 'tg' | 'bots' | 'dialogs'

const storageKey = 'tgvive_ui_accounts_tab'

function getInitialTab(): AccountsTab {
  try {
    const saved = (localStorage.getItem(storageKey) || '').trim()
    if (saved === 'tg' || saved === 'bots' || saved === 'dialogs') return saved as AccountsTab
  } catch {
    // ignore
  }
  return 'tg'
}

const activeTab = ref<AccountsTab>(getInitialTab())

const tgRef = ref<InstanceType<typeof TGAccountManager> | null>(null)
const botRef = ref<InstanceType<typeof BotManager> | null>(null)
const dlgRef = ref<InstanceType<typeof DialogIDFinder> | null>(null)

async function reloadAccounts() {
  await Promise.all([tgRef.value?.reloadAccounts?.(), botRef.value?.reloadBots?.(), dlgRef.value?.reload?.()])
}

defineExpose({ reloadAccounts })

watch(activeTab, (v) => {
  try {
    localStorage.setItem(storageKey, v)
  } catch {
    // ignore
  }
})
</script>

<template>
  <div class="accounts-view">
    <el-tabs v-model="activeTab" type="card" class="accounts-tabs">
      <el-tab-pane label="TG账号" name="tg">
        <TGAccountManager ref="tgRef" />
      </el-tab-pane>
      <el-tab-pane label="群组ID" name="dialogs">
        <DialogIDFinder ref="dlgRef" />
      </el-tab-pane>
      <el-tab-pane label="Bot管理" name="bots">
        <BotManager ref="botRef" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped>
.accounts-view {
  width: 100%;
}

.accounts-tabs :deep(.el-tabs__header) {
  margin-bottom: 12px;
}
</style>
