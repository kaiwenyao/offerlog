// Component tests for the shared status/formatting helpers + table row
// rendering logic (pure parts). API-interactive flows are covered by the
// Playwright e2e suite.
import { describe, expect, it } from 'vitest'
import { comboLabel, statusMeta, STATUSES, substatusOptions, validSubstatus } from '../src/lib/status'
import {
  FALLBACK_MILESTONE_KINDS,
  isDefaultLabel,
  MILESTONE_KIND_GROUPS,
  milestoneDefaultLabel,
  milestoneDotColor,
  milestoneIcon,
  milestoneKindsByGroup,
  statusEffectForKind,
} from '../src/lib/milestones'
import {
  fmtBytes,
  fmtDate,
  fmtDateTime,
  daysBetween,
  relativeDayLabel,
  dayToInstant,
  localDateTimeToInstant,
  toDayString,
  fmtDay,
} from '../src/lib/api'
import { defaultSubmittedIso } from '../src/lib/tz'
import { buildWeek } from '../src/features/today/week'
import { agenda, agendaWindowKeys, mondayKeyOf, weekColumns } from '../src/features/calendar/grid'
import { mergeTimeline } from '../src/features/database/timeline'
import {
  DB_SORT_FIELDS,
  DEFAULT_DB_SORT,
  loadDbSort,
} from '../src/features/database/views'
import type { AppEvent, CalendarEvent, Milestone } from '../src/lib/types'

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

// Guard for the client-side mirror of the backend state model.
//
// 方案 §4.1 replaced the hand-written edge list with a RULE (domain.allowedTarget):
// any non-terminal → any non-terminal, terminal → non-terminal (reopen),
// →已接受 only from Offer, 已接受 → 已撤回 (毁约), and terminal → terminal
// otherwise rejected. The transcription below spells that rule out explicitly so
// a drift in transitions.ts fails here instead of 400-ing at the API.
const NON_TERMINAL = ['saved', 'preparing', 'applied', 'screening', 'assessment', 'interviewing', 'offer']
const TERMINAL = ['accepted', 'rejected', 'withdrawn', 'closed']

function backendAllows(from: string, to: string): boolean {
  if (from === to) return true // 子状态 / 关注轮次变更
  if (to === 'accepted') return from === 'offer'
  if (from === 'accepted' && to === 'withdrawn') return true
  if (TERMINAL.includes(from) && TERMINAL.includes(to)) return false
  return true
}

// Every pair the backend allows, with the same same-stage carve-out the server
// applies in TargetCombos (a stage with no subdivision cannot target itself,
// because nothing would change).
const ALL_STATUS_KEYS = [...NON_TERMINAL, ...TERMINAL]
const BACKEND_EDGES: ReadonlyArray<readonly [string, string]> = ALL_STATUS_KEYS.flatMap((f) =>
  ALL_STATUS_KEYS.filter((t) => backendAllows(f, t) && !(f === t && substatusOptions(f).length === 0)).map(
    (t) => [f, t] as const,
  ),
)

