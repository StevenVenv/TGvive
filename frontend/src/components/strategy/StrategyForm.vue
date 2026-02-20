<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules, UploadRequestOptions } from 'element-plus'
import { apiFetchBlob, uploadWatermarkFont, uploadWatermarkPNG, type CommentRule, type WatermarkRule } from '../../api'

export type ScheduleRule = {
  start: string
  end: string
  limit: number
}

export type StrategyFormModel = {
  ID: number
  name: string
  remark: string

  clone_mode: number
  allowed_types: string[]
  content_types: string[]
  block_file_exts: string[]
  allow_file_exts: string[]

  scope_type: number
  scope_value: string
  history_order: number
  poll_interval: number
  enable_realtime: boolean
  schedule_rules: ScheduleRule[]

  comment_rule: CommentRule
  watermark_rule: WatermarkRule

  keep_reply: boolean
  realtime: boolean
  clone_comment: boolean
  gpu_accel: boolean
  change_md5: boolean
  random_filename: boolean
  enable_media_edit: boolean

  delay_min_ms: number
  delay_max_ms: number

  daily_limit: number
  run_window: string
}

export type StrategyFormExpose = {
  validate: () => Promise<boolean>
  clearValidate: () => void
}

const props = withDefaults(
  defineProps<{
    modelValue: StrategyFormModel
    loading?: boolean
    submitText?: string
    cancelText?: string
    showActions?: boolean
  }>(),
  {
    loading: false,
    submitText: '保存',
    cancelText: '取消',
    showActions: true,
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: StrategyFormModel): void
  (e: 'submit'): void
  (e: 'cancel'): void
}>()

const formRef = ref<FormInstance>()
const form = computed(() => props.modelValue)

const isUploadMode = computed(() => Number(form.value.clone_mode || 3) === 3)
const mediaEditDisabled = computed(() => !isUploadMode.value)

watch(
  () => Number(form.value.clone_mode || 3),
  (mode) => {
    if (mode !== 3) {
      form.value.enable_media_edit = false
    }
  },
  { immediate: true },
)

watch(
  () => Boolean(form.value.change_md5),
  (v) => {
    if (!v) form.value.random_filename = false
  },
  { immediate: true },
)

const enablePush = computed<boolean>({
  get() {
    const v = (form.value as any).enable_realtime
    if (v === undefined || v === null) return Boolean(form.value.realtime)
    return Boolean(v)
  },
  set(v) {
    form.value.enable_realtime = Boolean(v)
    form.value.realtime = Boolean(v) // legacy field for backward compatibility
  },
})

const enablePull = computed<boolean>({
  get() {
    return Number(form.value.poll_interval ?? 0) > 0
  },
  set(v) {
    if (!v) {
      form.value.poll_interval = 0
      return
    }
    const cur = Number(form.value.poll_interval ?? 0)
    if (!Number.isFinite(cur) || cur <= 0) form.value.poll_interval = 60
  },
})

const commentCollapse = ref<string[]>(['comment'])
const mediaCollapse = ref<string[]>(['media'])

type CommentAllowedTypeKey = 'text' | 'image' | 'file' | 'video'

const commentAllowedTypeOptions: Array<{ key: CommentAllowedTypeKey; label: string; icon: string }> = [
  { key: 'text', label: '文本', icon: 'ri-file-text-line' },
  { key: 'image', label: '图片', icon: 'ri-image-line' },
  { key: 'file', label: '文件(音频/文档)', icon: 'ri-file-3-line' },
  { key: 'video', label: '视频', icon: 'ri-video-line' },
]

const commentAllowedTypeKeys = commentAllowedTypeOptions.map((x) => x.key)

function defaultCommentRule(enable: boolean): CommentRule {
  return {
    enable,
    filter_mode: 'owner_only',
    trusted_user_ids: [],
    allow_anonymous: false,
    allowed_types: ['text', 'file', 'audio'],
    block_keywords: [],
  }
}

function ensureCommentRule(): CommentRule {
  const f = form.value as any
  let r = f.comment_rule as CommentRule | undefined
  if (!r || typeof r !== 'object') {
    r = defaultCommentRule(Boolean(f.clone_comment))
    f.comment_rule = r
  }

  r.enable = Boolean((r as any).enable)
  r.filter_mode = String((r as any).filter_mode || 'owner_only')
  r.allow_anonymous = Boolean((r as any).allow_anonymous)

  r.trusted_user_ids = Array.isArray((r as any).trusted_user_ids) ? ((r as any).trusted_user_ids as number[]) : []
  r.allowed_types = Array.isArray((r as any).allowed_types) ? ((r as any).allowed_types as string[]) : []
  r.block_keywords = Array.isArray((r as any).block_keywords) ? ((r as any).block_keywords as string[]) : []

  return r
}

function defaultWatermarkRule(enable: boolean): WatermarkRule {
  return {
    enable,
    type: 'text',
    text: '',
    text_style: 'stroke',
    text_color: '#FFFFFF',
    stroke_color: '#000000',
    shadow_color: '#000000',
    font_path: '',
    image_path: '',
    position: 'bottom_right',
    custom_x: 0.5,
    custom_y: 0.5,
    margin: 0.02,
    scale_ratio: 0.03,
    opacity: 0.35,
  }
}

function clampFloat01(v: any): number {
  const n = Number(v)
  if (!Number.isFinite(n)) return 0
  if (n < 0) return 0
  if (n > 1) return 1
  return n
}

function clampInt(min: number, v: any, max: number): number {
  const n = Number(v)
  if (!Number.isFinite(n)) return min
  const i = Math.floor(n)
  if (i < min) return min
  if (i > max) return max
  return i
}

function nearlyEqual(a: number, b: number, eps = 1e-6): boolean {
  return Math.abs(a-b) <= eps
}

function ensureWatermarkRule(): WatermarkRule {
  const f = form.value as any
  let r = f.watermark_rule as WatermarkRule | undefined
  if (!r || typeof r !== 'object') {
    r = defaultWatermarkRule(false)
    f.watermark_rule = r
  }

  r.enable = Boolean((r as any).enable)
  r.type = String((r as any).type || 'text')
  r.text = String((r as any).text || '')
  r.text_style = String((r as any).text_style || 'stroke')
  r.text_color = String((r as any).text_color || '#FFFFFF')
  r.stroke_color = String((r as any).stroke_color || '#000000')
  r.shadow_color = String((r as any).shadow_color || '#000000')
  r.font_path = normalizeUploadedWatermarkPath(String((r as any).font_path || ''))
  r.image_path = normalizeUploadedWatermarkPath(String((r as any).image_path || ''))
  r.position = String((r as any).position || 'bottom_right')

  r.custom_x = clampFloat01((r as any).custom_x)
  r.custom_y = clampFloat01((r as any).custom_y)

  r.margin = clampFloat01((r as any).margin)
  if (r.margin === 0) r.margin = 0.02
  if (r.margin > 0.1) r.margin = 0.1

  r.scale_ratio = clampFloat01((r as any).scale_ratio)
  if (r.scale_ratio === 0) {
    r.scale_ratio = String(r.type).trim().toLowerCase() === 'image' ? 0.15 : 0.03
  }
  if (r.scale_ratio > 0.5) r.scale_ratio = 0.5

  r.opacity = clampFloat01((r as any).opacity)
  if (r.opacity === 0) r.opacity = 0.35

  return r
}

const watermarkEnable = computed<boolean>({
  get() {
    return Boolean(ensureWatermarkRule().enable)
  },
  set(v) {
    ensureWatermarkRule().enable = Boolean(v)
  },
})

