import { apiFetch, type Task } from '../api'

export function updateTask(id: number, payload: Partial<Task>): Promise<Task> {
  const taskID = Number(id || 0)
  return apiFetch<Task>(`/api/v1/tasks/${taskID}`, {
    method: 'PUT',
    body: JSON.stringify(payload || {}),
  })
}

export function deleteTask(id: number): Promise<{ ok: boolean }> {
  const taskID = Number(id || 0)
  return apiFetch<{ ok: boolean }>(`/api/v1/tasks/${taskID}`, { method: 'DELETE' })
}

