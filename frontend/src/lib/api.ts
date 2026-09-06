// Thin fetch wrapper: attaches session cookie (same-origin), CSRF header on
// writes, and unwraps the canonical {code,message} error envelope.
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
  return text ? (JSON.parse(text) as T) : ({} as T)
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown, isForm = false) => request<T>('POST', p, b, isForm),
  patch: <T>(p: string, b: unknown) => request<T>('PATCH', p, b),
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

export function fmtDate(s: string | null | undefined): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return '—'
  return d.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit', year: 'numeric' })
}

export function fmtDateTime(s: string | null | undefined): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return '—'
  return d.toLocaleString('zh-CN', {
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