const watermarkType = computed<'text' | 'image'>({
  get() {
    const raw = String(ensureWatermarkRule().type || '')
      .trim()
      .toLowerCase()
    if (raw === 'image') return 'image'
    return 'text'
  },
  set(v) {
    const r = ensureWatermarkRule()
    const prev = String(r.type || 'text')
      .trim()
      .toLowerCase()
    const prevType = prev === 'image' ? 'image' : 'text'
    if (prevType !== v) {
      if (prevType === 'text' && nearlyEqual(Number(r.scale_ratio || 0), 0.03)) r.scale_ratio = 0.15
      if (prevType === 'image' && nearlyEqual(Number(r.scale_ratio || 0), 0.15)) r.scale_ratio = 0.03
    }
    r.type = v
  },
})

const watermarkText = computed<string>({
  get() {
    return String(ensureWatermarkRule().text || '')
  },
  set(v) {
    ensureWatermarkRule().text = String(v || '')
  },
})

type WatermarkTextStyleKey = 'plain' | 'stroke' | 'shadow' | 'stroke_shadow'

const watermarkTextStyle = computed<WatermarkTextStyleKey>({
  get() {
    const raw = String(ensureWatermarkRule().text_style || '')
      .trim()
      .toLowerCase()
    switch (raw) {
      case 'plain':
        return 'plain'
      case 'shadow':
        return 'shadow'
      case 'stroke_shadow':
        return 'stroke_shadow'
      case 'stroke':
      default:
        return 'stroke'
    }
  },
  set(v) {
    ensureWatermarkRule().text_style = v
  },
})

const watermarkTextColor = computed<string>({
  get() {
    return String(ensureWatermarkRule().text_color || '#FFFFFF')
  },
  set(v) {
    ensureWatermarkRule().text_color = String(v || '').trim() || '#FFFFFF'
  },
})

const watermarkStrokeColor = computed<string>({
  get() {
    return String(ensureWatermarkRule().stroke_color || '#000000')
  },
  set(v) {
    ensureWatermarkRule().stroke_color = String(v || '').trim() || '#000000'
  },
})

const watermarkShadowColor = computed<string>({
  get() {
    return String(ensureWatermarkRule().shadow_color || '#000000')
  },
  set(v) {
    ensureWatermarkRule().shadow_color = String(v || '').trim() || '#000000'
  },
})

const watermarkFontPath = computed<string>({
  get() {
    return String(ensureWatermarkRule().font_path || '')
  },
  set(v) {
    ensureWatermarkRule().font_path = String(v || '').trim()
  },
})

const watermarkImagePath = computed<string>({
  get() {
    return String(ensureWatermarkRule().image_path || '')
  },
  set(v) {
    ensureWatermarkRule().image_path = String(v || '')
  },
})

const watermarkImageUploading = ref(false)
const watermarkFontUploading = ref(false)

function beforeUploadWatermarkPNG(file: File): boolean {
  if (!file) return false
  const name = String((file as any).name || '').toLowerCase()
  const isPNG = file.type === 'image/png' || name.endsWith('.png')
  if (!isPNG) {
    ElMessage.error('仅支持 PNG 文件')
    return false
  }
  const max = 5 * 1024 * 1024
  if (Number(file.size || 0) > max) {
    ElMessage.error('PNG 文件过大，最大 5MB')
    return false
  }
  return true
}

async function uploadWatermarkPNGRequest(opts: UploadRequestOptions) {
  const file = opts.file as File
  if (!file) {
    opts.onError?.(new Error('no file') as any)
    return
  }
  watermarkImageUploading.value = true
  try {
    const res = await uploadWatermarkPNG(file)
    watermarkType.value = 'image'
    watermarkImagePath.value = normalizeUploadedWatermarkPath(String(res?.name || res?.path || ''))
    ElMessage.success('水印已上传')
    opts.onSuccess?.(res as any)
  } catch (e: any) {
    ElMessage.error(e?.message || '上传失败')
    opts.onError?.(e as any)
  } finally {
    watermarkImageUploading.value = false
  }
}

function beforeUploadWatermarkFont(file: File): boolean {
  if (!file) return false
  const name = String((file as any).name || '').toLowerCase()
  const ok = name.endsWith('.ttf') || name.endsWith('.otf')
  if (!ok) {
    ElMessage.error('仅支持 .ttf/.otf 字体文件')
    return false
  }
  const max = 10 * 1024 * 1024
  if (Number(file.size || 0) > max) {
    ElMessage.error('字体文件过大，最大 10MB')
    return false
  }
  return true
}

async function uploadWatermarkFontRequest(opts: UploadRequestOptions) {
  const file = opts.file as File
  if (!file) {
    opts.onError?.(new Error('no file') as any)
    return
  }
  watermarkFontUploading.value = true
  try {
    const res = await uploadWatermarkFont(file)
    watermarkType.value = 'text'
    watermarkFontPath.value = normalizeUploadedWatermarkPath(String(res?.name || res?.path || ''))
    ElMessage.success('字体已上传')
    opts.onSuccess?.(res as any)
  } catch (e: any) {
    ElMessage.error(e?.message || '上传失败')
    opts.onError?.(e as any)
  } finally {
    watermarkFontUploading.value = false
  }
}

function pathBasename(p: string): string {
  const s = String(p || '')
    .trim()
    .replace(/\\/g, '/')
  if (!s) return ''
  const parts = s.split('/')
  return parts[parts.length - 1] || ''
}

function normalizeUploadedWatermarkPath(p: string): string {
  const s = String(p || '')
    .trim()
    .replace(/\\/g, '/')
  if (!s) return ''
  if (s.includes('/data/watermarks/') || s.startsWith('data/watermarks/')) {
    return pathBasename(s)
  }
  return s
}

const watermarkImagePreviewURL = computed<string>(() => {
  const name = pathBasename(watermarkImagePath.value)
  if (!name) return ''
  return `/api/v1/watermarks/files/${encodeURIComponent(name)}`
})

const watermarkImagePreviewSrc = ref('')
const watermarkImagePreviewLoading = ref(false)
const watermarkImagePreviewOK = ref(true)

watch(
  () => [watermarkEnable.value, watermarkType.value, watermarkImagePreviewURL.value] as const,
  async ([enable, typ, url], _, onCleanup) => {
    watermarkImagePreviewOK.value = true
    watermarkImagePreviewLoading.value = false
    watermarkImagePreviewSrc.value = ''

    if (!enable || typ !== 'image' || !url) {
      return
    }
    if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function' || typeof URL.revokeObjectURL !== 'function') {
      watermarkImagePreviewOK.value = false
      return
    }

    const ac = new AbortController()
    onCleanup(() => ac.abort())

    let objectURL = ''
    onCleanup(() => {
      if (objectURL) URL.revokeObjectURL(objectURL)
    })

    watermarkImagePreviewLoading.value = true
    try {
      const blob = await apiFetchBlob(url, { method: 'GET', signal: ac.signal })
      if (ac.signal.aborted) return
      objectURL = URL.createObjectURL(blob)
      watermarkImagePreviewSrc.value = objectURL
      watermarkImagePreviewOK.value = true
    } catch {
      if (ac.signal.aborted) return
      watermarkImagePreviewOK.value = false
      watermarkImagePreviewSrc.value = ''
    } finally {
      if (!ac.signal.aborted) watermarkImagePreviewLoading.value = false
    }
  },
  { immediate: true },
)

const watermarkFontPreviewURL = computed<string>(() => {
  const name = pathBasename(watermarkFontPath.value)
  if (!name) return ''
  return `/api/v1/watermarks/fonts/${encodeURIComponent(name)}`
})

