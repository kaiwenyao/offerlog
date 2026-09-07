import type { ActionItem, HomeSummary } from '../../lib/types'
import { dayToInstant, toDayString } from '../../lib/api'
import { effectiveZone } from '../../lib/tz'

export interface TodoItem extends ActionItem {
  company_name?: string
  position?: string
  status?: string
  /** Set for standalone actions; absent for legacy derived next_actions. */
  action_id?: number | null
}

export interface TodoGroup {
  title: string
  dot: string
  items: TodoItem[]
}

const DAY_MS = 86_400_000

/** Local midnight (ms) of “today” in the user's zone (browser zone fallback). */
export function startOfDay(d: Date = new Date()): number {
  const zone = effectiveZone()
  if (zone) {
    const ds = toDayString(d.toISOString(), zone)
    const inst = ds ? dayToInstant(ds, zone) : null
    if (inst != null) return inst
  }
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

/**
 * Due as an instant: due_ts is already one; date-only due_date is interpreted
 * as midnight of that calendar day in the user's zone (matching the server).
 */
export function dueTime(item: Pick<TodoItem, 'due_ts' | 'due_date'>): number {
  if (item.due_ts) return new Date(item.due_ts).getTime()
  const day = toDayString(item.due_date, effectiveZone())
  const inst = day ? dayToInstant(day, effectiveZone()) : null
  return inst ?? Number.POSITIVE_INFINITY
}

/** Split open work into the design's 已逾期 / 今天 / 未来 7 天 / 更晚 buckets. */
export function groupActions(actions: TodoItem[]): TodoGroup[] {
  const today = startOfDay()
  const weekEnd = today + 7 * DAY_MS
  const groups: TodoGroup[] = [
    { title: '已逾期', dot: 'var(--danger)', items: [] },
    { title: '今天', dot: 'var(--accent)', items: [] },
    { title: '未来 7 天', dot: 'var(--info)', items: [] },
    { title: '更晚', dot: 'var(--neutral)', items: [] },
  ]
  for (const a of actions) {
    const due = dueTime(a)
    if (due === Number.POSITIVE_INFINITY) groups[3].items.push(a)
    else if (due < today) groups[0].items.push(a)
    else if (due < today + DAY_MS) groups[1].items.push(a)
    else if (due <= weekEnd) groups[2].items.push(a)
    else groups[3].items.push(a)
  }
  return groups.filter((g) => g.items.length > 0)
}

export interface WeekDay {
  weekday: string
  dayNum: number
  isToday: boolean
  items: Array<{ kind: string; who: string; tone: 'info' | 'warn' | 'good' | 'acc' | 'bad' }>
}

const WEEKDAYS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

export const CHIP_TONES: Record<WeekDay['items'][number]['tone'], { bg: string; fg: string }> = {
  info: { bg: 'var(--info-soft)', fg: 'var(--info-strong)' },
  warn: { bg: 'var(--warning-soft)', fg: 'var(--warning-strong)' },
  good: { bg: 'var(--positive-soft)', fg: 'var(--positive-strong)' },
  acc: { bg: 'var(--accent-soft)', fg: 'var(--accent-hover)' },
  bad: { bg: 'var(--danger-soft)', fg: 'var(--danger-strong)' },
}

/**
 * Build the Monday→Sunday strip from the server-computed week. The week's
 * start (summary.week.start) is the user-zone Monday 00:00 instant; each day
 * cell's number / weekday / “is today” is derived from that instant in the
 * user zone (via the zone-aware helpers), so a browser in another timezone
 * shows the same seven calendar days the server counted. week_items already
 * carry server-computed day indexes (Mon=0..Sun=6 in the user zone) and are
 * placed as-is.
 */
export function buildWeek(summary: Pick<HomeSummary, 'week' | 'week_items'>, now: Date = new Date()): WeekDay[] {
  const zone = effectiveZone()
  const todayKey = toDayString(now.toISOString(), zone)
  const startIso = summary.week.start
  const startMs = startIso ? dayToInstant(toDayString(startIso, zone), zone) : null

  const days: WeekDay[] = WEEKDAYS.map((weekday, i) => {
    // Monday 00:00 user-local + i calendar days.
    let key: string | null = null
    if (startMs != null) {
      const ts = addDaysUtc(startMs, i, zone)
      key = toDayString(new Date(ts).toISOString(), zone)
    }
    return {
      weekday,
      dayNum: key ? Number(key.slice(8, 10)) : 0,
      isToday: key != null && key === todayKey,
      items: [],
    }
  })

  const items = summary.week_items ?? []
  for (const it of items) {
    const day = it.day
    if (day < 0 || day > 6) continue
    days[day].items.push({ kind: it.kind, who: it.who, tone: it.tone })
  }
  return days
}

/** Add n calendar days to a user-zone midnight instant (DST-safe). */
function addDaysUtc(ms: number, n: number, zone?: string): number {
  // Work on the day key, then resolve back to a user-zone midnight.
  const startKey = toDayString(new Date(ms).toISOString(), zone)
  if (!startKey) return ms + n * DAY_MS
  const [y, m, d] = startKey.split('-').map(Number)
  const t = new Date(Date.UTC(y, m - 1, d + n))
  const key = t.toISOString().slice(0, 10)
  const inst = dayToInstant(key, zone)
  return inst ?? ms + n * DAY_MS
}

export interface Kpis {
  inProgress: number
  submittedThisWeek: number
  awaitingReply: number
  overdue: number
}

const IN_PROGRESS = new Set(['applied', 'screening', 'assessment', 'interviewing'])

/** Compatibility helper — the dashboard now reads its KPIs from /home/summary. */
export function buildKpis(rows: HomeSummary['recent']): Kpis {
  const todayStart = startOfDay()
  const weekStart = todayStart - ((new Date().getDay() + 6) % 7) * DAY_MS
  let inProgress = 0
  let submittedThisWeek = 0
  let awaitingReply = 0
  let overdue = 0
  for (const r of rows as Array<{ status: string; submitted_at?: string | null; next_action?: string }>) {
    if (IN_PROGRESS.has(r.status)) inProgress += 1
    if (r.submitted_at && new Date(r.submitted_at).getTime() >= weekStart) submittedThisWeek += 1
  }
  return { inProgress, submittedThisWeek, awaitingReply, overdue }
}
