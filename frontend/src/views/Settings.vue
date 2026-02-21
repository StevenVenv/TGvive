<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getProxyConfig, testProxyConfig, updateProxyConfig } from '../api'

type ProxyType = 'http' | 'socks5'
type ProxyConfig = {
  enabled: boolean
  type: ProxyType
  host: string
  port: number
  username?: string
  password?: string
}

const form = reactive<ProxyConfig>({
  enabled: false,
  type: 'http',
  host: '127.0.0.1',
  port: 7890,
  username: '',
  password: '',
})

const testing = ref(false)
const saving = ref(false)
const lastPing = ref<number | null>(null)
const passwordSet = ref(false)
const passwordDirty = ref(false)
const suppressDirty = ref(false)

const endpoint = computed(() => {
  const host = (form.host || '').trim()
  const port = Number(form.port || 0)
  if (!host || !port) return '-'
  return `${host}:${port}`
})

const passwordPlaceholder = computed(() => (passwordSet.value ? '已设置（留空不修改）' : '可留空'))

function loadConfig() {
  return getProxyConfig()
    .then((cfg) => {
      form.enabled = !!cfg?.enabled
      if (cfg?.type === 'http' || cfg?.type === 'socks5') form.type = cfg.type
      if (typeof cfg?.host === 'string') form.host = cfg.host
      if (typeof cfg?.port === 'number' && Number.isFinite(cfg.port)) form.port = cfg.port
      if (typeof cfg?.username === 'string') form.username = cfg.username

      passwordSet.value = !!cfg?.password_set
      suppressDirty.value = true
      form.password = ''
      suppressDirty.value = false
      passwordDirty.value = false
    })
    .catch(() => {
      // ignore (e.g. not logged in yet)
    })
}

function reload() {
  loadConfig()
  lastPing.value = null
}

defineExpose({ reload })

function validate(): string | null {
  if (!form.enabled) return null
  const host = (form.host || '').trim()
  const port = Number(form.port || 0)
  if (!host) return '请填写服务器地址 (Host)'
  if (!port || port < 1 || port > 65535) return '请填写有效端口 (1-65535)'
  return null
}

async function testConnection() {
  const err = validate()
  if (err) {
    ElMessage.warning(err)
    return
  }
  if (!form.enabled) {
    lastPing.value = null
    ElMessage.info('当前为直连模式，无需测试代理')
    return
  }

  testing.value = true
  try {
    const res = await testProxyConfig({
      enabled: true,
      type: form.type,
      host: (form.host || '').trim(),
      port: Number(form.port || 0),
      username: (form.username || '').trim(),
      password: String(form.password || ''),
      timeout_ms: 8000,
    })
    lastPing.value = Math.max(0, Number(res?.ping_ms || 0) || 0)
    ElMessage.success(`连接成功：${endpoint.value} (${lastPing.value}ms)`)
  } catch {
    ElMessage.error('测试失败（请确认代理可用/鉴权信息正确）')
  } finally {
    testing.value = false
  }
}

async function saveConfig() {
  const err = validate()
  if (err) {
    ElMessage.warning(err)
    return
  }

  saving.value = true
  try {
    const payload: any = {
      enabled: form.enabled,
      type: form.type,
      host: (form.host || '').trim(),
      port: Number(form.port || 0),
      username: (form.username || '').trim(),
    }
    if (passwordDirty.value) payload.password = String(form.password || '')

    const saved = await updateProxyConfig(payload)
    passwordSet.value = !!saved?.password_set
    suppressDirty.value = true
    form.password = ''
    suppressDirty.value = false
    passwordDirty.value = false
    ElMessage.success('已保存并热生效')
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void loadConfig()
})

watch(
  () => form.password,
  () => {
    if (suppressDirty.value) return
    passwordDirty.value = true
  },
)
</script>

<template>
  <div class="settings">
    <el-row :gutter="12">
      <el-col :xs="24" :lg="14">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-settings-3-line" />
                <span>全局设置</span>
              </div>
              <div class="card-sub">Network Proxy</div>
            </div>
          </template>

          <div class="status">
            <div class="status-left">
              <div class="status-title">程序网络代理</div>
              <div class="status-sub muted">
                当前：<b>{{ form.enabled ? form.type.toUpperCase() : 'DIRECT' }}</b>
                <span class="dot">·</span>
                <span>{{ form.enabled ? endpoint : '直连模式' }}</span>
                <span v-if="lastPing !== null" class="dot">·</span>
                <span v-if="lastPing !== null">{{ lastPing }}ms</span>
              </div>
            </div>
            <div class="status-right">
              <el-switch v-model="form.enabled" active-text="代理开启" inactive-text="直连" />
            </div>
          </div>

          <div class="split-line" />

          <el-form label-position="top" class="form">
            <el-row :gutter="12">
              <el-col :xs="24" :sm="12">
                <el-form-item label="代理类型">
                  <el-select v-model="form.type" placeholder="选择代理类型" style="width: 100%">
                    <el-option label="HTTP" value="http" />
                    <el-option label="SOCKS5" value="socks5" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="服务器地址 (Host)">
                  <el-input v-model="form.host" placeholder="例如：127.0.0.1" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="端口 (Port)">
                  <el-input v-model.number="form.port" placeholder="例如：7890" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="鉴权（可选）用户名">
                  <el-input v-model="form.username" placeholder="可留空" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item label="鉴权（可选）密码">
                  <el-input v-model="form.password" type="password" show-password :placeholder="passwordPlaceholder" />
                </el-form-item>
              </el-col>
            </el-row>

            <div class="actions">
              <el-button class="btn" @click="testConnection" :loading="testing">
                <i class="ri-pulse-line" />
                <span>测试连接</span>
              </el-button>
              <el-button type="primary" class="btn primary" @click="saveConfig" :loading="saving">
                <i class="ri-save-3-line" />
                <span>保存配置</span>
              </el-button>
            </div>
          </el-form>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="10">
        <el-card class="bt-card" shadow="never">
          <template #header>
            <div class="card-header">
              <div class="card-title">
                <i class="ri-information-line" />
                <span>说明</span>
              </div>
              <div class="card-sub">Notes</div>
            </div>
          </template>

          <div class="note">
            <div class="note-item">
              <div class="k muted">生效范围</div>
              <div class="v">保存到后端并立即热生效（不会修改 config.yaml）。</div>
            </div>
            <div class="note-item">
              <div class="k muted">仪表盘展示</div>
              <div class="v">返回“仪表盘”后会读取并展示当前代理地址与延时。</div>
            </div>
            <div class="note-item">
              <div class="k muted">安全提示</div>
              <div class="v">如需持久化到后端，请在 API 层对密码进行加密/脱敏。</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped lang="scss">
.settings {
  width: 100%;
}

.status {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.status-title {
  font-weight: 800;
}

.status-sub {
  margin-top: 4px;
  font-size: 12px;
}

.dot {
  margin: 0 6px;
  opacity: 0.7;
}

.actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.note {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.note-item {
  padding: 12px 12px;
  border-radius: 12px;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-light);
}

.note-item .k {
  font-size: 12px;
}

.note-item .v {
  margin-top: 6px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
}
</style>