const watermarkFontPreviewSrc = ref('')
const watermarkFontPreviewLoading = ref(false)

const watermarkPreviewFontFamily = ref('')
const watermarkPreviewFontError = ref('')

async function loadPreviewFont(url: string) {
  if (!url) {
    watermarkPreviewFontFamily.value = ''
    watermarkPreviewFontError.value = ''
    return
  }
  if (typeof FontFace === 'undefined' || typeof document === 'undefined' || !(document as any).fonts) {
    return
  }

  const key = encodeURIComponent(url).replace(/[^a-zA-Z0-9]/g, '')
  const family = `wm_${key.slice(-24) || 'custom'}`
  if (watermarkPreviewFontFamily.value === family) return

  try {
    const face = new FontFace(family, `url(${url})`)
    await face.load()
    ;(document as any).fonts.add(face)
    watermarkPreviewFontFamily.value = family
    watermarkPreviewFontError.value = ''
  } catch {
    watermarkPreviewFontFamily.value = ''
    watermarkPreviewFontError.value = '字体预览加载失败'
  }
}

watch(
  () => [watermarkEnable.value, watermarkType.value, watermarkFontPreviewURL.value] as const,
  async ([enable, typ, url], _, onCleanup) => {
    watermarkFontPreviewLoading.value = false
    watermarkFontPreviewSrc.value = ''

    if (!enable || typ !== 'text') {
      watermarkPreviewFontFamily.value = ''
      watermarkPreviewFontError.value = ''
      return
    }
    if (!url) {
      await loadPreviewFont('')
      return
    }
    if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function' || typeof URL.revokeObjectURL !== 'function') {
      watermarkPreviewFontFamily.value = ''
      watermarkPreviewFontError.value = '字体预览不支持'
      return
    }

    const ac = new AbortController()
    onCleanup(() => ac.abort())

    let objectURL = ''
    onCleanup(() => {
      if (objectURL) URL.revokeObjectURL(objectURL)
    })

    watermarkFontPreviewLoading.value = true
    try {
      const blob = await apiFetchBlob(url, { method: 'GET', signal: ac.signal })
      if (ac.signal.aborted) return
      objectURL = URL.createObjectURL(blob)
      watermarkFontPreviewSrc.value = objectURL
      await loadPreviewFont(objectURL)
    } catch {
      if (ac.signal.aborted) return
      watermarkPreviewFontFamily.value = ''
      watermarkPreviewFontError.value = '字体预览加载失败'
    } finally {
      if (!ac.signal.aborted) watermarkFontPreviewLoading.value = false
    }
  },
  { immediate: true },
)

function previewTextShadow(style: WatermarkTextStyleKey, strokeColor: string, shadowColor: string): string {
  const shadows: string[] = []
  const withStroke = style === 'stroke' || style === 'stroke_shadow'
  const withShadow = style === 'shadow' || style === 'stroke_shadow'
  if (withShadow) shadows.push(`2px 2px 0 ${shadowColor}`)
  if (withStroke) {
    const offsets: Array<[number, number]> = [
      [-2, 0],
      [2, 0],
      [0, -2],
      [0, 2],
      [-2, -2],
      [2, 2],
      [-2, 2],
      [2, -2],
    ]
    for (const [dx, dy] of offsets) {
      shadows.push(`${dx}px ${dy}px 0 ${strokeColor}`)
    }
  }
  return shadows.join(', ')
}

const watermarkPreviewTextStyle = computed<Record<string, string>>(() => {
  const r = ensureWatermarkRule()
  const baseW = 1080
  let fontSize = baseW * clampFloat01(Number(r.scale_ratio || 0))
  if (!Number.isFinite(fontSize) || fontSize <= 0) fontSize = 32
  if (fontSize < 12) fontSize = 12
  if (fontSize > 72) fontSize = 72

  const ff = watermarkPreviewFontFamily.value
  const family = ff ? `'${ff}', sans-serif` : 'inherit'

  return {
    fontFamily: family,
    fontSize: `${Math.round(fontSize)}px`,
    color: watermarkTextColor.value,
    opacity: String(clampFloat01(Number(r.opacity || 0.35)) || 0.35),
    textShadow: previewTextShadow(watermarkTextStyle.value, watermarkStrokeColor.value, watermarkShadowColor.value),
    whiteSpace: 'pre-line',
    lineHeight: '1.2',
  }
})

type WatermarkPositionKey = 'bottom_right' | 'bottom_left' | 'top_right' | 'top_left' | 'center' | 'custom'

const watermarkPosition = computed<WatermarkPositionKey>({
  get() {
    const raw = String(ensureWatermarkRule().position || '')
      .trim()
      .toLowerCase()
    switch (raw) {
      case 'bottom_left':
        return 'bottom_left'
      case 'top_right':
        return 'top_right'
      case 'top_left':
        return 'top_left'
      case 'center':
        return 'center'
      case 'custom':
        return 'custom'
      case 'bottom_right':
      default:
        return 'bottom_right'
    }
  },
  set(v) {
    ensureWatermarkRule().position = v
  },
})

const watermarkCustomX = computed<number>({
  get() {
    return clampInt(0, Math.round(clampFloat01(ensureWatermarkRule().custom_x) * 100), 100)
  },
  set(v) {
    ensureWatermarkRule().custom_x = clampInt(0, v, 100) / 100
  },
})

const watermarkCustomY = computed<number>({
  get() {
    return clampInt(0, Math.round(clampFloat01(ensureWatermarkRule().custom_y) * 100), 100)
  },
  set(v) {
    ensureWatermarkRule().custom_y = clampInt(0, v, 100) / 100
  },
})

const watermarkMarginPct = computed<number>({
  get() {
    return clampInt(0, Math.round(clampFloat01(ensureWatermarkRule().margin) * 100), 10)
  },
  set(v) {
    ensureWatermarkRule().margin = clampInt(0, v, 10) / 100
  },
})

const watermarkScalePct = computed<number>({
  get() {
    return clampInt(1, Math.round(clampFloat01(ensureWatermarkRule().scale_ratio) * 100), 50)
  },
  set(v) {
    ensureWatermarkRule().scale_ratio = clampInt(1, v, 50) / 100
  },
})

const watermarkOpacityPct = computed<number>({
  get() {
    return clampInt(0, Math.round(clampFloat01(ensureWatermarkRule().opacity) * 100), 100)
  },
  set(v) {
    ensureWatermarkRule().opacity = clampInt(0, v, 100) / 100
  },
})

const commentEnable = computed<boolean>({
  get() {
    return Boolean(ensureCommentRule().enable)
  },
  set(v) {
    const on = Boolean(v)
    const r = ensureCommentRule()
    r.enable = on
    form.value.clone_comment = on // legacy sync

    if (on && (!Array.isArray(r.allowed_types) || r.allowed_types.length === 0)) {
      r.allowed_types = ['text', 'file', 'audio']
    }
  },
})

watch(
  () => Boolean(form.value.clone_comment),
  (v) => {
    const r = ensureCommentRule()
    if (r.enable !== v) r.enable = v
  },
  { immediate: true },
)

const commentFilterMode = computed<'owner_only' | 'all'>({
  get() {
    const raw = String(ensureCommentRule().filter_mode || '')
      .trim()
      .toLowerCase()
    if (raw === 'all') return 'all'
    return 'owner_only'
  },
  set(v) {
    ensureCommentRule().filter_mode = v
  },
})

