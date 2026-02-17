<script setup lang="ts">
import { computed } from 'vue'
import { ElMessage } from 'element-plus'

type Row = {
  title: string
  expr: string
}

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
})

const tgRows: Row[] = [
  { title: 'TG 链接 (t.me)', expr: 'https?://t\\.me/\\S+' },
  { title: '所有链接', expr: 'https?://\\S+' },
  { title: '用户名/频道 (@xxx)', expr: '@\\w+' },
  { title: 'USDT-TRC20 地址', expr: 'T[A-Za-z1-9]{33}' },
]

const commonRows: Row[] = [
  { title: '中国手机号', expr: '1[3-9]\\d{9}' },
  { title: '邮箱', expr: '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}' },
  { title: '中文汉字', expr: '[\\u4e00-\\u9fa5]+' },
  { title: '纯数字 (6位验证码)', expr: '\\b\\d{6}\\b' },
]

const syntaxRows: Row[] = [
  { title: '行首 ^（例如以 abc 开头）', expr: '^abc' },
  { title: '行尾 $（例如以 abc 结尾）', expr: 'abc$' },
  { title: '*：重复 0 次或多次', expr: 'a*' },
  { title: '+：重复 1 次或多次', expr: 'a+' },
  { title: '?：出现 0 次或 1 次', expr: 'colou?r' },
]

async function copyToClipboard(text: string) {
  const value = String(text || '').trim()
  if (!value) return

  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      ElMessage.success('已复制到剪贴板')
      return
    }
  } catch {
    // fall through
  }

  try {
    const ta = document.createElement('textarea')
    ta.value = value
    ta.style.position = 'fixed'
    ta.style.top = '-9999px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (ok) ElMessage.success('已复制到剪贴板')
    else ElMessage.error('复制失败，请手动复制')
  } catch {
    ElMessage.error('复制失败，请手动复制')
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    title="正则表达式速查手册"
    width="700px"
    class="bt-dialog regex-cheat"
    destroy-on-close
    append-to-body
  >
    <el-alert
      type="warning"
      show-icon
      :closable="false"
      class="top-alert"
      title="本系统后端使用 Go 语言 (RE2 引擎)，不支持零宽断言 (Lookaround) 如 (?=...) 或 (?!...)。请仅使用标准正则。"
    />

    <el-tabs class="tabs">
      <el-tab-pane label="TG 专用">
        <el-table :data="tgRows" border stripe class="tbl" height="320">
          <el-table-column prop="title" label="说明" min-width="220" />
          <el-table-column label="表达式" min-width="320">
            <template #default="{ row }">
              <code class="expr">{{ row.expr }}</code>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="110" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="copyToClipboard(row.expr)">
                <i class="ri-file-copy-line" />
                复制
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="常用字符">
        <el-table :data="commonRows" border stripe class="tbl" height="320">
          <el-table-column prop="title" label="说明" min-width="220" />
          <el-table-column label="表达式" min-width="320">
            <template #default="{ row }">
              <code class="expr">{{ row.expr }}</code>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="110" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="copyToClipboard(row.expr)">
                <i class="ri-file-copy-line" />
                复制
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="基础语法">
        <el-table :data="syntaxRows" border stripe class="tbl" height="320">
          <el-table-column prop="title" label="说明" min-width="260" />
          <el-table-column label="示例" min-width="280">
            <template #default="{ row }">
              <code class="expr">{{ row.expr }}</code>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="110" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="copyToClipboard(row.expr)">
                <i class="ri-file-copy-line" />
                复制
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </el-dialog>
</template>

<style scoped lang="scss">
.regex-cheat {
  :deep(.el-dialog) {
    border: 1px solid #363637;
    background: #252525;
    border-radius: 4px;
  }

  :deep(.el-dialog__header) {
    border-bottom: 1px solid #363637;
    margin-right: 0;
  }

  :deep(.el-dialog__body) {
    padding-top: 12px;
  }

  :deep(.el-dialog__footer) {
    border-top: 1px solid #363637;
  }
}

.top-alert {
  margin-bottom: 12px;
}

.tabs :deep(.el-tabs__header) {
  margin: 0 0 10px;
}

.tbl :deep(.el-table__inner-wrapper) {
  border-radius: 4px;
}

.expr {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 6px 10px;
  border-radius: 4px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(0, 0, 0, 0.22);
  color: rgba(255, 255, 255, 0.92);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 12px;
}
</style>
