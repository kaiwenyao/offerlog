// Thin fetch wrapper: attaches session cookie (same-origin), CSRF header on
// writes, and unwraps the canonical {code,message} error envelope.
import { effectiveZone } from './tz'
import type { Me } from './types'

const CSRF_KEY = 'offerlog.csrf'

let csrfToken: string | null = localStorage.getItem(CSRF_KEY)

export function setCsrf(token: string | null) {
  csrfToken = token
  if (token) localStorage.setItem(CSRF_KEY, token)
  else localStorage.removeItem(CSRF_KEY)
}

export class ApiError extends Error {
  code: string
  status: number
  fieldErrors?: Record<string, string>
  constructor(code: string, message: string, status: number) {
    super(message)
    this.code = code
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown, isForm = false): Promise<T> {
  const headers: Record<string, string> = {}
  if (!isForm && body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: isForm ? (body as FormData) : body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 401 && !path.includes('/auth/')) {
    // session expired — kick back to login
    window.dispatchEvent(new CustomEvent('offerlog:unauthorized'))
  }
  const text = await res.text()
  if (!res.ok) {
    let code = 'error'
    let message = `请求失败 (${res.status})`
    try {
      const j = JSON.parse(text)
      code = j.code ?? code
      message = j.message ?? message
    } catch {
      /* non-json */
    }
    throw new ApiError(code, message, res.status)
  }
  return text ? (normalizeLists(JSON.parse(text)) as T) : ({} as T)
}

/**
 * Go marshals a nil slice as JSON `null`, so a list endpoint with no rows
 * answers `{"items": null}` rather than `{"items": []}`. Coerce the collection
 * fields of the response envelope to arrays once, here at the boundary, so no
 * caller has to defend against it.
 */
const LIST_FIELDS = ['items', 'nodes', 'links', 'errors', 'duplicate_candidates', 'member_ids']

function normalizeLists(body: unknown): unknown {
  if (body === null || typeof body !== 'object' || Array.isArray(body)) return body
  const src = body as Record<string, unknown>
  let patch: Record<string, unknown> | null = null
  for (const field of LIST_FIELDS) {
    if (field in src && src[field] === null) {
      patch ??= {}
      patch[field] = []
    }
  }
  return patch ? { ...src, ...patch } : src
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown, isForm = false) => request<T>('POST', p, b, isForm),
  patch: <T>(p: string, b: unknown) => request<T>('PATCH', p, b),
  put: <T>(p: string, b: unknown) => request<T>('PUT', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
}

export async function fetchMe(): Promise<Me | null> {
  try {
    const me = await api.get<Me>('/api/v1/auth/me')
    return me
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

export async function login(email: string, password: string): Promise<Me> {
  const res = await fetch('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ email, password }),
  })
  const text = await res.text()
  if (!res.ok) {
    let msg = '登录失败'
    try {
      const j = JSON.parse(text)
      msg = j.message ?? msg
    } catch { /* noop */ }
    throw new ApiError('login_failed', msg, res.status)
  }
  const me = JSON.parse(text) as Me
  setCsrf(me.csrf_token ?? null)
  return me
}

export async function fetchAuthConfig(): Promise<{ registration_open: boolean }> {
  return api.get<{ registration_open: boolean }>('/api/v1/auth/config')
}

export async function register(email: string, password: string, displayName?: string): Promise<Me> {
  const res = await fetch('/api/v1/auth/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ email, password, display_name: displayName }),
  })
  const text = await res.text()
  if (!res.ok) {
    let code = 'register_failed'
    let msg = '注册失败'
    try {
      const j = JSON.parse(text)
      code = j.code ?? code
      msg = j.message ?? msg
    } catch { /* noop */ }
    throw new ApiError(code, msg, res.status)
  }
  const me = JSON.parse(text) as Me
  setCsrf(me.csrf_token ?? null)
  return me
}

export async function logout(): Promise<void> {
  try {
    await api.post('/api/v1/auth/logout')
  } finally {
    setCsrf(null)
  }
}

export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

/** YYYY-MM-DD (a calendar day) or a full ISO instant? */
const DAY_ONLY = /^\d{4}-\d{2}-\d{2}$/

/**
 * Local calendar day (YYYY-MM-DD) for a date-only string *or* an instant.
 *
 * A date-only string is a calendar day and must never be routed through
 * `new Date(...)` (UTC-midnight parses shift the day in west-of-UTC
 * browsers). An instant is converted to the user's local calendar day.
 */
export function toDayString(s: string | null | undefined, zone?: string): string | null {
  if (!s) return null
  const trimmed = s.trim()
  if (DAY_ONLY.test(trimmed)) return trimmed
  const d = new Date(trimmed)
  if (isNaN(d.getTime())) return null
  if (zone) {
    try {
      return new Intl.DateTimeFormat('en-CA', {
        timeZone: zone,
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
      }).format(d)
    } catch {
      /* fall through to local */
    }
  }
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

/**
 * UTC instant of local midnight (00:00) for a calendar-day string in the
 * given zone. Example: 2026-09-10 in America/Los_Angeles → 2026-09-10T07:00Z;
 * in Europe/Dublin (summer, UTC+1) → 2026-09-09T23:00Z.
 *
 * Implementation: at UTC noon of the target day every real-world zone shows
 * the same calendar day, so the offset read there is the day's offset; local
 * midnight = midnight UTC − offset. Returns null for malformed input; without
 * a zone it falls back to the browser's local midnight.
 */
export function dayToInstant(dayStr: string | null | undefined, zone?: string): number | null {
  if (!dayStr) return null
  const s = dayStr.trim()
  if (!DAY_ONLY.test(s)) return null
  const [y, m, d] = s.split('-').map(Number)
  if (zone) {
    try {
      const fmt = new Intl.DateTimeFormat('en-US', {
        timeZone: zone,
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hourCycle: 'h23',
      })
      const read = (utcMs: number) => {
        const parts = fmt.formatToParts(utcMs)
        const map: Record<string, string> = {}
        for (const p of parts) map[p.type] = p.value
        const local = Date.UTC(
          Number(map.year),
          Number(map.month) - 1,
          Number(map.day),
          Number(map.hour),
          Number(map.minute),
          Number(map.second),
        )
        return local
      }
      const noonUtc = Date.UTC(y, m - 1, d, 12, 0, 0)
      const localAtNoon = read(noonUtc)
      const offsetMs = localAtNoon - noonUtc
      return Date.UTC(y, m - 1, d, 0, 0, 0) - offsetMs
    } catch {
      /* fall through to browser-local */
    }
  }
  return new Date(y, m - 1, d).getTime()
}

// Matches the value emitted by <input type="datetime-local">: a naive
// YYYY-MM-DDTHH:mm wall-clock string with NO timezone suffix.
const DATETIME_LOCAL = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/

/**
 * Interpret a naive datetime-local wall-clock string (YYYY-MM-DDTHH:mm, the
 * value of <input type="datetime-local">) as local time in the given zone and
 * return the UTC instant (ms). Returns null for malformed input.
 *
 * Why not `new Date(s).toISOString()`: per the ES spec a date-time string
 * without a zone suffix is parsed as BROWSER-local time. When the browser zone
 * differs from the user's configured zone (a Dublin browser + Shanghai user,
 * say), typing 14:30 would be stored as 14:30 in Dublin = 21:30 in Shanghai —
 * an instant that then pollutes the "明天有面试" reminder day and calendar
 * buckets. This interprets the input as wall-clock in the user's zone instead
 * (same contract as dayToInstant for date-only inputs). Without a zone it
 * falls back to the browser's own interpretation, matching legacy behavior.
 */
export function localDateTimeToInstant(s: string | null | undefined, zone?: string): number | null {
  if (!s) return null
  const v = s.trim()
  if (!DATETIME_LOCAL.test(v)) return null
  const [datePart, timePart] = v.split('T')
  const [y, mo, d] = datePart.split('-').map(Number)
  const [h, mi] = timePart.split(':').map(Number)
  const targetWall = Date.UTC(y, mo - 1, d, h, mi, 0) // naive wall clock as a UTC frame
  if (zone) {
    try {
      const fmt = new Intl.DateTimeFormat('en-US', {
        timeZone: zone,
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hourCycle: 'h23',
      })
      const wallAt = (utcMs: number) => {
        const parts = fmt.formatToParts(utcMs)
        const map: Record<string, string> = {}
        for (const p of parts) map[p.type] = p.value
        return Date.UTC(
          Number(map.year),
          Number(map.month) - 1,
          Number(map.day),
          Number(map.hour),
          Number(map.minute),
          Number(map.second),
        )
      }
      // Fixpoint: guess the instant (naive-as-UTC), read the zone wall clock
      // there, then shift by the wall-clock delta. Two passes converge even
      // across a DST transition (the offset only changes by ≤1h).
      let utc = targetWall
      for (let i = 0; i < 2; i++) {
        utc += targetWall - wallAt(utc)
      }
      return utc
    } catch {
      /* fall through to browser-local */
    }
  }
  return new Date(y, mo - 1, d, h, mi).getTime()
}

/**
 * Render a date-only string (or instant) as a local calendar day without
 * letting a UTC-midnight parse shift it. Date-only values pass through
 * untouched; instants are formatted in the given zone (default: browser).
 */
export function fmtDay(s: string | null | undefined, zone?: string): string {
  if (!s) return '—'
  const trimmed = s.trim()
  if (DAY_ONLY.test(trimmed)) {
    const [y, m, d] = trimmed.split('-').map(Number)
    return `${y}/${String(m).padStart(2, '0')}/${String(d).padStart(2, '0')}`
  }
  const ds = toDayString(s, zone)
  if (!ds) return '—'
  const [y, m, d] = ds.split('-').map(Number)
  return `${y}/${String(m).padStart(2, '0')}/${String(d).padStart(2, '0')}`
}

/**
 * Render an instant as a local calendar day in the given zone (default: the
 * user's configured zone via effectiveZone, falling back to the browser zone).
 * Passing an explicit zone keeps this pure/testable; instants must never be
 * rendered through the browser's local getters when the user zone differs.
 */
export function fmtDate(s: string | null | undefined, zone?: string): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return '—'
  return d.toLocaleDateString('zh-CN', {
    timeZone: zone ?? effectiveZone(),
    month: '2-digit',
    day: '2-digit',
    year: 'numeric',
  })
}

/**
 * Render an instant with date + time in the given zone (default: the user's
 * configured zone via effectiveZone, falling back to the browser zone). The
 * UI buckets events by the user's zone, so the displayed time string must be
 * rendered in that same zone or a Dublin browser + Shanghai user would see
 * the wrong local time next to the correct calendar day.
 */
export function fmtDateTime(s: string | null | undefined, zone?: string): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return '—'
  return d.toLocaleString('zh-CN', {
    timeZone: zone ?? effectiveZone(),
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function daysBetween(fromIso: string | null | undefined, to: Date = new Date()): number | null {
  if (!fromIso) return null
  const from = new Date(fromIso)
  return Math.max(0, Math.floor((to.getTime() - from.getTime()) / 86400000))
}