function normalizeCommentAllowedTypes(input: any): CommentAllowedTypeKey[] {
  const raw = Array.isArray(input) ? input : []
  const set = new Set<string>()
  for (const v of raw) {
    const k = String(v || '')
      .trim()
      .toLowerCase()
    if (!k) continue
    set.add(k)
  }
  // Backend may store "audio" separately; UI merges it into "file".
  if (set.has('audio')) set.add('file')
  const out: CommentAllowedTypeKey[] = []
  for (const k of commentAllowedTypeKeys) {
    if (set.has(k)) out.push(k)
  }
  return out
}

function buildCommentAllowedTypesPayload(input: any): string[] {
  const keys = normalizeCommentAllowedTypes(input)
  const out = new Set<string>(keys)
  // UI "file" covers both file + audio.
  if (out.has('file')) out.add('audio')
  return Array.from(out)
}

const commentAllowedTypes = computed<CommentAllowedTypeKey[]>({
  get() {
    return normalizeCommentAllowedTypes(ensureCommentRule().allowed_types)
  },
  set(v) {
    ensureCommentRule().allowed_types = buildCommentAllowedTypesPayload(v)
  },
})

const commentAllowAnonymous = computed<boolean>({
  get() {
    return Boolean(ensureCommentRule().allow_anonymous)
  },
  set(v) {
    ensureCommentRule().allow_anonymous = Boolean(v)
  },
})

const commentTrustedUserIDsText = ref('')
const commentBlockKeywordsText = ref('')

watch(
  () => ensureCommentRule().trusted_user_ids.join(','),
  (v) => {
    commentTrustedUserIDsText.value = v
  },
  { immediate: true },
)

watch(
  () => ensureCommentRule().block_keywords.join('\n'),
  (v) => {
    commentBlockKeywordsText.value = v
  },
  { immediate: true },
)

function parseTrustedUserIDs(input: string): number[] {
  const raw = String(input || '')
    .trim()
    .split(/[\s,]+/)
    .map((x) => x.trim())
    .filter(Boolean)
  const out: number[] = []
  const seen = new Set<number>()
  for (const part of raw) {
    const n = Number(part)
    if (!Number.isFinite(n) || n <= 0) continue
    const id = Math.floor(n)
    if (!Number.isFinite(id) || id <= 0) continue
    if (seen.has(id)) continue
    seen.add(id)
    out.push(id)
  }
  return out
}

function parseBlockKeywords(input: string): string[] {
  const raw = String(input || '')
    .split(/\r?\n/)
    .map((x) => x.trim())
    .filter(Boolean)
  const out: string[] = []
  const seen = new Set<string>()
  for (const w of raw) {
    const k = w.toLowerCase()
    if (seen.has(k)) continue
    seen.add(k)
    out.push(w)
  }
  return out
}

function normalizeScheduleRules(input: any): ScheduleRule[] {
  const raw = Array.isArray(input) ? input : []
  const out: ScheduleRule[] = []
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue
    const start = String((item as any).start ?? '').trim()
    const end = String((item as any).end ?? '').trim()
    const limit = Number((item as any).limit ?? 0)
    if (!start || !end) continue
    if (!Number.isFinite(limit) || limit <= 0) continue
    out.push({ start, end, limit: Math.floor(limit) })
  }
  return out
}

function ensureScheduleRulesArray() {
  if (!Array.isArray(form.value.schedule_rules)) form.value.schedule_rules = []
}

function addScheduleRule() {
  ensureScheduleRulesArray()
  form.value.schedule_rules = [...form.value.schedule_rules, { start: '', end: '', limit: 1 }]
}

function removeScheduleRule(index: number) {
  ensureScheduleRulesArray()
  form.value.schedule_rules = form.value.schedule_rules.filter((_, i) => i !== index)
}

type AllowedTypeKey = 'text' | 'image' | 'video' | 'audio' | 'file'

const allowedTypeOptions: Array<{ key: AllowedTypeKey; label: string; icon: string }> = [
  { key: 'text', label: '文本', icon: 'ri-file-text-line' },
  { key: 'image', label: '图片', icon: 'ri-image-line' },
  { key: 'video', label: '视频', icon: 'ri-video-line' },
  { key: 'audio', label: '音频', icon: 'ri-volume-up-line' },
  { key: 'file', label: '文件', icon: 'ri-file-3-line' },
]

const allowedTypeKeys = allowedTypeOptions.map((x) => x.key)

const scopeValuePlaceholder = computed(() => {
  const t = Number(form.value.scope_type || 1)
  if (t === 2) return '例如：100'
  if (t === 3) return '例如：2025-01-01~2025-01-31'
  if (t === 4) return '例如：1000-2000'
  return '可留空'
})

function normalizeFileExtList(input: any): string[] {
  const raw = Array.isArray(input) ? input : []
  const seen = new Set<string>()
  const out: string[] = []
  for (const v of raw) {
    let k = String(v || '')
      .trim()
      .toLowerCase()
    if (!k) continue
    if (!k.startsWith('.')) k = '.' + k
    if (k === '.') continue
    if (/[ \t\r\n/\\]/.test(k)) continue
    if (seen.has(k)) continue
    seen.add(k)
    out.push(k)
  }
  return out
}

const fileExtSuggestions = ['.zip', '.rar', '.7z', '.apk', '.exe', '.dmg', '.pkg', '.pdf', '.docx', '.xlsx', '.pptx']

const blockFileExtOptions = computed(() => {
  const set = new Set<string>()
  normalizeFileExtList((form.value as any).block_file_exts).forEach((v) => set.add(v))
  fileExtSuggestions.forEach((v) => set.add(v))
  return Array.from(set)
})

const allowFileExtOptions = computed(() => {
  const set = new Set<string>()
  normalizeFileExtList((form.value as any).allow_file_exts).forEach((v) => set.add(v))
  fileExtSuggestions.forEach((v) => set.add(v))
  return Array.from(set)
})

const blockFileExts = computed<string[]>({
  get() {
    return normalizeFileExtList((form.value as any).block_file_exts)
  },
  set(v) {
    ;(form.value as any).block_file_exts = normalizeFileExtList(v)
  },
})

const allowFileExts = computed<string[]>({
  get() {
    return normalizeFileExtList((form.value as any).allow_file_exts)
  },
  set(v) {
    ;(form.value as any).allow_file_exts = normalizeFileExtList(v)
  },
})

function normalizeAllowedTypes(input: any): AllowedTypeKey[] {
  const raw = Array.isArray(input) ? input : []
  const set = new Set<string>()
  for (const v of raw) {
    const k = String(v || '')
      .trim()
      .toLowerCase()
    if (!k) continue
    set.add(k)
  }
  const out: AllowedTypeKey[] = []
  for (const k of allowedTypeKeys) {
    if (set.has(k)) out.push(k)
  }
  return out
}

const allowedTypes = computed<AllowedTypeKey[]>({
  get() {
    const v = (form.value as any).allowed_types
    if (Array.isArray(v)) return normalizeAllowedTypes(v)
    return normalizeAllowedTypes(form.value.content_types)
  },
  set(v) {
    const next = normalizeAllowedTypes(v)
    ;(form.value as any).allowed_types = next
    form.value.content_types = next
  },
})

