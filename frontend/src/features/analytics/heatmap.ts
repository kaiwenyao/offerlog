// 投递热力图的纯函数层（docs/投递热力图方案.md §4.2）——不碰 React，也不碰
// `Date` 的本地时区。
//
// 日期一律当 `YYYY-MM-DD` 字符串/整日序数处理：`new Date('2026-10-09')` 会被
// 解析成 UTC 午夜，再用 `getDate()` 之类的本地取值就可能差一天（西半球浏览器
// 直接退到 10-08）。「今天」也由服务端按用户时区算好传进来（`today`），所以
// 连续天数的口径不随浏览器时区漂移。

export interface HeatmapDay {
  date: string
  count: number
}

export interface HeatmapStats {
  total: number
  activeDays: number
  maxDay: number
  currentStreak: number
  longestStreak: number
}

/** 单日投递量的固定档位（§4.1）：不按最大值动态分档，年份之间才能直接比较。 */
export const HEATMAP_PIECES = [
  { min: 0, max: 0, label: '0' },
  { min: 1, max: 1, label: '1' },
  { min: 2, max: 3, label: '2–3' },
  { min: 4, max: 6, label: '4–6' },
  { min: 7, max: null, label: '7+' },
] as const

const DATE_RE = /^(\d{4})-(\d{2})-(\d{2})$/

/**
 * Days since 1970-01-01 for a civil date (Howard Hinnant's algorithm). Pure
 * integer math on purpose: any `Date` round-trip would drag the local zone in.
 */
function daysFromCivil(y: number, m: number, d: number): number {
  const yy = y - (m <= 2 ? 1 : 0)
  const era = Math.floor(yy / 400)
  const yoe = yy - era * 400
  const doy = Math.floor((153 * (m + (m > 2 ? -3 : 9)) + 2) / 5) + d - 1
  const doe = yoe * 365 + Math.floor(yoe / 4) - Math.floor(yoe / 100) + doy
  return era * 146097 + doe - 719468
}

/** Inverse of daysFromCivil. */
function civilFromDays(z: number): [number, number, number] {
  const zz = z + 719468
  const era = Math.floor(zz / 146097)
  const doe = zz - era * 146097
  const yoe = Math.floor((doe - Math.floor(doe / 1460) + Math.floor(doe / 36524) - Math.floor(doe / 146096)) / 365)
  const y = yoe + era * 400
  const doy = doe - (365 * yoe + Math.floor(yoe / 4) - Math.floor(yoe / 100))
  const mp = Math.floor((5 * doy + 2) / 153)
  const d = doy - Math.floor((153 * mp + 2) / 5) + 1
  const m = mp + (mp < 10 ? 3 : -9)
  return [y + (m <= 2 ? 1 : 0), m, d]
}

/** Day ordinal of a YYYY-MM-DD string; NaN when it is not a calendar date. */
export function dayNumber(date: string): number {
  const m = DATE_RE.exec(date)
  if (!m) return NaN
  return daysFromCivil(Number(m[1]), Number(m[2]), Number(m[3]))
}

/** YYYY-MM-DD for a day ordinal. */
export function formatDay(n: number): string {
  const [y, m, d] = civilFromDays(n)
  return `${String(y).padStart(4, '0')}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`
}

/** date + n calendar days, as a YYYY-MM-DD string. */
export function addDays(date: string, n: number): string {
  return formatDay(dayNumber(date) + n)
}

/** 0 = 周日 … 6 = 周六. 1970-01-01 was a Thursday. */
export function weekdayIndex(date: string): number {
  return (((dayNumber(date) + 4) % 7) + 7) % 7
}

const WEEKDAY_ZH = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

/** 「10月9日 周五」— the tooltip headline, built from strings only. */
export function formatDayZh(date: string): string {
  const m = DATE_RE.exec(date)
  if (!m) return date
  return `${Number(m[2])}月${Number(m[3])}日 ${WEEKDAY_ZH[weekdayIndex(date)]}`
}

/**
 * 把稀疏的 days 补成 [from, to] 的连续序列，供 echarts calendar 使用；
 * 同一日期重复出现时求和（后端已 GROUP BY，这里只是防御）。
 */
export function fillDays(days: HeatmapDay[], from: string, to: string): Array<[string, number]> {
  const fromN = dayNumber(from)
  const toN = dayNumber(to)
  if (!Number.isFinite(fromN) || !Number.isFinite(toN) || toN < fromN) return []
  const counts = new Map<string, number>()
  for (const d of days) {
    if (!Number.isFinite(dayNumber(d.date))) continue
    counts.set(d.date, (counts.get(d.date) ?? 0) + d.count)
  }
  const out: Array<[string, number]> = []
  for (let n = fromN; n <= toN; n++) {
    const key = formatDay(n)
    out.push([key, counts.get(key) ?? 0])
  }
  return out
}

/**
 * 汇总统计（§4.2）。
 *
 * 当前连续天数的规则：今天没有投递时从昨天开始往前数——今天还没过完，
 * 不能算作断了（否则每天早上打开页面都会看到「连续 0 天」）。
 */
export function heatmapStats(days: HeatmapDay[], today: string): HeatmapStats {
  const counts = new Map<string, number>()
  for (const d of days) {
    if (!d.count || !Number.isFinite(dayNumber(d.date))) continue
    counts.set(d.date, (counts.get(d.date) ?? 0) + d.count)
  }

  let total = 0
  let maxDay = 0
  for (const n of counts.values()) {
    total += n
    if (n > maxDay) maxDay = n
  }

  const ordinals = [...counts.keys()].map(dayNumber).sort((a, b) => a - b)
  let longestStreak = 0
  let run = 0
  let prev: number | null = null
  for (const n of ordinals) {
    run = prev !== null && n === prev + 1 ? run + 1 : 1
    if (run > longestStreak) longestStreak = run
    prev = n
  }

  let currentStreak = 0
  if (counts.size > 0) {
    let cursor = dayNumber(today)
    if (!counts.has(formatDay(cursor))) cursor -= 1
    while (counts.has(formatDay(cursor))) {
      currentStreak++
      cursor--
    }
  }

  return { total, activeDays: counts.size, maxDay, currentStreak, longestStreak }
}
