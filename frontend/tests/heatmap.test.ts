// 投递热力图纯函数单测（docs/投递热力图方案.md §6）：补零的首尾与闰年、
// 连续天数的口径（今天为 0 不算断）、空档与空数据。
//
// 全部断言都用 YYYY-MM-DD 字符串，不经过 `Date`——被测函数本身也只做纯整数
// 日期运算，测试若用 new Date 反而会把本地时区偷偷带进来。
import { describe, expect, it } from 'vitest'
import {
  addDays,
  dayNumber,
  fillDays,
  formatDay,
  formatDayZh,
  heatmapStats,
  weekdayIndex,
} from '../src/features/analytics/heatmap'

describe('day arithmetic', () => {
  it('round-trips date strings without a local timezone', () => {
    expect(formatDay(dayNumber('2026-10-09'))).toBe('2026-10-09')
    expect(formatDay(dayNumber('2028-02-29'))).toBe('2028-02-29')
    expect(formatDay(dayNumber('1970-01-01'))).toBe('1970-01-01')
  })

  it('crosses month, year and leap boundaries', () => {
    expect(addDays('2026-10-09', 1)).toBe('2026-10-10')
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2027-01-01', -1)).toBe('2026-12-31')
    // 2028 是闰年：2 月有 29 天，28 号 + 1 天是 29 号而不是 3 月 1 号。
    expect(addDays('2028-02-28', 1)).toBe('2028-02-29')
    expect(addDays('2028-02-29', 1)).toBe('2028-03-01')
    expect(addDays('2027-02-28', 1)).toBe('2027-03-01')
  })

  it('names the weekday without a Date object', () => {
    // 2026-10-09 是周五；2026-10-11 是周日（0）。
    expect(formatDayZh('2026-10-09')).toBe('10月9日 周五')
    expect(weekdayIndex('2026-10-11')).toBe(0)
    expect(weekdayIndex('1970-01-01')).toBe(4) // Thursday
  })
})

describe('fillDays', () => {
  it('zero-fills a continuous range and keeps only real counts', () => {
    const out = fillDays(
      [
        { date: '2026-10-01', count: 2 },
        { date: '2026-10-03', count: 1 },
      ],
      '2026-10-01',
      '2026-10-04',
    )
    expect(out).toEqual([
      ['2026-10-01', 2],
      ['2026-10-02', 0],
      ['2026-10-03', 1],
      ['2026-10-04', 0],
    ])
  })

  it('covers both ends and a leap day', () => {
    const out = fillDays([], '2028-02-27', '2028-03-01')
    expect(out).toEqual([
      ['2028-02-27', 0],
      ['2028-02-28', 0],
      ['2028-02-29', 0],
      ['2028-03-01', 0],
    ])
    // 首尾同一天：一格，不是零格。
    expect(fillDays([], '2026-10-09', '2026-10-09')).toEqual([['2026-10-09', 0]])
  })

  it('returns nothing for an inverted or unparseable range', () => {
    expect(fillDays([], '2026-10-10', '2026-10-09')).toEqual([])
    expect(fillDays([], 'nope', '2026-10-09')).toEqual([])
  })

  it('sums duplicate dates instead of losing one', () => {
    expect(fillDays([{ date: '2026-10-01', count: 1 }, { date: '2026-10-01', count: 2 }], '2026-10-01', '2026-10-01')).toEqual([
      ['2026-10-01', 3],
    ])
  })
})

describe('heatmapStats', () => {
  it('is all zeros for empty data', () => {
    expect(heatmapStats([], '2026-10-09')).toEqual({
      total: 0,
      activeDays: 0,
      maxDay: 0,
      currentStreak: 0,
      longestStreak: 0,
    })
  })

  it('does not break the streak when today has none yet', () => {
    const days = [
      { date: '2026-10-06', count: 1 },
      { date: '2026-10-07', count: 3 },
      { date: '2026-10-08', count: 2 },
    ]
    // 今天（10-09）还没投：从昨天起算 → 连续 3 天，不算断。
    const s = heatmapStats(days, '2026-10-09')
    expect(s.currentStreak).toBe(3)
    expect(s.longestStreak).toBe(3)
    expect(s.total).toBe(6)
    expect(s.activeDays).toBe(3)
    expect(s.maxDay).toBe(3)
  })

  it('breaks the streak once yesterday is empty too', () => {
    const days = [
      { date: '2026-10-05', count: 1 },
      { date: '2026-10-06', count: 1 },
    ]
    // 今天（10-08）没有，昨天（10-07）也没有 → 连续天数为 0。
    expect(heatmapStats(days, '2026-10-08').currentStreak).toBe(0)
    // 今天 10-07 还没投 → 从 10-06 起算，回得到 10-05，连续 2 天。
    expect(heatmapStats(days, '2026-10-07').currentStreak).toBe(2)
    // 今天 10-06 有投 → 直接从今天起算。
    expect(heatmapStats(days, '2026-10-06').currentStreak).toBe(2)
  })

  it('counts a streak on a leap day', () => {
    const days = [
      { date: '2028-02-28', count: 1 },
      { date: '2028-02-29', count: 1 },
      { date: '2028-03-01', count: 1 },
    ]
    expect(heatmapStats(days, '2028-03-01').currentStreak).toBe(3)
  })

  it('picks the longest run across gaps, and maxDay ignores zero rows', () => {
    const days = [
      { date: '2026-01-01', count: 1 },
      { date: '2026-01-02', count: 1 },
      { date: '2026-01-03', count: 1 },
      { date: '2026-01-10', count: 1 },
      { date: '2026-01-11', count: 1 },
      { date: '2026-01-20', count: 9 },
      { date: '2026-01-21', count: 0 },
    ]
    const s = heatmapStats(days, '2026-01-21')
    expect(s.longestStreak).toBe(3)
    expect(s.maxDay).toBe(9)
    // count=0 的日期不算「活跃」。
    expect(s.activeDays).toBe(6)
    expect(s.total).toBe(14)
    // 今天（01-21）当天是 0，但昨天 01-20 有投 → 连续 1 天，不算断。
    expect(s.currentStreak).toBe(1)
  })

  it('ignores unparseable dates rather than corrupting the streak', () => {
    const s = heatmapStats([{ date: 'not-a-day', count: 5 }], '2026-10-09')
    expect(s).toEqual({ total: 0, activeDays: 0, maxDay: 0, currentStreak: 0, longestStreak: 0 })
  })
})
