<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'

import { createTask, type Task } from '../api'

const props = defineProps<{
  token: string
}>()

const emit = defineEmits<{
  (e: 'created', task: Task): void
}>()

const open = ref(false)
const submitting = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  source_url: '',
  target_url: '',

  session_key: '',

  clone_mode: 3,
  content_types: ['text', 'image', 'video', 'audio', 'file'],

  scope_type: 1,
  scope_value: '',
  history_order: 1,

  keep_reply: false,
  realtime: false,
  clone_comment: false,
  gpu_accel: false,
  change_md5: false,

  delay_min_ms: 1000,
  delay_max_ms: 3000,

  daily_limit: 0,
  run_window: '',
})

const rules: FormRules = {
  source_url: [{ required: true, message: '请输入源频道/群组', trigger: 'blur' }],
  target_url: [{ required: true, message: '请输入目标频道/群组', trigger: 'blur' }],
  clone_mode: [{ required: true, message: '请选择克隆模式', trigger: 'change' }],
  scope_type: [{ required: true, message: '请选择消息范围', trigger: 'change' }],
  delay_min_ms: [{ type: 'number', required: true, message: '请输入最小延时', trigger: 'change' }],
  delay_max_ms: [{ type: 'number', required: true, message: '请输入最大延时', trigger: 'change' }],
}

function resetForm() {
  form.source_url = ''
  form.target_url = ''
  form.session_key = ''
  form.clone_mode = 3
  form.content_types = ['text', 'image', 'video', 'audio', 'file']
  form.scope_type = 1
  form.scope_value = ''
  form.history_order = 1

  form.keep_reply = false
  form.realtime = false
  form.clone_comment = false
  form.gpu_accel = false
  form.change_md5 = false

  form.delay_min_ms = 1000
  form.delay_max_ms = 3000

  form.daily_limit = 0
  form.run_window = ''
}

async function submit() {
  if (!props.token.trim()) {
    ElMessage.warning('请先填写 Token')
    return
  }

  const inst = formRef.value
  if (!inst) return

  try {
    const ok = await inst.validate()
    if (!ok) return
  } catch {
    return
  }

  if (form.delay_min_ms < 0 || form.delay_max_ms < 0) {
    ElMessage.error('延时不能为负数')
    return
  }
  if (form.delay_max_ms < form.delay_min_ms) {
    ElMessage.error('最大延时必须大于等于最小延时')
    return
  }

  submitting.value = true
  try {
    const payload: Partial<Task> = {
      source_url: form.source_url.trim(),
      target_url: form.target_url.trim(),
      session_key: form.session_key.trim(),

      clone_mode: form.clone_mode,
      content_types: form.content_types,

      scope_type: form.scope_type,
      scope_value: form.scope_value.trim(),
      history_order: form.history_order,

      keep_reply: form.keep_reply,
      realtime: form.realtime,
      clone_comment: form.clone_comment,
      gpu_accel: form.gpu_accel,
      change_md5: form.change_md5,

      delay_min_ms: form.delay_min_ms,
      delay_max_ms: form.delay_max_ms,

      daily_limit: form.daily_limit,
      run_window: form.run_window.trim(),
    }

    const created = await createTask(props.token, payload)
    ElMessage.success(`任务已创建 #${created.ID}`)
    emit('created', created)
    open.value = false
  } catch (err: any) {
    ElMessage.error(err?.message || '创建失败')
  } finally {
    submitting.value = false
  }
}

watch(open, (v) => {
  if (!v) {
    formRef.value?.clearValidate()
    resetForm()
  }
})
</script>

<template>
  <el-button type="primary" @click="open = true">新建任务</el-button>

  <el-dialog v-model="open" title="新建搬运任务" width="720px">
    <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
      <el-form-item label="源频道/群组" prop="source_url">
        <el-input v-model="form.source_url" placeholder="例如：https://t.me/source 或 @source" />
      </el-form-item>

      <el-form-item label="目标频道/群组" prop="target_url">
        <el-input v-model="form.target_url" placeholder="例如：https://t.me/target 或 @target" />
      </el-form-item>

      <el-form-item label="绑定账号" prop="session_key">
        <el-input v-model="form.session_key" placeholder="可选：sessions/session_<key>.json 的 key" />
      </el-form-item>

      <el-divider />

      <el-form-item label="克隆模式" prop="clone_mode">
        <el-radio-group v-model="form.clone_mode">
          <el-radio :label="1">转发</el-radio>
          <el-radio :label="2">发送</el-radio>
          <el-radio :label="3">下载上传</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item label="内容类型">
        <el-checkbox-group v-model="form.content_types">
          <el-checkbox label="text">文本</el-checkbox>
          <el-checkbox label="image">图片</el-checkbox>
          <el-checkbox label="video">视频</el-checkbox>
          <el-checkbox label="audio">语音/音频</el-checkbox>
          <el-checkbox label="file">文件/贴纸</el-checkbox>
        </el-checkbox-group>
        <div class="hint">不选=全类型</div>
      </el-form-item>

      <el-form-item label="消息范围" prop="scope_type">
        <el-select v-model="form.scope_type" style="width: 260px">
          <el-option :value="1" label="全部" />
          <el-option :value="2" label="最近 N 条" />
          <el-option :value="3" label="时间范围" />
          <el-option :value="4" label="ID 范围" />
        </el-select>
      </el-form-item>

      <el-form-item label="范围参数" prop="scope_value">
        <el-input
          v-model="form.scope_value"
          :placeholder="
            form.scope_type === 2
              ? '例如：100'
              : form.scope_type === 3
                ? '例如：2025-01-01~2025-01-31'
                : form.scope_type === 4
                  ? '例如：1000-2000'
                  : '可留空'
          "
        />
      </el-form-item>

      <el-form-item label="历史方向">
        <el-radio-group v-model="form.history_order">
          <el-radio :label="1">从旧到新</el-radio>
          <el-radio :label="2">从新到旧</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-divider />

      <el-form-item label="随机延时 (ms)">
        <div class="inline">
          <el-input-number v-model="form.delay_min_ms" :min="0" :step="100" controls-position="right" />
          <span class="sep">~</span>
          <el-input-number v-model="form.delay_max_ms" :min="0" :step="100" controls-position="right" />
        </div>
        <div class="hint">每处理完一条消息后随机 sleep</div>
      </el-form-item>

      <el-form-item label="每日配额">
        <el-input-number v-model="form.daily_limit" :min="0" :step="10" controls-position="right" />
        <div class="hint">0 = 不限制</div>
      </el-form-item>

      <el-form-item label="运行窗口">
        <el-input v-model="form.run_window" placeholder='例如：09:00-18:00（留空=全天）' style="max-width: 320px" />
      </el-form-item>

      <el-divider />

      <el-form-item label="处理开关">
        <el-space wrap>
          <el-switch v-model="form.keep_reply" active-text="保留回复" />
          <el-switch v-model="form.realtime" active-text="实时监控" />
          <el-switch v-model="form.clone_comment" active-text="克隆评论" />
          <el-switch v-model="form.gpu_accel" active-text="GPU 加速" />
          <el-switch v-model="form.change_md5" active-text="修改 MD5" />
        </el-space>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-space>
        <el-button @click="open = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">创建</el-button>
      </el-space>
    </template>
  </el-dialog>
</template>

<style scoped>
.inline {
  display: flex;
  align-items: center;
}

.sep {
  padding: 0 8px;
  color: #909399;
}

.hint {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
</style>
