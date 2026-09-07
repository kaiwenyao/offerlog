// Pure helpers for the calendar/agenda view — week/day window math plus
// grouping events into the Mon-Sun week grid. Kept free of React so it is
// unit-testable.
//
// All bucketing of event *instants* into calendar days is done in the signed-in
// user's timezone (the same zone the server uses for week windows), so a
// browser elsewhere cannot disagree with the server. The zone is passed in by
// the caller (default: the browser's own zone).
import type { CalendarEvent } from '../../lib/types'
import { toDayString } from '../../lib/api'

export type ViewMode = 'week' | 'month' | 'agenda'

const DAY_MS = 86_400_000
export const WEEKDAYS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

/** Monday 00:00 local of the week containing now. */
export function mondayOf(now: Date = new Date()): Date {
  const d = new Date(now)
  d.setHours(0, 0, 0, 0)
  const offset = (d.getDay() + 6) % 7
  d.setDate(d.getDate() - offset)
  return d
}

/** First day of the local month. */
export function monthStart(now: Date = new Date()): Date {
  return new Date(now.getFullYear(), now.getMonth(), 1)
}

export function addDays(d: Date, n: number): Date {
  const c = new Date(d)
  c.setDate(c.getDate() + n)
  return c
}

/** Serialize a Date to the RFC3339 the API expects (UTC instant). */
export function toISO(d: Date): string {
  return d.toISOString()
}

export interface DayCell {
  date: Date
  isToday: boolean
  inMonth?: boolean
  events: CalendarEvent[]
}

/**
 * The calendar-day key (YYYY-MM-DD) an event belongs to, in the user's zone.
 * All-day events arrive at the user's local midnight so this is exact; point
 * events use the same zone the server-side windows use.
 */
function eventDay(e: CalendarEvent, zone?: string): string | null {
  if (!e.start) return null
  return toDayString(e.start, zone)
}

/** Build the 7 columns of the week view starting at weekStart (Monday). */
export function weekColumns(events: CalendarEvent[], weekStart: Date, zone?: string): DayCell[] {
  return WEEKDAYS.map((_, i) => {
    const date = addDays(weekStart, i)
    const key = dateKey(date)
    const dayEvents = events.filter((e) => eventDay(e, zone) === key)
    return { date, isToday: sameLocalDay(date, new Date()), events: dayEvents }
  })
}

/** Build a month grid (leading/trailing blanks filled from adjacent weeks). */
export function monthGrid(events: CalendarEvent[], first: Date, zone?: string): DayCell[][] {
  const startDow = (first.getDay() + 6) % 7 // Mon=0
  const gridStart = addDays(first, -startDow)
  const weeks: DayCell[][] = []
  for (let w = 0; w < 6; w++) {
    const cols: DayCell[] = []
    for (let i = 0; i < 7; i++) {
      const date = addDays(gridStart, w * 7 + i)
      const key = dateKey(date)
      const dayEvents = events.filter((e) => eventDay(e, zone) === key)
      cols.push({
        date,
        isToday: sameLocalDay(date, new Date()),
        inMonth: date.getMonth() === first.getMonth(),
        events: dayEvents,
      })
    }
    weeks.push(cols)
  }
  return weeks
}

export interface AgendaGroup {
  title: string
  items: CalendarEvent[]
}

/**
 * Group events into agenda buckets: 已逾期（仅 action，仍在用户本地今天之前）/
 * 今天 / 未来 7 天 / 之后。逾期待办必须渲染出来，不能收进一个永不展示的桶。
 */
export function agenda(events: CalendarEvent[], now: Date = new Date(), zone?: string): AgendaGroup[] {
  const todayKey = dateKey(now)
  const soon = addDays(now, 7)
  const soonKey = dateKey(soon)

  const overdue: CalendarEvent[] = []
  const today: CalendarEvent[] = []
  const next7: CalendarEvent[] = []
  const later: CalendarEvent[] = []
  for (const e of events) {
    const day = eventDay(e, zone)
    if (!day) {
      later.push(e) // undated
      continue
    }
    if (e.kind === 'action' && !e.done && day < todayKey) overdue.push(e)
    else if (day === todayKey) today.push(e)
    else if (day < soonKey) next7.push(e)
    else later.push(e)
  }
  const sortByDay = (a: CalendarEvent, b: CalendarEvent) =>
    (a.start ?? '').localeCompare(b.start ?? '')
  const groups: AgendaGroup[] = []
  if (overdue.length) {
    groups.push({ title: '已逾期', items: overdue.sort(sortByDay) })
  }
  if (today.length) groups.push({ title: '今天', items: today.sort(sortByDay) })
  if (next7.length) groups.push({ title: '未来 7 天', items: next7.sort(sortByDay) })
  if (later.length) groups.push({ title: '之后', items: later.sort(sortByDay) })
  return groups
}

/**
 * Fetch window for the agenda view: overdue actions can be arbitrarily old,
 * so the fetch spans [−90d, +30d] around the current week — wide enough that
 * the “已逾期” bucket is not empty for real data, and far enough forward that
 * upcoming items render too.
 */
export function agendaWindow(anchor: Date): { from: Date; to: Date } {
  const monday = mondayOf(anchor)
  return { from: addDays(monday, -90), to: addDays(monday, 30) }
}

/** YYYY-MM-DD of a Date in the browser-local calendar. */
function dateKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function sameLocalDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}
