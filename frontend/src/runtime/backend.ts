const backendOriginStorageKey = 'tgvive_backend_origin'

export function getBackendOrigin(): string {
  try {
    if (typeof localStorage === 'undefined') return ''
    return String(localStorage.getItem(backendOriginStorageKey) || '').trim()
  } catch {
    return ''
  }
}

export function normalizeBackendOrigin(input: string): string {
  let s = String(input || '').trim()
  if (!s) return ''

  if (!/^https?:\/\//i.test(s)) {
    s = 'http://' + s
  }

  try {
    const u = new URL(s)
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return ''
    if (!u.host) return ''
    return `${u.protocol}//${u.host}`
  } catch {
    return ''
  }
}

export function setBackendOrigin(input: string) {
  const norm = normalizeBackendOrigin(input)
  try {
    if (typeof localStorage === 'undefined') return
    if (!norm) localStorage.removeItem(backendOriginStorageKey)
    else localStorage.setItem(backendOriginStorageKey, norm)
  } catch {
    // ignore
  }
}

export function resolveAPIURL(path: string): string {
  const p = String(path || '')
  if (/^https?:\/\//i.test(p)) return p
  const origin = getBackendOrigin()
  if (!origin) return p
  if (!p) return origin
  return p.startsWith('/') ? origin + p : origin + '/' + p
}

export function resolveWSURL(path: string): string {
  const p = String(path || '')
  if (/^wss?:\/\//i.test(p)) return p

  const stored = getBackendOrigin()
  if (stored) {
    try {
      const u = new URL(stored)
      const proto = u.protocol === 'https:' ? 'wss:' : 'ws:'
      const base = `${proto}//${u.host}`
      return p.startsWith('/') ? base + p : base + '/' + p
    } catch {
      // ignore
    }
  }

  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const base = `${proto}://${window.location.host}`
  return p.startsWith('/') ? base + p : base + '/' + p
}

export function suggestLocalBackendOrigin(port = 8081): string {
  const proto = window.location.protocol === 'https:' ? 'https:' : 'http:'
  const host = String(window.location.hostname || '').trim()
  if (!host) return ''
  return `${proto}//${host}:${Math.max(1, Math.floor(port))}`
}

export async function probeBackendOrigin(origin: string, timeoutMs = 1200): Promise<boolean> {
  const base = normalizeBackendOrigin(origin)
  const url = base ? base + '/api/v1/ping' : '/api/v1/ping'

  const ac = new AbortController()
  const t = window.setTimeout(() => ac.abort(), Math.max(200, Math.floor(timeoutMs)))
  try {
    const res = await fetch(url, { method: 'GET', cache: 'no-store', signal: ac.signal, credentials: 'include' })
    const ct = String(res.headers.get('content-type') || '').toLowerCase()
    if (!ct.includes('application/json')) return false
    const json = (await res.json()) as any
    return json && typeof json === 'object' && Number(json.code) === 0
  } catch {
    return false
  } finally {
    window.clearTimeout(t)
  }
}

// ensureBackendOrigin tries to keep API access working when frontend and backend are on different ports,
// e.g. using `python -m http.server` to serve frontend dist.
export async function ensureBackendOrigin(): Promise<void> {
  const stored = getBackendOrigin()
  if (stored) {
    if (await probeBackendOrigin(stored)) return
    if (await probeBackendOrigin('')) {
      setBackendOrigin('')
      return
    }
    return
  }

  if (await probeBackendOrigin('')) return

  const cand = suggestLocalBackendOrigin(8081)
  if (cand && (await probeBackendOrigin(cand))) {
    setBackendOrigin(cand)
  }
}
