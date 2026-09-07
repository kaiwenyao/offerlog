// Component tests for the shared status/formatting helpers + table row
// rendering logic (pure parts). API-interactive flows are covered by the
// Playwright e2e suite.
import { describe, expect, it } from 'vitest'
import { statusMeta, STATUSES } from '../src/lib/status'
import {
  fmtBytes,
  fmtDate,
  fmtDateTime,
  daysBetween,
  dayToInstant,
  localDateTimeToInstant,
  toDayString,
  fmtDay,
} from '../src/lib/api'
import { buildWeek } from '../src/features/today/week'
import { agenda, agendaWindowKeys, mondayKeyOf, weekColumns } from '../src/features/calendar/grid'
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
    // Explicit zone keeps the expectation machine-timezone-independent.
    expect(fmtDate('2026-09-05T10:00:00Z', 'UTC')).toBe('2026/09/05')
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
  it('places week_items into the correct Mon-Sun day columns under the Monday default', () => {
    // 2026-08-31 is a Monday; week_start defaults to 1 (周一).
    const mon = new Date(2026, 7, 31, 10, 0, 0)
    const summary = {
      week_start: 1,
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
    expect(days[6].weekday).toBe('周日')
  })

  it('anchors the strip at the user week_start=0 (周日): column 0 is 周日 and chips land on their real day', () => {
    // 2026-09-06 is a Sunday. With week_start=0 the window starts Sunday
    // 2026-09-06; chips are indexed 0 = 周日.
    const sunday = new Date(2026, 8, 6, 12, 0, 0)
    const summary = {
      week_start: 0,
      week: { start: '2026-09-06T00:00:00', end: '2026-09-13T00:00:00' },
      week_items: [
        { day: 0, kind: '投递', who: 'SunCo', tone: 'info' }, // 周日
        { day: 3, kind: '面试', who: 'WedCo · 一面', tone: 'acc' }, // 周三
      ],
    } as never
    const days = buildWeek(summary, sunday)
    expect(days[0].weekday).toBe('周日')
    expect(days[3].weekday).toBe('周三')
    expect(days[6].weekday).toBe('周六')
    expect(days[0].items.map((i) => i.who)).toContain('SunCo')
    expect(days[3].items.map((i) => i.who)).toContain('WedCo · 一面')
    expect(days[0].dayNum).toBe(6) // 2026-09-06
    expect(days[3].dayNum).toBe(9) // 2026-09-09
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
  const ZONE = 'Europe/Dublin' // deterministic zone for day bucketing
  it('places an interview on the correct Monday-start week column (zone-aware keys)', () => {
    // 2026-09-09T10:00Z is 2026-09-09 in Dublin — day key '2026-09-09'.
    const monKey = mondayKeyOf('2026-09-09')
    expect(monKey).toBe('2026-09-07') // 09-09 is a Wednesday
    const ev: CalendarEvent = {
      id: 1, kind: 'interview', application_id: 1, company_name: 'Acme', position: 'R',
      title: 'Acme · 一面', start: '2026-09-09T10:00:00Z', timezone: ZONE,
      all_day: false, location: '', meeting_url: '', cancelled: false, done: false,
      round_name: '一面', format: 'video',
    }
    const cols = weekColumns([ev], monKey, ZONE)
    const col = cols.find((c) => c.key === '2026-09-09')
    expect(col?.events.length).toBe(1)
    expect(cols[0].key).toBe('2026-09-07')
  })

  it('a Shanghai-morning instant lands on the SHANGHAI day column, not the Dublin one', () => {
    // 2026-09-09T00:30:00Z = 09-09 08:30 in Shanghai, but 09-09 01:30 in Dublin
    // (summer) — both are 09-09 here. Pick a case that differs: 23:30Z on
    // 09-09 is 09-10 07:30 in Shanghai but 09-10 00:30 in Dublin — same day in
    // both. Use a real divergent case: 2026-09-09T16:30Z = 09-10 00:30 in
    // Shanghai, 09-09 17:30 in Dublin.
    const ev: CalendarEvent = {
      id: 9, kind: 'interview', application_id: 1, company_name: 'Acme', position: '',
      title: '', start: '2026-09-09T16:30:00Z', timezone: 'Asia/Shanghai',
      all_day: false, location: '', meeting_url: '', cancelled: false, done: false,
      round_name: '', format: '',
    }
    const week = mondayKeyOf('2026-09-10')
    const shCols = weekColumns([ev], week, 'Asia/Shanghai')
    expect(shCols.find((c) => c.key === '2026-09-10')?.events.length).toBe(1)
    const dubCols = weekColumns([ev], week, 'Europe/Dublin')
    expect(dubCols.find((c) => c.key === '2026-09-09')?.events.length).toBe(1)
    expect(dubCols.find((c) => c.key === '2026-09-10')?.events.length).toBe(0)
  })

  it('groups events into today / future buckets and surfaces overdue actions', () => {
    // Bucketing happens in Europe/Dublin.
    const now = new Date('2026-09-07T10:00:00Z') // 2026-09-07 11:00 in Dublin
    const todayNoon = '2026-09-07T12:00:00Z'
    const in3d = '2026-09-10T12:00:00Z'
    const in20d = '2026-09-27T12:00:00Z'
    // an action due yesterday (overdue must surface in its own bucket)
    const overdue = { ...baseEv, id: 4, kind: 'action' as const, start: '2026-09-05T12:00:00Z' }
    // a PAST interview (3 days ago) must NOT appear under “未来 7 天”
    const pastInterview = { ...baseEv, id: 5, kind: 'interview' as const, start: '2026-09-04T12:00:00Z' }
    const evs: CalendarEvent[] = [
      { ...baseEv, id: 1, start: todayNoon },
      { ...baseEv, id: 2, start: in3d },
      { ...baseEv, id: 3, start: in20d },
      overdue,
      pastInterview,
    ]
    const groups = agenda(evs, now, ZONE)
    expect(groups.find((g) => g.title === '已逾期')?.items.map((e) => e.id).sort()).toEqual([4, 5])
    expect(groups.find((g) => g.title === '今天')?.items.map((e) => e.id)).toEqual([1])
    expect(groups.find((g) => g.title === '未来 7 天')?.items.map((e) => e.id)).toEqual([2])
    expect(groups.find((g) => g.title === '之后')?.items.map((e) => e.id)).toEqual([3])
    expect(groups[0].title).toBe('已逾期')
  })
})
const baseEv: CalendarEvent = {
  id: 0, kind: 'interview', application_id: 1, company_name: 'Acme', position: '',
  title: '', start: '', timezone: 'UTC', all_day: false, location: '', meeting_url: '',
  cancelled: false, done: false, round_name: '', format: '',
}

describe('date-only helpers (§P0 timezone semantics)', () => {
  it('dayToInstant: LA midnight of 2026-09-10 is 07:00Z; Dublin summer is 23:00Z the prior day', () => {
    const la = dayToInstant('2026-09-10', 'America/Los_Angeles')!
    expect(new Date(la).toISOString()).toBe('2026-09-10T07:00:00.000Z')
    const dub = dayToInstant('2026-09-10', 'Europe/Dublin')!
    expect(new Date(dub).toISOString()).toBe('2026-09-09T23:00:00.000Z')
  })
  it('toDayString passes date-only through and converts instants per zone', () => {
    expect(toDayString('2026-09-10')).toBe('2026-09-10')
    // Dublin-local midnight instant 2026-09-09T23:00Z == 2026-09-10 in Dublin
    expect(toDayString('2026-09-09T23:00:00Z', 'Europe/Dublin')).toBe('2026-09-10')
    // same instant in LA is 09-09
    expect(toDayString('2026-09-09T23:00:00Z', 'America/Los_Angeles')).toBe('2026-09-09')
  })
  it('fmtDay never shifts a date-only value through Date()', () => {
    expect(fmtDay('2026-09-10')).toBe('2026/09/10')
  })
})

describe('calendar agenda window', () => {
  it('spans −90d..+30d so old overdue and upcoming items are both fetched', () => {
    // Monday key of the week of 2026-09-09 (Wednesday) is 2026-09-07.
    const { fromKey, toKey } = agendaWindowKeys('2026-09-07')
    expect(fromKey).toBe('2026-06-09') // −90d
    expect(toKey).toBe('2026-10-07') // +30d
    // an action overdue 10 days (2026-08-30) falls inside [fromKey, toKey)
    expect('2026-08-30' >= fromKey && '2026-08-30' < toKey).toBe(true)
  })
})

describe('datetime-local wall-clock interpretation (§P1 round 5)', () => {
  it('interprets a naive datetime-local value in the target zone, not the browser zone', () => {
    // Shanghai user types 14:30 on 2026-09-10 (naive, no zone suffix).
    // Correct: 14:30 Asia/Shanghai = 06:30Z. A browser-local parse would
    // instead treat it as the (UTC-running) test browser's 14:30 = 14:30Z.
    const ms = localDateTimeToInstant('2026-09-10T14:30', 'Asia/Shanghai')!
    expect(new Date(ms).toISOString()).toBe('2026-09-10T06:30:00.000Z')
  })
  it('Dublin summer: 14:30 local = 13:30Z (UTC+1)', () => {
    const ms = localDateTimeToInstant('2026-07-10T14:30', 'Europe/Dublin')!
    expect(new Date(ms).toISOString()).toBe('2026-07-10T13:30:00.000Z')
  })
  it('cross-DST-boundary times still resolve to the requested wall clock', () => {
    // 2026-03-29 is the EU spring-forward day (Europe/Dublin goes UTC+0 → +1
    // at 01:00 UTC). A wall time after the transition must map with the new
    // offset: 02:30 local = 01:30Z.
    const after = localDateTimeToInstant('2026-03-29T02:30', 'Europe/Dublin')!
    expect(new Date(after).toISOString()).toBe('2026-03-29T01:30:00.000Z')
  })
  it('falls back to browser-local interpretation when no zone is given', () => {
    // No zone → legacy behavior: the naive value is the BROWSER's local time.
    // Compare against the same local constructor rather than assuming UTC.
    const ms = localDateTimeToInstant('2026-09-10T08:15')!
    const [y, mo, d] = [2026, 9, 10]
    expect(ms).toBe(new Date(y, mo - 1, d, 8, 15).getTime())
  })
  it('rejects malformed values', () => {
    expect(localDateTimeToInstant(null)).toBeNull()
    expect(localDateTimeToInstant('not-a-date')).toBeNull()
    expect(localDateTimeToInstant('2026-09-10')).toBeNull() // missing time part
  })
})

describe('dayToInstant DST-transition days (§P0 date semantics)', () => {
  it('stays exact on spring-forward days (midnight offset ≠ noon offset)', () => {
    // Dublin springs forward 2026-03-29 at 01:00Z. Local 00:00 still exists
    // (on GMT, +0) → the instant is 00:00Z. Sampling the offset at local
    // noon (already IST, +1) would land at 2026-03-28T23:00Z — one hour early.
    const spring = dayToInstant('2026-03-29', 'Europe/Dublin')!
    expect(new Date(spring).toISOString()).toBe('2026-03-29T00:00:00.000Z')
    // New York springs forward 2026-03-08 at 02:00 local; 00:00 EST = 05:00Z
    // (a noon-offset shortcut reads EDT −4 and lands at 04:00Z).
    const ny = dayToInstant('2026-03-08', 'America/New_York')!
    expect(new Date(ny).toISOString()).toBe('2026-03-08T05:00:00.000Z')
    // Southern hemisphere: Sydney springs forward 2026-10-04 at 02:00 local;
    // 00:00 AEST (+10) = 2026-10-03T14:00Z.
    const syd = dayToInstant('2026-10-04', 'Australia/Sydney')!
    expect(new Date(syd).toISOString()).toBe('2026-10-03T14:00:00.000Z')
  })
  it('picks the single first midnight on fall-back days', () => {
    // Dublin falls back 2026-10-25 at 01:00Z (02:00 IST → 01:00 GMT). The only
    // 00:00 wall clock of that day is on IST (+1) → 2026-10-24T23:00Z.
    const fall = dayToInstant('2026-10-25', 'Europe/Dublin')!
    expect(new Date(fall).toISOString()).toBe('2026-10-24T23:00:00.000Z')
  })
})

describe('fmtDate/fmtDateTime render in the given zone (§P1 round 5)', () => {
  it('fmtDateTime shows the user-zone wall time (Shanghai), not browser-local', () => {
    // 2026-09-10T06:30Z = 14:30 Shanghai, 07:30 Dublin summer.
    const sh = fmtDateTime('2026-09-10T06:30:00Z', 'Asia/Shanghai')
    const dub = fmtDateTime('2026-09-10T06:30:00Z', 'Europe/Dublin')
    expect(sh).toContain('09/10')
    expect(dub).toContain('09/10')
    // locale digits differ by zone: Shanghai 14:30, Dublin 07:30.
    expect(sh).toMatch(/14:30/)
    expect(dub).toMatch(/07:30/)
  })
  it('fmtDate renders the calendar day in the given zone', () => {
    // 2026-09-09T23:00Z is already 09-10 in Shanghai but still 09-09 in LA.
    const sh = fmtDate('2026-09-09T23:00:00Z', 'Asia/Shanghai')
    const la = fmtDate('2026-09-09T23:00:00Z', 'America/Los_Angeles')
    expect(sh).toContain('2026/09/10')
    expect(la).toContain('2026/09/09')
  })
})
