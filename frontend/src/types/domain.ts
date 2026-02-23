export type Task = {
  ID: number

  user_id?: number
  source_url: string
  target_url: string
  remark?: string
  session_key?: string
  publish_type?: string
  publish_session_key?: string
  publish_bot_id?: string
  strategy_id?: number
  keyword_profile_id?: number
  source_channel_id?: number

  clone_mode: number
  content_types: string[]

  scope_type: number
  scope_value: string

  keep_reply?: boolean
  realtime?: boolean
  clone_comment?: boolean
  gpu_accel?: boolean
  change_md5?: boolean
  random_filename?: boolean
  enable_media_edit?: boolean

  delay_min_ms?: number
  delay_max_ms?: number

  daily_limit?: number
  today_count?: number
  today_date?: string
  run_window?: string

  status: number
  last_error?: string
  next_run_time?: string | null

  current_slot_count?: number
  current_slot_key?: string

  history_cursor?: number
  history_order?: number
}

export type CommentRule = {
  enable: boolean
  filter_mode: 'owner_only' | 'all' | string
  trusted_user_ids: number[]
  allow_anonymous: boolean
  allowed_types: string[]
  block_keywords: string[]
}

export type WatermarkRule = {
  enable: boolean
  type: 'text' | 'image' | string
  text: string
  text_style: 'plain' | 'stroke' | 'shadow' | 'stroke_shadow' | string
  text_color: string
  stroke_color: string
  shadow_color: string
  font_path: string
  image_path: string
  position: 'bottom_right' | 'bottom_left' | 'top_right' | 'top_left' | 'center' | 'custom' | string
  custom_x: number
  custom_y: number
  margin: number
  scale_ratio: number
  opacity: number
}

export type Strategy = {
  ID: number

  user_id?: number
  name: string
  remark?: string

  clone_mode: number
  allowed_types?: string[]
  content_types: string[]
  block_file_exts?: string[]
  allow_file_exts?: string[]

  scope_type: number
  scope_value: string
  history_order?: number
  poll_interval?: number

  enable_realtime?: boolean
  schedule_rules?: Array<{ start: string; end: string; limit: number }>

  comment_rule?: CommentRule
  watermark_rule?: WatermarkRule

  keep_reply?: boolean
  realtime?: boolean
  clone_comment?: boolean
  gpu_accel?: boolean
  change_md5?: boolean
  random_filename?: boolean
  enable_media_edit?: boolean

  delay_min_ms?: number
  delay_max_ms?: number

  daily_limit?: number
  run_window?: string
}

export type ReplaceRule = {
  from: string
  to: string
}

export type KeywordRule = {
  content: string
  is_regex: boolean
}

export type KeywordProfile = {
  ID: number

  user_id?: number
  name: string
  remark?: string

  block_words: KeywordRule[]
  allow_words: KeywordRule[]
  replace_rules: ReplaceRule[]
}

export type TaskProgress = {
  task_id: number
  status: string
  speed: string
  progress_pct: number
  processed_cnt: number
  success_cnt: number
  fail_cnt: number
  root_cnt?: number
  reply_cnt?: number
  total_msg: number
  logs: string[]
}

export type TGAccount = {
  key: string
  updated_at: number
  size: number

  user_id?: number
  username?: string
  name?: string
  phone?: string
  avatar?: string
  meta_updated_at?: number
}

export type TGDialogItem = {
  kind: 'channel' | 'supergroup' | 'group' | string
  title: string
  username?: string

  channel_id?: number
  chat_id?: number

  bot_chat_id: string
  task_value: string
}

export type TGBot = {
  id: string
  name: string
  api_base: string
  disabled?: boolean

  token_set: boolean
  token_mask?: string

  created_at?: number
  updated_at?: number
}

export type TGBotTestResult = {
  ok: boolean
  latency_ms?: number
  http_status?: number
  error?: string

  error_code?: number
  description?: string

  bot_user_id?: number
  username?: string
  name?: string
}

export type QRState = {
  session_id: string
  url?: string
  image?: string
  status: string
  error?: string
  expires_at?: number
  updated_at?: number
}

export type CodeAuthState = {
  session_id: string
  phone?: string
  status: string
  error?: string
  updated_at?: number
}

export type ProxyConfig = {
  enabled: boolean
  type: 'http' | 'socks5' | string
  host: string
  port: number
  username: string
  password_set?: boolean
  updated_at?: number
}

export type ProxyTestResult = {
  target: string
  ping_ms: number
}
