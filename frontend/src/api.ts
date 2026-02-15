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
  source_channel_id?: number

  session_key?: string

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

async function apiFetch<T>(path: string, token: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers ? (init.headers as Record<string, string>) : {}),
  }

  const cleanToken = token.trim()
  if (cleanToken) {
    headers.Authorization = `Bearer ${cleanToken}`
  }

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

export function getTasks(token: string): Promise<Task[]> {
  return apiFetch<Task[]>('/api/v1/tasks', token, { method: 'GET' })
}

export function createTask(token: string, payload: Partial<Task>): Promise<Task> {
  return apiFetch<Task>('/api/v1/tasks', token, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function taskAction(token: string, id: number, action: 'start' | 'pause' | 'stop'): Promise<{ status: number; msg: string }> {
  return apiFetch<{ status: number; msg: string }>('/api/v1/tasks/action', token, {
    method: 'POST',
    body: JSON.stringify({ id, action }),
  })
}

export function getTaskProgress(token: string, id: number): Promise<TaskProgress> {
  return apiFetch<TaskProgress>(`/api/v1/tasks/${id}/progress`, token, { method: 'GET' })
}

