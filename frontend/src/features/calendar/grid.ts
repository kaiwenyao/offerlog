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

/** Weekday names indexed the way JS does it: 0 = 周日 … 6 = 周六. */
export const WEEKDAY_NAMES = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

/** Monday-first labels — the default grid header (week_start = 1). */
export const WEEKDAYS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

/**
 * 每周起始日偏好（0 = 周日 … 6 = 周六）。设置页把它存在服务端，首页的「本周工序」
 * 一直是按它排的；日历与「本周面试」以前写死周一，于是把起始日改成周日之后，
 * 首页的 7 列和日历的 7 列差了一天。
 */
export const DEFAULT_WEEK_START = 1

/** Clamp an arbitrary stored value into 0..6, falling back to Monday. */
export function normalizeWeekStart(v: number | null | undefined): number {
  return typeof v === 'number' && Number.isInteger(v) && v >= 0 && v <= 6 ? v : DEFAULT_WEEK_START
}

/** The 7 weekday labels in grid order for the given week start. */
export function weekdayLabels(weekStart: number = DEFAULT_WEEK_START): string[] {
  const start = normalizeWeekStart(weekStart)
  return Array.from({ length: 7 }, (_, i) => WEEKDAY_NAMES[(start + i) % 7])
}

/** JS weekday (0 = Sunday) of a YYYY-MM-DD key. */
export function weekdayOf(key: string): number {
  const [y, m, d] = key.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d)).getUTCDay()
}

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

/** The key of the week's first day for the given week start (0=周日..6=周六). */
export function weekStartKeyOf(key: string, weekStart: number = DEFAULT_WEEK_START): string {
  const start = normalizeWeekStart(weekStart)
  const offset = (weekdayOf(key) - start + 7) % 7
  return addDaysToKey(key, -offset)
}

/** The Monday key of the week containing key (weekStartKeyOf with 周一 start). */
export function mondayKeyOf(key: string): string {
  return weekStartKeyOf(key, 1)
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

/** 7 columns starting at weekStartKey (the week's first day in the user zone). */
export function weekColumns(events: CalendarEvent[], weekStartKey: string, zone?: string): DayCell[] {
  const today = todayKeyInZone(zone)
  return WEEKDAYS.map((_, i) => {
    const key = addDaysToKey(weekStartKey, i)
    const dayEvents = events.filter((e) => eventDay(e, zone) === key)
    return { key, isToday: key === today, events: dayEvents }
  })
}

/** 月视图单元格里直接可见的日程条数——再多就折进「+N」。 */
export const MONTH_CELL_LIMIT = 3

/**
 * 拆分一天的全部日程：可见的前 N 条 + 折起来的剩余。
 *
 * 折起来的部分必须能展开（单元格里的「+N」是可点的）：只藏起来不给入口的话，
 * 用户只能在月视图之外重新定位到那一天，才看得到第 4 条以后的日程。
 * 顺序保持月视图的排序（按时间），展开面板与网格里看到的完全一致。
 */
export function splitMonthCell(
  events: CalendarEvent[],
  limit: number = MONTH_CELL_LIMIT,
): { shown: CalendarEvent[]; hidden: CalendarEvent[] } {
  const n = Math.max(0, limit)
  return { shown: events.slice(0, n), hidden: events.slice(n) }
}

/** 6-week month grid starting at the week start on/before monthKey's 1st. */
export function monthGrid(
  events: CalendarEvent[],
  monthKey: string,
  zone?: string,
  weekStart: number = DEFAULT_WEEK_START,
): DayCell[][] {
  const firstKey = monthKeyOf(monthKey)
  const gridStartKey = weekStartKeyOf(firstKey, weekStart)
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

// ---------------------------------------------------------------------------
// Prev / next stepper
// ---------------------------------------------------------------------------

/**
 * 「上一步 / 下一步」按钮的文案，null = 这个视图没有可翻的上下页。
 *
 * 原来两个按钮写死成「上一周 / 下一周」，对另外两个视图都是错的：月视图点一下
 * 翻的是**一整个月**（文案对不上行为），议程视图的窗口固定锚在本周（−90d…+30d），
 * 翻页处理函数对它压根没有分支——三个按钮亮着，点了什么都不发生。文案跟着视图
 * 走，没有动作的视图就不画这排按钮。
 */
export function stepLabels(view: ViewMode): { prev: string; next: string } | null {
  switch (view) {
    case 'week':
      return { prev: '上一周', next: '下一周' }
    case 'month':
      return { prev: '上个月', next: '下个月' }
    default:
      return null
  }
}