const rules: FormRules = {
  name: [{ required: true, message: '请填写策略名称', trigger: 'blur' }],
  poll_interval: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        if (!Number.isFinite(n)) cb(new Error('轮询间隔不合法'))
        else if (n < 0) cb(new Error('轮询间隔不能为负数'))
        else if (n > 0 && n < 10) cb(new Error('轮询间隔最小 10 秒'))
        else cb()
      },
      trigger: 'change',
    },
  ],
  delay_min_ms: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        if (!Number.isFinite(n) || n < 0) cb(new Error('最小延时不能为负数'))
        else cb()
      },
      trigger: 'change',
    },
  ],
  delay_max_ms: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        const min = Number(form.value.delay_min_ms ?? 0)
        if (!Number.isFinite(n) || n < 0) cb(new Error('最大延时不能为负数'))
        else if (n < min) cb(new Error('最大延时必须 ≥ 最小延时'))
        else cb()
      },
      trigger: 'change',
    },
  ],
  daily_limit: [
    {
      validator: (_: any, v: any, cb: any) => {
        const n = Number(v ?? 0)
        if (!Number.isFinite(n) || n < 0) cb(new Error('每日配额不能为负数'))
        else cb()
      },
      trigger: 'change',
    },
  ],
}

async function validate(): Promise<boolean> {
  const inst = formRef.value
  if (!inst) return false
  try {
    return await inst.validate()
  } catch {
    return false
  }
}

function clearValidate() {
  formRef.value?.clearValidate()
}

async function submit() {
  // keep legacy + new fields in sync
  form.value.enable_realtime = enablePush.value
  form.value.realtime = enablePush.value

  if (!enablePull.value) {
    form.value.poll_interval = 0
  }
  ;(form.value as any).block_file_exts = normalizeFileExtList((form.value as any).block_file_exts)
  ;(form.value as any).allow_file_exts = normalizeFileExtList((form.value as any).allow_file_exts)
  form.value.schedule_rules = normalizeScheduleRules(form.value.schedule_rules)

  // normalize comment_rule + sync legacy flag
  {
    const r = ensureCommentRule()
    r.enable = Boolean(commentEnable.value)
    r.filter_mode = commentFilterMode.value
    r.trusted_user_ids = parseTrustedUserIDs(commentTrustedUserIDsText.value)
    r.allowed_types = buildCommentAllowedTypesPayload(r.allowed_types)
    r.allow_anonymous = Boolean(r.allow_anonymous)
    r.block_keywords = parseBlockKeywords(commentBlockKeywordsText.value)

    if (r.enable && (!Array.isArray(r.allowed_types) || r.allowed_types.length === 0)) {
      r.allowed_types = ['text', 'file', 'audio']
    }
    form.value.clone_comment = Boolean(r.enable)
    ;(form.value as any).comment_rule = r
  }

  // normalize watermark_rule (percent sliders -> float)
  {
    const r = ensureWatermarkRule()
    r.enable = Boolean(watermarkEnable.value)
    r.type = watermarkType.value
    r.text = String(r.text || '').trim()
    r.text_style = watermarkTextStyle.value
    r.text_color = String(watermarkTextColor.value || '').trim() || '#FFFFFF'
    r.stroke_color = String(watermarkStrokeColor.value || '').trim() || '#000000'
    r.shadow_color = String(watermarkShadowColor.value || '').trim() || '#000000'
    r.font_path = String(watermarkFontPath.value || '').trim()
    r.image_path = String(r.image_path || '').trim()
    r.position = watermarkPosition.value

    r.custom_x = clampFloat01(r.custom_x)
    r.custom_y = clampFloat01(r.custom_y)

    r.margin = clampFloat01(r.margin)
    if (r.margin === 0) r.margin = 0.02
    if (r.margin > 0.1) r.margin = 0.1

    r.scale_ratio = clampFloat01(r.scale_ratio)
    if (r.scale_ratio === 0) r.scale_ratio = r.type === 'image' ? 0.15 : 0.03
    if (r.scale_ratio > 0.5) r.scale_ratio = 0.5

    r.opacity = clampFloat01(r.opacity)
    if (r.opacity === 0) r.opacity = 0.35

    ;(form.value as any).watermark_rule = r
  }

  const ok = await validate()
  if (!ok) return
  emit('submit')
}

function cancel() {
  emit('cancel')
}

defineExpose<StrategyFormExpose>({
  validate,
  clearValidate,
})
</script>

