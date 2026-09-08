// Pure helpers for the calendar/agenda view — week/day window math plus
// grouping events into the Mon-Sun week grid. Kept free of React so it is
// unit-testable.
//
// One rule governs this file: when a zone is supplied, every calendar-day
// decision is made in that zone (the signed-in user's timezone, the same zone
// the server uses). Internally all day math runs on calendar-day KEYS
// (YYYY-MM-DD strings) — pure day arithmetic with no timezone/DST involvement —
// and real instants are converted to keys through the zone exactly once. A
// browser elsewhere therefore can never disagree with the server's windows.
import type { CalendarEvent } from '../../lib/types'
import { toDayString } from '../../lib/api'

export type ViewMode = 'week' | 'month' | 'agenda'

export const WEEKDAYS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

// ---------------------------------------------------------------------------
// Calendar-day keys (YYYY-MM-DD) and pure day-string arithmetic
// ---------------------------------------------------------------------------

/** Day key of an instant in the given zone (browser zone when none). */
export function dayKeyInZone(d: Date | string, zone?: string): string {
  const iso = typeof d === 'string' ? d : d.toISOString()
  return toDayString(iso, zone) ?? ''
}

/** Day key of “now” in the given zone. */
export function todayKeyInZone(zone?: string): string {
  return dayKeyInZone(new Date(), zone)
}

/** Add n calendar days to a YYYY-MM-DD key (n may be negative). */
export function addDaysToKey(key: string, n: number): string {
  const [y, m, d] = key.split('-').map(Number)
  const t = new Date(Date.UTC(y, m - 1, d + n))
  return t.toISOString().slice(0, 10)
}

/** The Monday key of the week containing key. */
export function mondayKeyOf(key: string): string {
  const [y, m, d] = key.split('-').map(Number)
  const dow = (new Date(Date.UTC(y, m - 1, d)).getUTCDay() + 6) % 7 // Mon=0
  return addDaysToKey(key, -dow)
}

/** Key of the first day of the month containing key. */
export function monthKeyOf(key: string): string {
  return key.slice(0, 8) + '01'
}

// ---------------------------------------------------------------------------
// Grid cells
// ---------------------------------------------------------------------------

export interface DayCell {
  /** Calendar-day key of the column in the active zone (YYYY-MM-DD). */
  key: string
  isToday: boolean
  inMonth?: boolean
  events: CalendarEvent[]
}

/**
 * The calendar-day key an event belongs to in the active zone. All-day events
 * arrive at the user's local midnight so this is exact; point events use the
 * same zone the server-side windows use.
 */
function eventDay(e: CalendarEvent, zone?: string): string | null {
  if (!e.start) return null
  return dayKeyInZone(e.start, zone)
}

/** 7 Monday-start columns; weekStartKey is the Monday key. */
export function weekColumns(events: CalendarEvent[], weekStartKey: string, zone?: string): DayCell[] {
  const today = todayKeyInZone(zone)
  return WEEKDAYS.map((_, i) => {
    const key = addDaysToKey(weekStartKey, i)
    const dayEvents = events.filter((e) => eventDay(e, zone) === key)
    return { key, isToday: key === today, events: dayEvents }
  })
}

/** 6-week month grid starting at the Monday on/before monthKey's 1st. */
export function monthGrid(events: CalendarEvent[], monthKey: string, zone?: string): DayCell[][] {
  const firstKey = monthKeyOf(monthKey)
  const gridStartKey = mondayKeyOf(firstKey)
  const today = todayKeyInZone(zone)
  const weeks: DayCell[][] = []
  for (let w = 0; w < 6; w++) {
    const cols: DayCell[] = []
    for (let i = 0; i < 7; i++) {
      const key = addDaysToKey(gridStartKey, w * 7 + i)
      const dayEvents = events.filter((e) => eventDay(e, zone) === key)
      cols.push({
        key,
        isToday: key === today,
        inMonth: key.slice(0, 7) === monthKey.slice(0, 7),
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
 * Group events into agenda buckets — 已逾期 / 今天 / 未来 7 天 / 之后 — all in
 * the active zone. Every event whose calendar day is before today belongs to
 * the first (past) bucket, whatever its kind: only doing that for open actions
 * let a 3-day-old interview or deadline fall into “未来 7 天”. The −90d fetch
 * window exists precisely so old overdue rows stay visible, and past events
 * must never masquerade as upcoming.
 */
export function agenda(events: CalendarEvent[], now: Date = new Date(), zone?: string): AgendaGroup[] {
  const todayKey = dayKeyInZone(now, zone)
  const soonKey = addDaysToKey(todayKey, 7)

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
    if (day < todayKey) overdue.push(e)
    else if (day === todayKey) today.push(e)
    else if (day < soonKey) next7.push(e)
    else later.push(e)
  }
  const sortByDay = (a: CalendarEvent, b: CalendarEvent) => (a.start ?? '').localeCompare(b.start ?? '')
  const groups: AgendaGroup[] = []
  if (overdue.length) groups.push({ title: '已逾期', items: overdue.sort(sortByDay) })
  if (today.length) groups.push({ title: '今天', items: today.sort(sortByDay) })
  if (next7.length) groups.push({ title: '未来 7 天', items: next7.sort(sortByDay) })
  if (later.length) groups.push({ title: '之后', items: later.sort(sortByDay) })
  return groups
}

// ---------------------------------------------------------------------------
// Agenda fetch window (pure day keys — no browser-zone Date arithmetic)
// ---------------------------------------------------------------------------

/**
 * Fetch window for the agenda view, anchored at the Monday KEY of the current
 * week in the active zone. Returns calendar-day KEYS; callers convert them to
 * user-zone local-midnight instants via dayToInstant (never the browser zone),
 * so the server's AT TIME ZONE comparisons see exactly the intended days even
 * when the browser and the user zone disagree.
 */
export function agendaWindowKeys(weekStartKey: string): { fromKey: string; toKey: string } {
  return { fromKey: addDaysToKey(weekStartKey, -90), toKey: addDaysToKey(weekStartKey, 30) }
}
