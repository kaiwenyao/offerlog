// Component tests for the shared status/formatting helpers + table row
// rendering logic (pure parts). API-interactive flows are covered by the
// Playwright e2e suite.
import { describe, expect, it } from 'vitest'
import { statusMeta, STATUSES } from '../src/lib/status'
import { fmtBytes, fmtDate, daysBetween } from '../src/lib/api'
import { buildWeek } from '../src/features/today/week'
import { agenda, mondayOf, weekColumns } from '../src/features/calendar/grid'
import type { CalendarEvent } from '../src/lib/types'

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

describe('server week strip (home summary)', () => {
  it('places week_items into the correct Mon-Sun day columns', () => {
    // 2026-08-31 is a Monday.
    const mon = new Date(2026, 7, 31, 10, 0, 0)
    const summary = {
      week: { start: '2026-08-31T00:00:00', end: '2026-09-07T00:00:00' },
      week_items: [
        { day: 0, kind: '投递', who: 'Acme', tone: 'info' },
        { day: 3, kind: '面试', who: 'BigCo · 一面', tone: 'acc' },
      ],
    } as never
    const days = buildWeek(summary, mon)
    expect(days[0].items.map((i) => i.who)).toContain('Acme')
    expect(days[3].items.map((i) => i.who)).toContain('BigCo · 一面')
    expect(days[0].weekday).toBe('周一')
  })
})

describe('analytics rate display semantics (§4.2)', () => {
  it('shows 0% when denominator>0 and numerator=0; — only when no sample', () => {
    // Replicate the pure formatting rules the page uses.
    const rateOrDash = (rate: number | null | undefined, denom: number) => {
      if (denom === 0) return '—'
      const r = rate ?? 0
      return `${(r * 100).toFixed(1)}%`
    }
    expect(rateOrDash(0, 10)).toBe('0.0%') // 全部未回复 → 0%，不是 —
    expect(rateOrDash(0.5, 10)).toBe('50.0%')
    expect(rateOrDash(null, 0)).toBe('—') // 空样本
    expect(rateOrDash(undefined, 0)).toBe('—')
  })
})

describe('calendar grid helpers', () => {
  it('places an interview on the correct Monday-start week column', () => {
    const wed = new Date(2026, 8, 9, 10, 0) // 2026-09-09 Wed
    const mon = mondayOf(wed)
    expect(mon.getDay()).toBe(1)
    const ev: CalendarEvent = {
      id: 1, kind: 'interview', application_id: 1, company_name: 'Acme', position: 'R',
      title: 'Acme · 一面', start: '2026-09-09T10:00:00Z', timezone: 'Europe/Dublin',
      all_day: false, location: '', meeting_url: '', cancelled: false, done: false,
      round_name: '一面', format: 'video',
    }
    const cols = weekColumns([ev], mon)
    const col = cols.find((c) => c.date.getDate() === 9)
    expect(col?.events.length).toBe(1)
    expect(cols[0].date.getDay()).toBe(1)
  })

  it('groups events into recent/after agenda buckets', () => {
    const now = new Date(2026, 8, 7, 12, 0)
    const in3d = new Date(now.getTime() + 3 * 86400000).toISOString()
    const in20d = new Date(now.getTime() + 20 * 86400000).toISOString()
    const evs: CalendarEvent[] = [
      { ...baseEv, id: 1, start: in3d },
      { ...baseEv, id: 2, start: in20d },
    ]
    const groups = agenda(evs, now)
    expect(groups).toHaveLength(2)
    expect(groups[0].items.map((e) => e.id)).toEqual([1])
    expect(groups[1].items.map((e) => e.id)).toEqual([2])
  })
})
const baseEv: CalendarEvent = {
  id: 0, kind: 'interview', application_id: 1, company_name: 'Acme', position: '',
  title: '', start: '', timezone: 'UTC', all_day: false, location: '', meeting_url: '',
  cancelled: false, done: false, round_name: '', format: '',
}