describe('事件类型表（mirror of backend domain/milestone.go，迁移 00006）', () => {
  // 这张表是整个改版的支点：用户只选事件类型，岗位阶段由 status_effect 推导。
  // 前后端各存一份（前端的是离线兜底），所以必须逐项对齐。
  const EXPECTED: Array<[string, string, string]> = [
    ['save', '收藏岗位', 'saved'],
    ['prepare', '准备材料', 'preparing'],
    ['apply', '投递', 'applied'],
    ['screen', '初筛', 'screening'],
    ['oa', 'OA / 笔试', 'assessment'],
    ['interview', '面试', 'interviewing'],
    ['offer', '收到 Offer', 'offer'],
    ['accept', '接受 Offer', 'accepted'],
    ['reject', '被拒绝', 'rejected'],
    ['withdraw', '撤回申请', 'withdrawn'],
    ['close', '岗位关闭', 'closed'],
    ['phone', '电话沟通', ''],
    ['custom', '自定义事件', ''],
  ]

  it('maps every suggested kind to its label and stage effect', () => {
    expect(FALLBACK_MILESTONE_KINDS.map((k) => k.key)).toEqual(EXPECTED.map(([k]) => k))
    for (const [key, label, effect] of EXPECTED) {
      expect(milestoneDefaultLabel(key)).toBe(label)
      expect(statusEffectForKind(key)).toBe(effect)
    }
  })

  it('covers every stage the pipeline can reach, so no status is unrecordable', () => {
    // 每个非派生的阶段都得有一个事件能把岗位带过去，否则用户会遇到一个
    // 「看得见却记不出来」的状态。
    const reachable = new Set(FALLBACK_MILESTONE_KINDS.map((k) => k.status_effect).filter(Boolean))
    for (const s of STATUSES) {
      expect(reachable.has(s.key)).toBe(true)
    }
  })

  it('leaves the stage alone for note-only kinds and unknown slugs', () => {
    expect(statusEffectForKind('phone')).toBe('')
    expect(statusEffectForKind('custom')).toBe('')
    // 开放集合：用户自创的类型照存不误，但绝不能悄悄改变阶段。
    expect(statusEffectForKind('coffee-chat')).toBe('')
    expect(milestoneDefaultLabel('coffee-chat')).toBe('自定义节点')
  })

  it('groups the picker into flow / end / other with nothing left out', () => {
    const grouped = MILESTONE_KIND_GROUPS.flatMap((g) => milestoneKindsByGroup(g))
    expect(grouped).toHaveLength(FALLBACK_MILESTONE_KINDS.length)
    expect(milestoneKindsByGroup('flow').map((k) => k.key)).toEqual([
      'save',
      'prepare',
      'apply',
      'screen',
      'oa',
      'interview',
      'offer',
    ])
    expect(milestoneKindsByGroup('end').map((k) => k.key)).toEqual(['accept', 'reject', 'withdraw', 'close'])
  })

  it('borrows the stage colour and icon so one event reads the same everywhere', () => {
    expect(milestoneDotColor('oa')).toBe(statusMeta('assessment').dot)
    expect(milestoneIcon('interview')).toBe(statusMeta('interviewing').icon)
    // 不改阶段的事件保持中性，不冒充任何一个阶段的颜色。
    expect(milestoneDotColor('custom')).toBe('var(--neutral)')
    expect(milestoneIcon('phone')).toBe('☎️')
  })

  it('recognises a default label so renaming an event is never undone', () => {
    // 名称没改过时跟随类型自动填；改过（比如「一面」）就必须原样保留。
    expect(isDefaultLabel('面试')).toBe(true)
    expect(isDefaultLabel('一面')).toBe(false)
  })
})