<template>
  <div class="strategy-form">
    <div class="form-scroll" v-loading="loading">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" size="small" class="form">
        <el-row :gutter="12">
          <el-col :xs="24" :sm="12">
            <el-form-item label="策略名称" prop="name">
              <el-input v-model="form.name" placeholder="例如：极速转发 / 去重+转码" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12">
            <el-form-item label="备注（可选）" prop="remark">
              <el-input v-model="form.remark" placeholder="用于团队协作/区分用途" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-card class="panel-card" shadow="never">
          <template #header>
            <div class="panel-head">
              <div class="panel-title">
                <i class="ri-sliders-2-line" />
                <span>基础配置</span>
              </div>
              <div class="panel-sub">Base Config</div>
            </div>
          </template>

          <el-row :gutter="12">
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="克隆模式" prop="clone_mode">
                <el-radio-group v-model="form.clone_mode" class="radio-dense">
                  <el-radio :label="1">转发</el-radio>
                  <el-radio :label="2">发送</el-radio>
                  <el-radio :label="3">下载上传</el-radio>
                </el-radio-group>
              </el-form-item>
            </el-col>

            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="历史方向" prop="history_order">
                <el-radio-group v-model="form.history_order" class="radio-dense">
                  <el-radio :label="1">从旧到新</el-radio>
                  <el-radio :label="2">从新到旧</el-radio>
                </el-radio-group>
              </el-form-item>
            </el-col>

            <el-col :xs="24" :sm="8" :lg="4">
              <el-form-item label="消息范围" prop="scope_type">
                <el-select v-model="form.scope_type" class="ctrl ctrl-sm" popper-class="tgvive-dark-popper">
                  <el-option :value="1" label="全部" />
                  <el-option :value="2" label="最近 N 条" />
                  <el-option :value="3" label="时间范围" />
                  <el-option :value="4" label="ID 范围" />
                </el-select>
              </el-form-item>
            </el-col>

            <el-col :xs="24" :sm="16" :lg="8">
              <el-form-item label="范围参数" prop="scope_value">
                <el-input v-model="form.scope_value" class="ctrl ctrl-md" :placeholder="scopeValuePlaceholder" />
              </el-form-item>
            </el-col>
          </el-row>
        </el-card>

        <el-card class="panel-card" shadow="never">
          <template #header>
            <div class="panel-head">
              <div class="panel-title">
                <i class="ri-shield-flash-line" />
                <span>风控与处理</span>
              </div>
              <div class="panel-sub">Anti-detect & Processing</div>
            </div>
          </template>

          <el-form-item label="允许转发的内容类型" prop="allowed_types">
            <el-checkbox-group v-model="allowedTypes" class="types-group">
              <el-checkbox v-for="opt in allowedTypeOptions" :key="opt.key" :label="opt.key" class="type-item">
                <i :class="opt.icon" />
                <span class="type-label">{{ opt.label }}</span>
              </el-checkbox>
            </el-checkbox-group>
            <div class="hint compact">未选择表示不过滤（全类型，包括未知类型）</div>
          </el-form-item>

          <el-row :gutter="12">
            <el-col :xs="24" :sm="12">
              <el-form-item prop="block_file_exts">
                <template #label>
                  <span class="label-with-icon">
                    <i class="ri-forbid-2-line" />
                    <span>屏蔽文件后缀</span>
                  </span>
                </template>
                <el-select
                  v-model="blockFileExts"
                  class="ctrl"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  reserve-keyword
                  clearable
                  collapse-tags
                  collapse-tags-tooltip
                  placeholder="例如：.apk .zip（回车添加）"
                  popper-class="tgvive-dark-popper"
                >
                  <el-option v-for="opt in blockFileExtOptions" :key="opt" :label="opt" :value="opt" />
                </el-select>
                <div class="hint compact">命中后缀：该文件将被跳过</div>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12">
              <el-form-item prop="allow_file_exts">
                <template #label>
                  <span class="label-with-icon">
                    <i class="ri-shield-check-line" />
                    <span>保留文件后缀（白名单）</span>
                  </span>
                </template>
                <el-select
                  v-model="allowFileExts"
                  class="ctrl"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  reserve-keyword
                  clearable
                  collapse-tags
                  collapse-tags-tooltip
                  placeholder="为空表示不过滤（全放行）"
                  popper-class="tgvive-dark-popper"
                >
                  <el-option v-for="opt in allowFileExtOptions" :key="opt" :label="opt" :value="opt" />
                </el-select>
                <div class="hint compact">设置后，仅保留这些后缀的文件</div>
              </el-form-item>
            </el-col>
          </el-row>
          <div class="hint compact">仅对“文件”类型生效（非图片/音频/视频）。支持 zip 或 .zip，不区分大小写。</div>

          <div class="monitor-box">
            <div class="monitor-pane">
              <el-form-item class="monitor-item">
                <template #label>
                  <div class="monitor-label">
                    <i class="ri-broadcast-line" />
                    <span>实时监听 (Push)</span>
                  </div>
                </template>
                <el-switch v-model="enablePush" inline-prompt active-text="开" inactive-text="关" />
                <div class="hint compact">通过 Telegram 推送事件触发转发</div>
              </el-form-item>
            </div>

            <div class="monitor-pane">
              <el-form-item prop="poll_interval" class="monitor-item">
                <template #label>
                  <div class="monitor-label">
                    <i class="ri-loop-right-line" />
                    <span>定时兜底 (Pull)</span>
                    <el-tooltip effect="dark" placement="top" content="用于兜底：按间隔主动检测最新消息，建议间隔 60 秒以上。">
                      <i class="ri-question-line monitor-tip" />
                    </el-tooltip>
                  </div>
                </template>

                <div class="poll-mode">
                  <el-switch v-model="enablePull" inline-prompt active-text="开" inactive-text="关" />
                  <div v-if="enablePull" class="poll-interval">
                    <el-input-number v-model="form.poll_interval" :min="10" :step="10" controls-position="right" class="poll-input" />
                    <span class="poll-unit">秒</span>
                  </div>
                </div>
              </el-form-item>
            </div>
          </div>

          <el-row :gutter="12">
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="最小延时 (ms)" prop="delay_min_ms">
                <el-input-number v-model="form.delay_min_ms" :min="0" :step="100" controls-position="right" class="ctrl ctrl-num" />
                <div class="hint compact">每条消息处理后随机 sleep</div>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="最大延时 (ms)" prop="delay_max_ms">
                <el-input-number v-model="form.delay_max_ms" :min="0" :step="100" controls-position="right" class="ctrl ctrl-num" />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="每日配额" prop="daily_limit">
                <el-input-number v-model="form.daily_limit" :min="0" :step="10" controls-position="right" class="ctrl ctrl-num" />
                <div class="hint compact">0 = 不限制</div>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :lg="6">
              <el-form-item label="运行窗口" prop="run_window">
                <el-input v-model="form.run_window" class="ctrl ctrl-sm" placeholder="09:00-18:00（留空=全天）" />
              </el-form-item>
            </el-col>
          </el-row>

          <div class="sub-split">
            <span>分时段计划</span>
          </div>

          <el-table :data="form.schedule_rules" size="small" border class="schedule-table" empty-text="未配置">
            <el-table-column label="开始时间" width="150">
              <template #default="{ row }">
                <el-time-picker
                  v-model="row.start"
                  value-format="HH:mm"
                  format="HH:mm"
                  placeholder="HH:mm"
                  class="ctrl ctrl-time"
                  popper-class="tgvive-dark-popper"
                />
              </template>
            </el-table-column>
            <el-table-column label="结束时间" width="150">
              <template #default="{ row }">
                <el-time-picker
                  v-model="row.end"
                  value-format="HH:mm"
                  format="HH:mm"
                  placeholder="HH:mm"
                  class="ctrl ctrl-time"
                  popper-class="tgvive-dark-popper"
                />
              </template>
            </el-table-column>
            <el-table-column label="时段配额" width="160">
              <template #default="{ row }">
                <el-input-number v-model="row.limit" :min="1" :step="1" controls-position="right" class="ctrl ctrl-num" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="90" align="right">
              <template #default="{ $index }">
                <el-button type="danger" link size="small" @click="removeScheduleRule($index)">
                  <i class="ri-delete-bin-line" />
                </el-button>
              </template>
            </el-table-column>
          </el-table>

          <div class="schedule-actions">
            <el-button type="primary" plain size="small" @click="addScheduleRule">
              <i class="ri-add-line" />
              添加时段
            </el-button>
            <div class="hint">示例：10:00-11:00 配额 2；12:00-13:00 配额 4</div>
          </div>

          <div class="sub-split">
            <span>评论区设置</span>
          </div>

          <el-collapse v-model="commentCollapse" class="comment-collapse">
            <el-collapse-item name="comment" title="评论区设置">
              <el-row :gutter="12">
                <el-col :xs="24" :sm="8">
                  <el-form-item label="启用评论克隆">
                    <el-switch v-model="commentEnable" inline-prompt active-text="开" inactive-text="关" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="16">
                  <el-form-item label="模式">
                    <el-radio-group v-model="commentFilterMode" class="radio-dense" :disabled="!commentEnable">
                      <el-radio label="owner_only">仅官方/白名单（推荐）</el-radio>
                      <el-radio label="all">所有人（慎用）</el-radio>
                    </el-radio-group>
                    <div class="hint compact">采用任务级独立本地库缓存机制，主频道配额不受评论影响。支持 FloodWait 断点续传。</div>
                  </el-form-item>
                </el-col>
              </el-row>

              <el-row :gutter="12">
                <el-col :xs="24" :sm="16">
                  <el-form-item label="白名单 UserID（逗号/空格分隔）">
                    <el-input
                      v-model="commentTrustedUserIDsText"
                      type="textarea"
                      :rows="2"
                      :disabled="!commentEnable"
                      placeholder="例如：123456789, 987654321"
                    />
                    <div class="hint compact">仅搬运这些账号或楼主本人在评论区的发言</div>
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="8">
                  <el-form-item label="匿名管理员">
                    <el-switch v-model="commentAllowAnonymous" inline-prompt active-text="允" inactive-text="拒" :disabled="!commentEnable" />
                    <div class="hint compact">允许 GroupAnonymousBot</div>
                  </el-form-item>
                </el-col>
              </el-row>

              <el-form-item label="评论区媒体类型（独立规则）">
                <el-checkbox-group v-model="commentAllowedTypes" :disabled="!commentEnable" class="types-group">
                  <el-checkbox v-for="opt in commentAllowedTypeOptions" :key="opt.key" :label="opt.key" class="type-item">
                    <i :class="opt.icon" />
                    <span class="type-label">{{ opt.label }}</span>
                  </el-checkbox>
                </el-checkbox-group>
              </el-form-item>

              <el-form-item label="垃圾词黑名单（每行一个）">
                <el-input
                  v-model="commentBlockKeywordsText"
                  type="textarea"
                  :rows="3"
                  :disabled="!commentEnable"
                  placeholder="例如：免费\n加群\n私聊"
                />
              </el-form-item>
            </el-collapse-item>
          </el-collapse>

          <div class="sub-split">
            <span>媒体加工</span>
          </div>

          <el-collapse v-model="mediaCollapse" class="comment-collapse">
            <el-collapse-item name="media">
              <template #title>
                <span class="label-with-icon">
                  <i class="ri-brush-line" />
                  <span>媒体加工</span>
                </span>
              </template>

              <el-row :gutter="12">
                <el-col :xs="24" :sm="8">
                  <el-form-item label="启用水印">
                    <el-switch v-model="watermarkEnable" inline-prompt active-text="开" inactive-text="关" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="16">
                  <div class="hint compact">开启后，对图片做纯内存水印渲染并重新上传发送（评论 + 主贴）。</div>
                </el-col>
              </el-row>

              <el-row v-if="watermarkEnable" :gutter="12">
                <el-col :xs="24" :sm="12">
                  <el-form-item label="水印类型">
                    <el-radio-group v-model="watermarkType" class="radio-dense">
                      <el-radio label="text">文字水印</el-radio>
                      <el-radio label="image">图片水印 (PNG)</el-radio>
                    </el-radio-group>
                  </el-form-item>
                </el-col>

                <el-col v-if="watermarkType === 'text'" :xs="24" :sm="12">
                  <el-form-item label="文字内容">
                    <el-input v-model="watermarkText" placeholder="例如：@MyChannel" />
                  </el-form-item>
                </el-col>
                <el-col v-else :xs="24" :sm="12">
	                  <el-form-item label="PNG 路径/文件名">
	                    <div class="wm-upload">
	                      <el-input v-model="watermarkImagePath" placeholder="例如：wm_xxx.png 或 /var/www/watermark/logo.png" />
	                      <el-upload
	                        :show-file-list="false"
	                        accept="image/png"
	                        :before-upload="beforeUploadWatermarkPNG"
                        :http-request="uploadWatermarkPNGRequest"
                        :disabled="watermarkImageUploading"
                      >
                        <el-button plain size="small" :loading="watermarkImageUploading">
                          <i class="ri-upload-2-line" />
                          上传 PNG
                        </el-button>
	                      </el-upload>
	                    </div>
	                    <div class="hint compact">上传后仅保存文件名（data/watermarks/ 下），避免暴露本机绝对路径。</div>
	                  </el-form-item>
	                </el-col>
	              </el-row>

              <el-row v-if="watermarkEnable && watermarkType === 'text'" :gutter="12">
                <el-col :xs="24" :sm="8">
                  <el-form-item label="文字样式">
                    <el-select v-model="watermarkTextStyle" class="ctrl ctrl-sm" popper-class="tgvive-dark-popper">
                      <el-option value="stroke" label="描边" />
                      <el-option value="shadow" label="阴影" />
                      <el-option value="stroke_shadow" label="描边 + 阴影" />
                      <el-option value="plain" label="无" />
                    </el-select>
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="8">
                  <el-form-item label="文字颜色">
                    <el-color-picker v-model="watermarkTextColor" color-format="hex" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="8">
                  <el-form-item :label="watermarkTextStyle === 'shadow' ? '阴影颜色' : '描边颜色'">
                    <el-color-picker v-if="watermarkTextStyle === 'shadow'" v-model="watermarkShadowColor" color-format="hex" />
                    <el-color-picker v-else v-model="watermarkStrokeColor" color-format="hex" />
                  </el-form-item>
                </el-col>
              </el-row>

              <el-row v-if="watermarkEnable && watermarkType === 'text'" :gutter="12">
                <el-col :xs="24">
	                  <el-form-item label="自定义字体（可选）">
	                    <div class="wm-upload">
	                      <el-input v-model="watermarkFontPath" placeholder="例如：font_xxx.ttf 或 /abs/custom.ttf" />
	                      <el-upload
	                        :show-file-list="false"
	                        accept=".ttf,.otf"
	                        :before-upload="beforeUploadWatermarkFont"
                        :http-request="uploadWatermarkFontRequest"
                        :disabled="watermarkFontUploading"
                      >
                        <el-button plain size="small" :loading="watermarkFontUploading">
                          <i class="ri-upload-2-line" />
                          上传字体
                        </el-button>
	                      </el-upload>
	                    </div>
	                    <div class="hint compact">上传后仅保存文件名（data/watermarks/fonts/ 下），并自动用于预览与渲染。</div>
	                    <div v-if="watermarkPreviewFontError" class="hint compact">{{ watermarkPreviewFontError }}</div>
	                  </el-form-item>
	                </el-col>
	              </el-row>

              <el-row v-if="watermarkEnable" :gutter="12">
                <el-col :xs="24" :sm="12">
                  <el-form-item label="位置">
                    <el-select v-model="watermarkPosition" class="ctrl ctrl-sm" popper-class="tgvive-dark-popper">
                      <el-option value="bottom_right" label="右下角" />
                      <el-option value="bottom_left" label="左下角" />
                      <el-option value="top_right" label="右上角" />
                      <el-option value="top_left" label="左上角" />
                      <el-option value="center" label="正中心" />
                      <el-option value="custom" label="自定义坐标" />
                    </el-select>
                  </el-form-item>
                </el-col>
              </el-row>

              <el-row v-if="watermarkEnable && watermarkPosition === 'custom'" :gutter="12">
                <el-col :xs="24" :sm="12">
                  <el-form-item label="自定义 X（%）">
                    <el-slider v-model="watermarkCustomX" :min="0" :max="100" :step="1" show-input />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="12">
                  <el-form-item label="自定义 Y（%）">
                    <el-slider v-model="watermarkCustomY" :min="0" :max="100" :step="1" show-input />
                  </el-form-item>
                </el-col>
              </el-row>

	              <el-row v-if="watermarkEnable" :gutter="12">
	                <el-col :xs="24" :sm="8">
	                  <el-form-item label="边距（0-10%）">
	                    <el-slider v-model="watermarkMarginPct" :min="0" :max="10" :step="1" show-input />
	                  </el-form-item>
	                </el-col>
	                <el-col :xs="24" :sm="8">
	                  <el-form-item label="缩放占比（1-50%）">
	                    <el-slider v-model="watermarkScalePct" :min="1" :max="50" :step="1" show-input />
	                    <div class="hint compact">控制水印占画面宽度的比例。</div>
	                  </el-form-item>
	                </el-col>
	                <el-col :xs="24" :sm="8">
	                  <el-form-item label="透明度（0-100%）">
	                    <el-slider v-model="watermarkOpacityPct" :min="0" :max="100" :step="1" show-input />
	                    <div class="hint compact">100% 为完全不透明。</div>
	                  </el-form-item>
	                </el-col>
	              </el-row>

	              <div v-if="watermarkEnable" class="wm-preview">
	                <div class="wm-preview-head">
	                  <i class="ri-eye-line" />
	                  <span>水印预览</span>
	                </div>
	                <div class="wm-preview-stage">
	                  <template v-if="watermarkType === 'text'">
	                    <div class="wm-preview-text" :style="watermarkPreviewTextStyle">
	                      {{ watermarkText || '@Preview' }}
	                    </div>
		                  </template>
		                  <template v-else>
		                    <img
		                      v-if="watermarkImagePreviewSrc && watermarkImagePreviewOK"
		                      :src="watermarkImagePreviewSrc"
		                      class="wm-preview-img"
		                      @error="watermarkImagePreviewOK = false"
		                    />
		                    <div v-else class="hint compact">{{ watermarkImagePreviewLoading ? '水印加载中…' : '无可预览图片（请先上传 PNG）' }}</div>
		                  </template>
		                </div>
		              </div>
		            </el-collapse-item>
	          </el-collapse>

          <div class="sub-split">
            <span>处理开关</span>
          </div>

          <div class="switch-wrap">
            <el-switch v-model="form.keep_reply" active-text="保留回复" />
            <el-tooltip effect="dark" placement="top" content="媒体编辑仅在‘上传模式’下可用" :disabled="!mediaEditDisabled">
              <span class="switch-tooltip">
                <el-switch v-model="form.enable_media_edit" :disabled="mediaEditDisabled" active-text="媒体编辑" />
              </span>
            </el-tooltip>
            <el-switch v-model="form.gpu_accel" active-text="GPU 加速" />
            <el-switch v-model="form.change_md5" active-text="修改 MD5" />
            <el-tooltip effect="dark" placement="top" content="需先开启“修改 MD5”" :disabled="form.change_md5">
              <span class="switch-tooltip">
                <el-switch v-model="form.random_filename" :disabled="!form.change_md5" active-text="随机文件名" />
              </span>
            </el-tooltip>
          </div>
        </el-card>
      </el-form>
    </div>

    <div v-if="showActions" class="actions">
      <slot name="actions">
        <el-space>
          <el-button @click="cancel">{{ cancelText }}</el-button>
          <el-button type="primary" @click="submit">{{ submitText }}</el-button>
        </el-space>
      </slot>
    </div>
  </div>
