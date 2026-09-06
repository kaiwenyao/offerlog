import type { ActionItem, AppRow } from '../../lib/types'

export interface TodoItem extends ActionItem {
  company_name?: string
  position?: string
  status?: string
}

export interface TodoGroup {
  title: string
  dot: string
  items: TodoItem[]
}

const DAY_MS = 86_400_000

export function startOfDay(d: Date = new Date()): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

export function dueTime(item: Pick<TodoItem, 'due_ts' | 'due_date'>): number {
  const iso = item.due_ts ?? item.due_date
  return iso ? new Date(iso).getTime() : Number.POSITIVE_INFINITY
}

/** Split open work into the design's 已逾期 / 今天 / 未来 7 天 / 更晚 buckets. */
export function groupActions(actions: TodoItem[]): TodoGroup[] {
  const today = startOfDay()
  const weekEnd = today + 7 * DAY_MS
  const groups: TodoGroup[] = [
    { title: '已逾期', dot: '#d94a5a', items: [] },
    { title: '今天', dot: '#8b5cf6', items: [] },
    { title: '未来 7 天', dot: '#4a7fd9', items: [] },
    { title: '更晚', dot: '#8b8b99', items: [] },
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
  info: { bg: '#cfdcf7', fg: '#20365e' },
  warn: { bg: '#f7e3c4', fg: '#6b4410' },
  good: { bg: '#c9ecdb', fg: '#1c5340' },
  acc: { bg: '#e0d4ff', fg: '#3f2a7a' },
  bad: { bg: '#f7cfd4', fg: '#6e1f28' },
}

function toneForStatus(status: string, overdue: boolean): WeekDay['items'][number]['tone'] {
  if (overdue) return 'bad'
  switch (status) {
    case 'offer':
    case 'accepted':
      return 'good'
    case 'assessment':
      return 'warn'
    case 'interviewing':
      return 'acc'
    default:
      return 'info'
  }
}

/** The Monday→Sunday strip around today, filled from real deadlines. */
export function buildWeek(rows: AppRow[], now: Date = new Date()): WeekDay[] {
  const todayStart = startOfDay(now)
  // JS weeks start on Sunday; the design starts on Monday.
  const offsetToMonday = (now.getDay() + 6) % 7
  const monday = todayStart - offsetToMonday * DAY_MS

  const days: WeekDay[] = WEEKDAYS.map((weekday, i) => {
    const ts = monday + i * DAY_MS
    return { weekday, dayNum: new Date(ts).getDate(), isToday: ts === todayStart, items: [] }
  })

  for (const row of rows) {
    const iso = row.next_action_due_at ?? row.deadline
    if (!iso) continue
    const at = startOfDay(new Date(iso))
    const idx = Math.round((at - monday) / DAY_MS)
    if (idx < 0 || idx > 6) continue
    days[idx].items.push({
      kind: row.next_action ? '待办' : '截止',
      who: row.company_name,
      tone: toneForStatus(row.status, at < todayStart),
    })
  }
  return days
}

export interface Kpis {
  inProgress: number
  submittedThisWeek: number
  awaitingReply: number
  overdue: number
}

const IN_PROGRESS = new Set(['applied', 'screening', 'assessment', 'interviewing'])

export function buildKpis(rows: AppRow[], now: Date = new Date()): Kpis {
  const todayStart = startOfDay(now)
  const weekStart = todayStart - ((now.getDay() + 6) % 7) * DAY_MS
  let inProgress = 0
  let submittedThisWeek = 0
  let awaitingReply = 0
  let overdue = 0
  for (const r of rows) {
    if (r.archived || r.deleted) continue
    if (IN_PROGRESS.has(r.status)) inProgress += 1
    if (r.submitted_at && new Date(r.submitted_at).getTime() >= weekStart) submittedThisWeek += 1
    if (r.submitted_at && !r.first_response_at && IN_PROGRESS.has(r.status)) awaitingReply += 1
    if (r.next_action && r.next_action_due_at && new Date(r.next_action_due_at).getTime() < todayStart) overdue += 1
  }
  return { inProgress, submittedThisWeek, awaitingReply, overdue }
}