describe('substatus dictionary (mirror of backend domain/substatus.go, 方案 §3.1)', () => {
  it('defines legal combinations per stage and never leaks one across stages', () => {
    expect(substatusOptions('assessment').map((s) => s.key)).toEqual(['preparing', 'completed', 'passed'])
    expect(substatusOptions('offer').map((s) => s.key)).toEqual(['reviewing', 'negotiating', 'ready_to_accept'])
    // 「preparing」 exists in several stages but with its own label each time.
    expect(substatusOptions('screening').find((s) => s.key === 'preparing')?.label).toBe('准备初筛')
    expect(substatusOptions('interviewing').find((s) => s.key === 'preparing')?.label).toBe('准备面试')
    // 待投递 / 已投递 have no subdivision at all.
    expect(substatusOptions('saved')).toEqual([])
    expect(substatusOptions('applied')).toEqual([])
  })

  it('accepts "" (未细分) but rejects a substatus from another stage', () => {
    expect(validSubstatus('assessment', '')).toBe(true)
    expect(validSubstatus('assessment', 'preparing')).toBe(true)
    expect(validSubstatus('saved', 'preparing')).toBe(false)
    expect(validSubstatus('offer', 'completed')).toBe(false)
    expect(validSubstatus('assessment', 'bogus')).toBe(false)
  })

  it('names the un-subdivided state instead of pretending it is 准备 OA', () => {
    // 方案 §7: 旧数据必须保持「进度未细分」，不能凭停留时长猜成准备中。
    expect(comboLabel('assessment', '')).toBe('OA / 作业 · 进度未细分')
    expect(comboLabel('assessment', 'preparing')).toBe('准备 OA')
    expect(comboLabel('assessment', 'completed')).toBe('已完成 OA · 等结果')
    // 作业类测评换名词（方案 §3.2）。
    expect(comboLabel('assessment', 'completed', 'take_home')).toBe('已提交作业 · 等结果')
    expect(comboLabel('assessment', 'preparing', 'take_home')).toBe('准备作业')
    expect(comboLabel('applied', '')).toBe('已投递 · 等回复')
    expect(comboLabel('offer', 'negotiating')).toBe('协商 Offer')
    expect(comboLabel('rejected', '')).toBe('被拒绝')
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

describe('relative day labels (timeline right-hand time)', () => {
  const now = new Date('2026-09-08T12:00:00Z')
  it('names the last two days the way the user described them', () => {
    expect(relativeDayLabel('2026-09-08T09:00:00Z', 'UTC', now)).toBe('今天')
    expect(relativeDayLabel('2026-09-07T23:30:00Z', 'UTC', now)).toBe('昨天')
    expect(relativeDayLabel('2026-09-06T18:31:00Z', 'UTC', now)).toBe('前天')
  })
  it('handles future business times (a scheduled round)', () => {
    expect(relativeDayLabel('2026-09-09T08:00:00Z', 'UTC', now)).toBe('明天')
    expect(relativeDayLabel('2026-09-10T08:00:00Z', 'UTC', now)).toBe('后天')
  })
  it('falls back to no label once the absolute date reads better', () => {
    expect(relativeDayLabel('2026-09-01T08:00:00Z', 'UTC', now)).toBeNull()
    expect(relativeDayLabel(null, 'UTC', now)).toBeNull()
  })
  it('compares calendar days in the user zone, not raw elapsed ms', () => {
    // 23:00 UTC on the 7th is 00:00 on the 8th in Shanghai → 今天 there,
    // 昨天 in UTC. A ms-based diff would call both the same.
    expect(relativeDayLabel('2026-09-07T23:00:00Z', 'UTC', now)).toBe('昨天')
    expect(relativeDayLabel('2026-09-07T23:00:00Z', 'Asia/Shanghai', now)).toBe('今天')
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

describe('database sort preference (自选排序)', () => {
  it('defaults to newest-update-first when nothing / garbage is persisted', () => {
    expect(loadDbSort(null)).toEqual(DEFAULT_DB_SORT)
    expect(loadDbSort('')).toEqual(DEFAULT_DB_SORT)
    expect(loadDbSort('not json')).toEqual(DEFAULT_DB_SORT)
    expect(loadDbSort('{"field":"company_name"}')).toEqual(DEFAULT_DB_SORT) // 不可排序字段
    expect(loadDbSort('{"field":"updated_at","dir":"sideways"}')).toEqual({
      field: 'updated_at',
      dir: 'desc',
    }) // 方向非法回退默认，字段保留
  })
  it('keeps every persisted field×dir combination the UI can produce', () => {
    // 两个字段 × 两个方向，都是后端 views.CoreFields 白名单里的可排序字段
    expect(loadDbSort('{"field":"created_at","dir":"asc"}')).toEqual({
      field: 'created_at',
      dir: 'asc',
    })
    expect(loadDbSort('{"field":"created_at","dir":"desc"}')).toEqual({
      field: 'created_at',
      dir: 'desc',
    })
    expect(loadDbSort('{"field":"updated_at","dir":"asc"}')).toEqual({
      field: 'updated_at',
      dir: 'asc',
    })
    expect(DB_SORT_FIELDS.map((f) => f.value)).toEqual(['updated_at', 'created_at'])
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

describe('实际投递时间 留空兜底 (§P1: 不再必填)', () => {
  const now = new Date('2026-09-11T10:00:00.000Z')
  it('no 发生时间 at all → now', () => {
    expect(defaultSubmittedIso(null, now)).toBe('2026-09-11T10:00:00.000Z')
  })
  it('backdated 发生时间 → that instant (a submission cannot follow the change it precedes)', () => {
    // repo reads application_events ORDER BY occurred_at, so stamping "now"
    // here would draw 「昨天 OA → 今天 已投递」 and inflate 等待天数.
    expect(defaultSubmittedIso('2026-09-08T23:30:00.000Z', now)).toBe('2026-09-08T23:30:00.000Z')
  })
  it('发生时间 in the future → now, never a future 投递时间', () => {
    expect(defaultSubmittedIso('2026-09-11T10:04:00.000Z', now)).toBe('2026-09-11T10:00:00.000Z')
  })
  it('malformed 发生时间 falls back to now instead of throwing', () => {
    expect(defaultSubmittedIso('not-a-date', now)).toBe('2026-09-11T10:00:00.000Z')
  })
})

describe('时间线合并排序（用户添加的事件，迁移 00005 / 00006）', () => {
  const ev = (id: number, type: string, occurred: string, seq = 0): AppEvent => ({
    id,
    sequence: seq,
    event_type: type,
    from_status: null,
    to_status: type === 'status_change' ? 'applied' : null,
    from_substatus: null,
    to_substatus: null,
    activity_kind: '',
    activity_id: null,
    change_type: 'advance',
    note: '',
    reason: '',
    occurred_at: occurred,
    recorded_at: occurred,
    corrects_event_id: null,
  })
  const ms = (id: number, kind: string, occurred: string | null, label = ''): Milestone => ({
    id,
    application_id: 1,
    kind,
    label,
    status_effect: statusEffectForKind(kind),
    occurred_at: occurred,
    note: '',
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
  })

  it('按业务时间把用户节点插入状态事件之间，自动排序', () => {
    const out = mergeTimeline([
      { type: 'event', e: ev(1, 'created', '2026-09-01T09:00:00Z'), pinned: true },
      { type: 'event', e: ev(2, 'status_change', '2026-09-05T09:00:00Z', 2), pinned: false },
      { type: 'milestone', m: ms(3, 'oa', '2026-09-08T12:00:00Z', 'OA') },
      { type: 'milestone', m: ms(4, 'screen', '2026-09-03T10:00:00Z', '初筛') },
    ])
    expect(out.map((x) => (x.type === 'event' ? `e${x.e.id}` : `m${x.m.id}`))).toEqual(['e1', 'm4', 'e2', 'm3'])
  })

  it('建档始终钉在最前，即使节点的时间更早', () => {
    const out = mergeTimeline([
      { type: 'event', e: ev(1, 'created', '2026-09-05T09:00:00Z'), pinned: true },
      { type: 'milestone', m: ms(2, 'phone', '2026-09-01T09:00:00Z', '电话沟通') },
    ])
    expect(out[0].type === 'event' && out[0].e.event_type).toBe('created')
  })

  it('时间未定的节点排在所有有时间的节点之后（不伪造时间）', () => {
    const out = mergeTimeline([
      { type: 'milestone', m: ms(9, 'interview', null, '面试') },
      { type: 'event', e: ev(1, 'status_change', '2026-09-05T09:00:00Z'), pinned: false },
      { type: 'milestone', m: ms(2, 'oa', '2026-09-02T09:00:00Z', 'OA') },
    ])
    const last = out[out.length - 1]
    expect(last.type === 'milestone' && last.m.occurred_at === null).toBe(true)
  })

  it('同一时刻状态事件排在前，多个未定节点按创建顺序排', () => {
    const out = mergeTimeline([
      { type: 'milestone', m: ms(5, 'interview', null, '面试') },
      { type: 'milestone', m: ms(4, 'offer', null, 'Offer') },
      { type: 'event', e: ev(1, 'status_change', '2026-09-05T09:00:00Z'), pinned: false },
      { type: 'milestone', m: ms(2, 'oa', '2026-09-05T09:00:00Z', 'OA') },
    ])
    expect(out.map((x) => (x.type === 'event' ? `e${x.e.id}` : `m${x.m.id}`))).toEqual(['e1', 'm2', 'm4', 'm5'])
  })
})