</template>

<style scoped lang="scss">
.strategy-form {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.form-scroll {
  flex: 1;
  overflow: auto;
  padding-right: 2px;
}

.form {
  width: 100%;
  max-width: 1200px;
  margin: 0 auto;
}

.form :deep(.el-form-item) {
  margin-bottom: 12px;
}

.form :deep(.el-form-item__label) {
  padding: 0 0 6px;
  line-height: 1.15;
  color: #a6a9ad;
}

.hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.hint.compact {
  margin-top: 6px;
}

.label-with-icon {
  display: inline-flex;
  align-items: center;
  gap: 8px;

  i {
    font-size: 14px;
    color: var(--el-color-primary);
  }
}

.radio-dense :deep(.el-radio) {
  margin-right: 12px;
}

.wm-upload {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;

  :deep(.el-input) {
    flex: 1 1 auto;
  }
}

.wm-preview {
  margin-top: 10px;
  border: 1px solid #363637;
  border-radius: 6px;
  background: #161616;
  overflow: hidden;
}

.wm-preview-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-bottom: 1px solid #2a2a2a;
  background: #202020;
  color: var(--el-text-color-primary);

  i {
    color: var(--el-color-primary);
  }
}

.wm-preview-stage {
  padding: 12px;
  min-height: 84px;
  display: flex;
  align-items: center;
  justify-content: center;
  background:
    linear-gradient(45deg, rgba(255, 255, 255, 0.06) 25%, transparent 25%, transparent 75%, rgba(255, 255, 255, 0.06) 75%),
    linear-gradient(45deg, rgba(255, 255, 255, 0.06) 25%, transparent 25%, transparent 75%, rgba(255, 255, 255, 0.06) 75%);
  background-position: 0 0, 10px 10px;
  background-size: 20px 20px;
}

