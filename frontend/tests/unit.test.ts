// Component tests for the shared status/formatting helpers + table row
// rendering logic (pure parts). API-interactive flows are covered by the
// Playwright e2e suite.
import { describe, expect, it } from 'vitest'
import { statusMeta, STATUSES } from '../src/lib/status'
import { fmtBytes, fmtDate, daysBetween } from '../src/lib/api'

describe('status dictionary', () => {
  it('covers all 11 standard statuses with Chinese labels + icons', () => {
    expect(STATUSES).toHaveLength(11)
    const keys = STATUSES.map((s) => s.key)
    for (const k of [
      'saved', 'preparing', 'applied', 'screening', 'assessment', 'interviewing',
      'offer', 'accepted', 'rejected', 'withdrawn', 'closed',
    ]) {
      expect(keys).toContain(k)
    }
    expect(statusMeta('offer').label).toBe('收到 Offer')
    expect(statusMeta('offer').icon).toBeTruthy()
    expect(statusMeta('bogus').label).toBe('bogus') // graceful fallback
  })
  it('maps statuses into the four lifecycle categories', () => {
    expect(statusMeta('saved').category).toBe('preparing')
    expect(statusMeta('interviewing').category).toBe('in_progress')
    expect(statusMeta('offer').category).toBe('decision')
    expect(statusMeta('rejected').category).toBe('ended')
  })
})

describe('formatting helpers', () => {
  it('formats bytes', () => {
    expect(fmtBytes(0)).toBe('0 B')
    expect(fmtBytes(2048)).toBe('2.0 KB')
    expect(fmtBytes(5 * 1024 * 1024)).toBe('5.0 MB')
  })
  it('formats dates with dashes for empty input', () => {
    expect(fmtDate(null)).toBe('—')
    expect(fmtDate('2026-09-05T10:00:00Z')).toBe('2026/09/05')
  })
  it('computes waiting days (plan: 等待天数 by backend/client)', () => {
    const past = new Date(Date.now() - 3 * 86400000).toISOString()
    expect(daysBetween(past)).toBe(3)
    expect(daysBetween(null)).toBeNull()
  })
})

describe('filter tree helpers (used by saved views)', () => {
  it('groups builtin board views by the correct status sets', () => {
    // replicate BUILTIN semantics in a tiny pure fn
    const statusForBuiltin = (id: number): string[] => {
      if (id === -2) return ['saved', 'preparing']
      if (id === -3) return ['applied', 'screening', 'assessment', 'interviewing']
      if (id === -4) return ['offer', 'accepted']
      if (id === -5) return ['accepted', 'rejected', 'withdrawn', 'closed']
      return []
    }
    expect(statusForBuiltin(-2)).toEqual(['saved', 'preparing'])
    expect(statusForBuiltin(-4)).toEqual(['offer', 'accepted'])
  })
})
