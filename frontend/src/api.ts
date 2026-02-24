import type {
  CodeAuthState,
  KeywordProfile,
  ProxyConfig,
  ProxyTestResult,
  QRState,
  Strategy,
  Task,
  TaskProgress,
  TGAccount,
  TGDialogItem,
  TGBot,
  TGBotTestResult,
} from './types/domain'

import { resolveAPIURL } from './runtime/backend'

export type ApiResponse<T> = {
  code: number
  msg: string
  data: T
}

export type {
  CodeAuthState,
  CommentRule,
  KeywordProfile,
  KeywordRule,
  ProxyConfig,
  ProxyTestResult,
  QRState,
  ReplaceRule,
  Strategy,
  Task,
  TaskProgress,
  TGAccount,
  TGDialogItem,
  TGBot,
  TGBotTestResult,
  VideoWatermarkRule,
  WatermarkRule,
} from './types/domain'

async function apiUpload<T>(path: string, form: FormData): Promise<T> {
  const url = resolveAPIURL(path)
  const res = await fetch(url, {
    method: 'POST',
    body: form,
    credentials: 'include',
  })

  const json = (await res.json()) as ApiResponse<T>
  if (json.code !== 0) {
    throw new Error(json.msg || `API error: ${json.code}`)
  }
  return json.data
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const url = resolveAPIURL(path)
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers ? (init.headers as Record<string, string>) : {}),
  }
  const credentials = init?.credentials ?? 'include'

  const res = await fetch(url, {
    ...init,
    headers,
    credentials,
  })

  const json = (await res.json()) as ApiResponse<T>
  if (json.code !== 0) {
    throw new Error(json.msg || `API error: ${json.code}`)
  }
  return json.data
}

export async function apiFetchBlob(path: string, init?: RequestInit): Promise<Blob> {
  const url = resolveAPIURL(path)
  const headers: Record<string, string> = {
    ...(init?.headers ? (init.headers as Record<string, string>) : {}),
  }
  const credentials = init?.credentials ?? 'include'

  const res = await fetch(url, {
    ...init,
    headers,
    credentials,
  })

  const ct = String(res.headers.get('content-type') || '').toLowerCase()
  if (ct.includes('application/json')) {
    const json = (await res.json()) as ApiResponse<any>
    throw new Error(json?.msg || `API error: ${json?.code ?? 'unknown'}`)
  }

  if (!res.ok) {
    throw new Error(`HTTP ${res.status} ${res.statusText}`.trim())
  }

  return res.blob()
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

export type WatermarkUploadResult = { path: string; name?: string; url?: string }

export function uploadWatermarkPNG(file: File): Promise<WatermarkUploadResult> {
  const form = new FormData()
  form.append('file', file)
  return apiUpload<WatermarkUploadResult>('/api/v1/watermarks/upload', form)
}

export function uploadWatermarkFont(file: File): Promise<WatermarkUploadResult> {
  const form = new FormData()
  form.append('file', file)
  return apiUpload<WatermarkUploadResult>('/api/v1/watermarks/fonts/upload', form)
}

export type SystemCapabilities = {
  ffmpeg: { enabled: boolean; path: string; available: boolean; reason?: string }
}

export function getSystemCapabilities(): Promise<SystemCapabilities> {
  return apiFetch<SystemCapabilities>('/api/v1/system/capabilities', { method: 'GET' })
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

export function taskAction(
  id: number,
  action: 'start' | 'pause' | 'stop' | 'restart',
): Promise<{ status: number; msg: string }> {
  return apiFetch<{ status: number; msg: string }>('/api/v1/tasks/action', {
    method: 'POST',
    body: JSON.stringify({ id, action }),
  })
}

export function getTaskProgress(id: number): Promise<TaskProgress> {
  return apiFetch<TaskProgress>(`/api/v1/tasks/${id}/progress`, { method: 'GET' })
}

export function getTaskProgressBatch(ids: number[]): Promise<Record<number, TaskProgress>> {
  const uniq = Array.from(new Set((ids || []).map((v) => Math.floor(Number(v || 0))).filter((v) => Number.isFinite(v) && v > 0))).slice(
    0,
    200,
  )
  if (uniq.length === 0) return Promise.resolve({})
  const q = new URLSearchParams({ ids: uniq.join(',') })
  return apiFetch<Record<number, TaskProgress>>(`/api/v1/tasks/progress?${q.toString()}`, { method: 'GET' })
}

export function listTGAccounts(): Promise<TGAccount[]> {
  return apiFetch<TGAccount[]>('/api/v1/tg/accounts', { method: 'GET' })
}

export function deleteTGAccount(key: string): Promise<{ ok: boolean }> {
  const safe = encodeURIComponent(key || '')
  return apiFetch<{ ok: boolean }>(`/api/v1/tg/accounts/${safe}`, { method: 'DELETE' })
}

export function listTGAccountDialogs(key: string, limit = 500): Promise<TGDialogItem[]> {
  const safe = encodeURIComponent(key || '')
  const q = new URLSearchParams({ limit: String(Math.max(1, Math.floor(Number(limit || 0) || 500))) })
  return apiFetch<TGDialogItem[]>(`/api/v1/tg/accounts/${safe}/dialogs?${q.toString()}`, { method: 'GET' })
}

export function resolveTGAccountPeer(key: string, peer: string): Promise<TGDialogItem> {
  const safe = encodeURIComponent(key || '')
  const q = new URLSearchParams({ peer: String(peer || '') })
  return apiFetch<TGDialogItem>(`/api/v1/tg/accounts/${safe}/resolve?${q.toString()}`, { method: 'GET' })
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

export function listTGBots(): Promise<TGBot[]> {
  return apiFetch<TGBot[]>('/api/v1/tg/bots', { method: 'GET' })
}

export function addTGBot(payload: { name: string; token: string; api_base?: string }): Promise<TGBot> {
  return apiFetch<TGBot>('/api/v1/tg/bots', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateTGBot(
  id: string,
  payload: { name?: string; token?: string; api_base?: string; disabled?: boolean },
): Promise<TGBot> {
  const safe = encodeURIComponent(id || '')
  return apiFetch<TGBot>(`/api/v1/tg/bots/${safe}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function deleteTGBot(id: string): Promise<{ ok: boolean }> {
  const safe = encodeURIComponent(id || '')
  return apiFetch<{ ok: boolean }>(`/api/v1/tg/bots/${safe}`, { method: 'DELETE' })
}

export function testTGBot(id: string, payload?: { timeout_ms?: number }): Promise<TGBotTestResult> {
  const safe = encodeURIComponent(id || '')
  return apiFetch<TGBotTestResult>(`/api/v1/tg/bots/${safe}/test`, {
    method: 'POST',
    body: JSON.stringify(payload || {}),
  })
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

export function getProxyConfig(): Promise<ProxyConfig> {
  return apiFetch<ProxyConfig>('/api/v1/settings/proxy', { method: 'GET' })
}

export function updateProxyConfig(payload: {
  enabled: boolean
  type: string
  host: string
  port: number
  username: string
  password?: string
}): Promise<ProxyConfig> {
  return apiFetch<ProxyConfig>('/api/v1/settings/proxy', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function testProxyConfig(payload: {
  enabled: boolean
  type: string
  host: string
  port: number
  username: string
  password: string
  target?: string
  timeout_ms?: number
}): Promise<ProxyTestResult> {
  return apiFetch<ProxyTestResult>('/api/v1/settings/proxy/test', {
    method: 'POST',
    body: JSON.stringify(payload),
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