.wm-preview-text {
  max-width: 100%;
  text-align: center;
  word-break: break-word;
}

.wm-preview-img {
  max-width: 220px;
  max-height: 120px;
  object-fit: contain;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.2);
}

.actions {
  display: flex;
  justify-content: flex-end;
}

.panel-card {
  border: 1px solid #363637;
  border-radius: 4px;
  background: #1e1e1e;
  margin-bottom: 12px;

  :deep(.el-card__header) {
    padding: 10px 12px;
    border-bottom: 1px solid #363637;
    background: #252525;
  }

  :deep(.el-card__body) {
    padding: 12px;
  }
}

.panel-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 800;
  color: var(--el-text-color-primary);

  i {
    font-size: 16px;
    color: var(--el-color-primary);
  }
}

.panel-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.ct-block {
  width: 100%;
}

.ct-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.ct-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.ct-mode {
  flex: none;
}

.ct-actions {
  flex: none;
}

.ct-meta {
  font-size: 12px;
  color: #a6a9ad;
  white-space: nowrap;
}

.ct-tags {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px 10px;
  align-items: center;
}

.ct-all {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
  color: #a6a9ad;
  font-size: 12px;
}

.ct-tags :deep(.el-check-tag) {
  border-radius: 4px;
  border: 1px solid #363637;
  background: #252525;
  color: #a6a9ad;
  padding: 6px 10px;
  height: 30px;
  line-height: 18px;
  display: inline-flex;
  align-items: center;
  transition: background 0.12s ease, border-color 0.12s ease, color 0.12s ease;
}

.ct-tags :deep(.el-check-tag.is-checked) {
  border-color: var(--el-color-primary);
  background: color-mix(in srgb, var(--el-color-primary) 18%, #252525);
  color: #ffffff;
}

.ct-tags :deep(.el-check-tag:hover) {
  border-color: color-mix(in srgb, #363637 60%, #ffffff);
}

.ct-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.ctrl {
  width: 100%;
}

@media (min-width: 768px) {
  .ctrl-sm {
    width: min(100%, 220px);
  }
  .ctrl-num {
    width: min(100%, 180px);
  }
  .ctrl-md {
    width: min(100%, 320px);
  }
}

.sub-split {
  margin: 10px 0 10px;
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.sub-split::before,
.sub-split::after {
  content: '';
  height: 1px;
  flex: 1;
  background: rgba(255, 255, 255, 0.06);
}

.monitor-box {
  border: 1px solid #363637;
  border-radius: 4px;
  background: #252525;
  padding: 12px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-bottom: 12px;
}

@media (max-width: 768px) {
  .monitor-box {
    grid-template-columns: 1fr;
  }
}

.monitor-box :deep(.el-form-item) {
  margin-bottom: 0;
}

.monitor-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;

  i {
    font-size: 14px;
    color: var(--el-color-primary);
  }
}

.monitor-tip {
  font-size: 14px;
  color: rgba(191, 203, 217, 0.7);
  cursor: pointer;
}

.monitor-tip:hover {
  color: rgba(255, 255, 255, 0.92);
}

.poll-mode {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}

.poll-interval {
  display: flex;
  align-items: center;
  margin-left: 15px;
}

.poll-input {
  width: 150px;
}

.poll-unit {
  margin-left: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.schedule-table {
  width: 100%;
}

.schedule-actions {
  margin-top: 10px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.schedule-actions .hint {
  margin-top: 0;
}

.switch-wrap {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  flex-wrap: wrap;
  gap: 12px 20px;
}

.switch-tooltip {
  display: inline-flex;
}

.switch-wrap :deep(.el-switch__label) {
  color: rgba(191, 203, 217, 0.9);
}

.switch-wrap :deep(.el-switch__label.is-active) {
  color: rgba(255, 255, 255, 0.92);
}

/* Fix: select text/placeholder in dark mode */
:global(html.dark) .strategy-form :deep(.el-select__wrapper) {
  background: rgba(255, 255, 255, 0.02);
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08) inset;
}

:global(html.dark) .strategy-form :deep(.el-select__selected-item) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .strategy-form :deep(.el-select__placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .strategy-form :deep(.el-select__input) {
  color: rgba(255, 255, 255, 0.92);
}

:global(html.dark) .strategy-form :deep(.el-select__input::placeholder) {
  color: rgba(191, 203, 217, 0.6);
}

:global(html.dark) .strategy-form :deep(.el-select__caret) {
  color: rgba(191, 203, 217, 0.75);
}
</style>
