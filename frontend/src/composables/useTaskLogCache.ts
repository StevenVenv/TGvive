import { onBeforeUnmount, ref } from 'vue'

export type TaskLogCacheOptions = {
  storageKey?: string
  maxLines?: number
}

type CacheShape = Record<number, string[]>

export function useTaskLogCache(options?: TaskLogCacheOptions) {
  const storageKey = String(options?.storageKey || 'tgvive_task_logs_v1')
  const maxLines = Math.max(20, Math.floor(options?.maxLines ?? 100))

  const cache = ref<CacheShape>({})
  let persistTimer: number | undefined

  function load(): CacheShape {
    try {
      const raw = (localStorage.getItem(storageKey) || '').trim()
      if (!raw) return {}
      const parsed = JSON.parse(raw) as Record<string, unknown>
      if (!parsed || typeof parsed !== 'object') return {}

      const out: CacheShape = {}
      for (const [k, v] of Object.entries(parsed)) {
        const id = Math.floor(Number(k || 0))
        if (!Number.isFinite(id) || id <= 0) continue
        if (!Array.isArray(v)) continue
        const lines = (v as unknown[]).filter((x): x is string => typeof x === 'string' && x.trim() !== '')
        if (lines.length > 0) out[id] = lines.slice(-maxLines)
      }
      return out
    } catch {
      return {}
    }
  }

  function persistNow() {
    try {
      const out: Record<string, string[]> = {}
      for (const [k, v] of Object.entries(cache.value || {})) {
        const id = Math.floor(Number(k || 0))
        if (!Number.isFinite(id) || id <= 0) continue
        if (!Array.isArray(v) || v.length === 0) continue
        out[String(id)] = v.slice(-maxLines)
      }
      localStorage.setItem(storageKey, JSON.stringify(out))
    } catch {
      // ignore
    }
  }

  function schedulePersist() {
    if (persistTimer) window.clearTimeout(persistTimer)
    persistTimer = window.setTimeout(() => {
      persistTimer = undefined
      persistNow()
    }, 250)
  }

  function mergeLines(existing: string[], incoming: string[]): string[] {
    if (!Array.isArray(incoming) || incoming.length === 0) return existing
    if (!Array.isArray(existing) || existing.length === 0) return incoming.slice(-maxLines)

    const a = existing
    const b = incoming
    const maxOverlap = Math.min(a.length, b.length)
    let overlap = 0
    for (let k = maxOverlap; k > 0; k--) {
      let ok = true
      for (let i = 0; i < k; i++) {
        if (a[a.length - k + i] !== b[i]) {
          ok = false
          break
        }
      }
      if (ok) {
        overlap = k
        break
      }
    }

    let merged = overlap > 0 ? a.concat(b.slice(overlap)) : a.concat(b)
    if (merged.length > maxLines) merged = merged.slice(merged.length - maxLines)
    return merged
  }

  function ingest(taskID: number, logs: unknown) {
    const id = Math.floor(Number(taskID || 0))
    if (!Number.isFinite(id) || id <= 0) return
    if (!Array.isArray(logs) || logs.length === 0) return

    const incoming = (logs as unknown[]).filter((x): x is string => typeof x === 'string' && x.trim() !== '')
    if (incoming.length === 0) return

    const prev = cache.value[id] || []
    const merged = mergeLines(prev, incoming)
    if (merged.length === prev.length && merged[merged.length - 1] === prev[prev.length - 1]) return

    cache.value = { ...cache.value, [id]: merged }
    schedulePersist()
  }

  function clear(taskID: number) {
    const id = Math.floor(Number(taskID || 0))
    if (!Number.isFinite(id) || id <= 0) return
    if (!cache.value[id]?.length) return
    const next = { ...cache.value }
    delete next[id]
    cache.value = next
    schedulePersist()
  }

  function prune(keepIDs: number[]) {
    const keep = new Set(
      (keepIDs || [])
        .map((v) => Math.floor(Number(v || 0)))
        .filter((v) => Number.isFinite(v) && v > 0),
    )
    const next: CacheShape = {}
    for (const [k, v] of Object.entries(cache.value || {})) {
      const id = Math.floor(Number(k || 0))
      if (!Number.isFinite(id) || id <= 0) continue
      if (!keep.has(id)) continue
      if (!Array.isArray(v) || v.length === 0) continue
      next[id] = v.slice(-maxLines)
    }
    cache.value = next
    schedulePersist()
  }

  function get(taskID: number): string[] {
    const id = Math.floor(Number(taskID || 0))
    if (!Number.isFinite(id) || id <= 0) return []
    const lines = cache.value[id]
    return Array.isArray(lines) ? lines : []
  }

  function init() {
    cache.value = load()
  }

  init()

  onBeforeUnmount(() => {
    if (persistTimer) window.clearTimeout(persistTimer)
    persistTimer = undefined
    persistNow()
  })

  return { cache, ingest, clear, prune, get, persistNow }
}

