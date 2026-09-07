// Pure helpers for the calendar/agenda view — week/day window math over the
// user's own timezone via explicit instants, plus grouping events into the
// Mon-Sun week grid. Kept free of React so it is unit-testable.
import type { CalendarEvent } from '../../lib/types'

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

/** Build the 7 columns of the week view starting at weekStart (Monday). */
export function weekColumns(events: CalendarEvent[], weekStart: Date): DayCell[] {
  return WEEKDAYS.map((_, i) => {
    const date = addDays(weekStart, i)
    const dayEvents = events.filter((e) => eventDayIndex(e, weekStart) === i)
    return { date, isToday: sameLocalDay(date, new Date()), events: dayEvents }
  })
}

/** Build a month grid (leading/trailing blanks filled from adjacent weeks). */
export function monthGrid(events: CalendarEvent[], first: Date): DayCell[][] {
  const startDow = (first.getDay() + 6) % 7 // Mon=0
  const gridStart = addDays(first, -startDow)
  const weeks: DayCell[][] = []
  for (let w = 0; w < 6; w++) {
    const cols: DayCell[] = []
    for (let i = 0; i < 7; i++) {
      const date = addDays(gridStart, w * 7 + i)
      const dayEvents = events.filter((e) => sameLocalDay(new Date(e.start ?? e.dueDate ?? ''), date))
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

/** Group events into agenda buckets: 今天 / 未来 7 天 / 之后（排序）。 */
export function agenda(events: CalendarEvent[], now: Date = new Date()): Array<{ title: string; items: CalendarEvent[] }> {
  const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const soon = todayStart + 7 * DAY_MS
  const future: CalendarEvent[] = []
  const thisWeek: CalendarEvent[] = []
  const later: CalendarEvent[] = []
  const overdue: CalendarEvent[] = []
  for (const e of events) {
    const at = new Date(e.start ?? e.dueDate ?? '').getTime()
    if (e.kind === 'action' && e.start && at < todayStart) overdue.push(e)
    else if (at < soon) thisWeek.push(e)
    else future.push(e)
  }
  if (overdue.length) later.push(...overdue)
  const groups: Array<{ title: string; items: CalendarEvent[] }> = []
  if (thisWeek.length) groups.push({ title: '近期（7 天内）', items: thisWeek })
  if (future.length) groups.push({ title: '之后', items: future })
  return groups
}

function eventDayIndex(e: CalendarEvent, weekStart: Date): number {
  const at = new Date(e.start ?? e.dueDate ?? '')
  if (isNaN(at.getTime())) return -1
  const start = weekStart.getTime()
  const diff = Math.round((localMidnight(at).getTime() - start) / DAY_MS)
  return diff >= 0 && diff < 7 ? diff : -1
}

function localMidnight(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

export function sameLocalDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}
