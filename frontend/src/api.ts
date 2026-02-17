export type ApiResponse<T> = {
  code: number
  msg: string
  data: T
}

export type Task = {
  ID: number

  user_id?: number
  source_url: string
  target_url: string
  session_key?: string
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

  delay_min_ms?: number
  delay_max_ms?: number

  daily_limit?: number
  today_count?: number
  today_date?: string
  run_window?: string

  status: number
  history_cursor?: number
  history_order?: number
}

export type Strategy = {
  ID: number

  user_id?: number
  name: string
  remark?: string

  clone_mode: number
  content_types: string[]

  scope_type: number
  scope_value: string
  history_order?: number

  keep_reply?: boolean
  realtime?: boolean
  clone_comment?: boolean
  gpu_accel?: boolean
  change_md5?: boolean

  delay_min_ms?: number
  delay_max_ms?: number

  daily_limit?: number
  run_window?: string
}

export type ReplaceRule = {
  from: string
  to: string
}

export type KeywordProfile = {
  ID: number

  user_id?: number
  name: string

  block_words: string[]
  allow_words: string[]
  replace_rules: ReplaceRule[]
  use_regex: boolean
}

export type TaskProgress = {
  task_id: number
  status: string
  speed: string
  progress_pct: number
  success_cnt: number
  fail_cnt: number
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

const tokenStorageKey = 'tgvive_jwt_token'

function getStoredToken(): string {
  try {
    if (typeof localStorage === 'undefined') return ''
    return (localStorage.getItem(tokenStorageKey) || '').trim()
  } catch {
    return ''
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers ? (init.headers as Record<string, string>) : {}),
  }

  const cleanToken = getStoredToken().trim()
  if (cleanToken) headers.Authorization = `Bearer ${cleanToken}`

  const res = await fetch(path, {
    ...init,
    headers,
  })

  const json = (await res.json()) as ApiResponse<T>
  if (json.code !== 0) {
    throw new Error(json.msg || `API error: ${json.code}`)
  }
  return json.data
}

export function getTasks(): Promise<Task[]> {
  return apiFetch<Task[]>('/api/v1/tasks', { method: 'GET' })
}

export function createTask(payload: Partial<Task>): Promise<Task> {
  return apiFetch<Task>('/api/v1/tasks', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getStrategies(): Promise<Strategy[]> {
  return apiFetch<Strategy[]>('/api/v1/strategies', { method: 'GET' })
}

export function createStrategy(payload: Partial<Strategy>): Promise<Strategy> {
  return apiFetch<Strategy>('/api/v1/strategies', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateStrategy(id: number, payload: Partial<Strategy>): Promise<Strategy> {
  return apiFetch<Strategy>(`/api/v1/strategies/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function deleteStrategy(id: number): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/api/v1/strategies/${id}`, { method: 'DELETE' })
}

export function getKeywordProfiles(): Promise<KeywordProfile[]> {
  return apiFetch<KeywordProfile[]>('/api/v1/keyword-profiles', { method: 'GET' })
}

export function createKeywordProfile(payload: Partial<KeywordProfile>): Promise<KeywordProfile> {
  return apiFetch<KeywordProfile>('/api/v1/keyword-profiles', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateKeywordProfile(id: number, payload: Partial<KeywordProfile>): Promise<KeywordProfile> {
  return apiFetch<KeywordProfile>(`/api/v1/keyword-profiles/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function deleteKeywordProfile(id: number): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/api/v1/keyword-profiles/${id}`, { method: 'DELETE' })
}

export function taskAction(id: number, action: 'start' | 'pause' | 'stop'): Promise<{ status: number; msg: string }> {
  return apiFetch<{ status: number; msg: string }>('/api/v1/tasks/action', {
    method: 'POST',
    body: JSON.stringify({ id, action }),
  })
}

export function getTaskProgress(id: number): Promise<TaskProgress> {
  return apiFetch<TaskProgress>(`/api/v1/tasks/${id}/progress`, { method: 'GET' })
}

export function listTGAccounts(): Promise<TGAccount[]> {
  return apiFetch<TGAccount[]>('/api/v1/tg/accounts', { method: 'GET' })
}

export function deleteTGAccount(key: string): Promise<{ ok: boolean }> {
  const safe = encodeURIComponent(key || '')
  return apiFetch<{ ok: boolean }>(`/api/v1/tg/accounts/${safe}`, { method: 'DELETE' })
}

export function startAccountQR(): Promise<{ session_id: string }> {
  return apiFetch<{ session_id: string }>('/api/v1/tg/accounts/qr', {
    method: 'POST',
    body: '{}',
  })
}

export function getAccountQRStatus(sessionId: string): Promise<QRState> {
  const q = new URLSearchParams({ session_id: sessionId })
  return apiFetch<QRState>(`/api/v1/tg/accounts/qr/status?${q.toString()}`, { method: 'GET' })
}

export function startCodeLogin(phone: string): Promise<{ session_id: string }> {
  return apiFetch<{ session_id: string }>('/api/v1/tg/accounts/code', {
    method: 'POST',
    body: JSON.stringify({ phone }),
  })
}

export function getCodeLoginStatus(sessionId: string): Promise<CodeAuthState> {
  const q = new URLSearchParams({ session_id: sessionId })
  return apiFetch<CodeAuthState>(`/api/v1/tg/accounts/code/status?${q.toString()}`, { method: 'GET' })
}

export function submitCode(sessionId: string, code: string): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>('/api/v1/tg/accounts/code/submit', {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId, code }),
  })
}

export function submitPassword(sessionId: string, password: string): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>('/api/v1/tg/accounts/code/password', {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId, password }),
  })
}

export function submitQRPassword(sessionId: string, password: string): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>('/api/v1/tg/accounts/qr/password', {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId, password }),
  })
}

export function getDevToken(username = 'admin'): Promise<{ token: string; user: { id: number; username: string } }> {
  const q = new URLSearchParams({ username })
  return apiFetch<{ token: string; user: { id: number; username: string } }>(`/api/v1/auth/dev/token?${q.toString()}`, {
    method: 'GET',
  })
}
