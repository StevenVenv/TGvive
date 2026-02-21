import { onBeforeUnmount, onMounted, ref } from 'vue'
import { apiFetch } from '../api'

export type BackendPingStatus = 'checking' | 'online' | 'offline'

export function useBackendPing(options?: { intervalMs?: number; timeoutMs?: number }) {
  const status = ref<BackendPingStatus>('checking')
  const latencyMs = ref<number | null>(null)
  const checkedAt = ref<number | null>(null)

  const intervalMs = Math.max(2000, Math.floor(options?.intervalMs ?? 10_000))
  const timeoutMs = Math.max(500, Math.floor(options?.timeoutMs ?? 2000))

  let timer: number | undefined
  let inFlight = false

  async function pingOnce() {
    if (inFlight) return
    inFlight = true

    status.value = checkedAt.value ? 'checking' : status.value

    const ac = new AbortController()
    const start = performance.now()
    const timeout = window.setTimeout(() => ac.abort(), timeoutMs)

    try {
      await apiFetch<unknown>('/api/v1/ping', { method: 'GET', signal: ac.signal })
      latencyMs.value = Math.max(0, Math.round(performance.now() - start))
      status.value = 'online'
    } catch {
      latencyMs.value = null
      status.value = 'offline'
    } finally {
      window.clearTimeout(timeout)
      checkedAt.value = Date.now()
      inFlight = false
    }
  }

  function startPolling() {
    if (timer) window.clearInterval(timer)
    timer = window.setInterval(() => {
      void pingOnce()
    }, intervalMs)
  }

  function stopPolling() {
    if (timer) window.clearInterval(timer)
    timer = undefined
  }

  onMounted(() => {
    void pingOnce()
    startPolling()
  })

  onBeforeUnmount(() => {
    stopPolling()
  })

  return { status, latencyMs, checkedAt, pingOnce, startPolling, stopPolling }
}

