// 这一轮修的都是「同一件事在两个页面上说法不一样」的 bug，能提成纯函数的都在
// 这里钉住语义。其余修复（抽屉里取消归档不再把人弹走、视图/布局写回 URL、
// 弹窗焦点与背景滚动锁）发生在组件的交互路径上，由 Playwright e2e 覆盖。
import { describe, expect, it } from 'vitest'
import {
  DEFAULT_WEEK_START,
  monthGrid,
  normalizeWeekStart,
  weekdayLabels,
  weekStartKeyOf,
} from '../src/features/calendar/grid'
import { importPreviewCopy } from '../src/features/settings/importCopy'
import { isUpcoming } from '../src/features/database/timeline'

describe('每周起始日：日历跟着账号偏好走', () => {
  // 原始 bug：设置页的「每周起始日」只对首页生效，日历周视图与数据库页的
  // 「本周面试」写死周一，于是把起始日改成周日之后，两处「本周」差一天。
  // 2026-09-23 是周三。
  const wednesday = '2026-09-23'

  it('周一起始（默认）落在 09-21', () => {
    expect(weekStartKeyOf(wednesday, 1)).toBe('2026-09-21')
  })

  it('周日起始落在 09-20', () => {
    expect(weekStartKeyOf(wednesday, 0)).toBe('2026-09-20')
  })

  it('周六起始落在 09-19', () => {
    expect(weekStartKeyOf(wednesday, 6)).toBe('2026-09-19')
  })

  it('本来就是起始日的那天不往前挪', () => {
    expect(weekStartKeyOf('2026-09-21', 1)).toBe('2026-09-21')
    expect(weekStartKeyOf('2026-09-20', 0)).toBe('2026-09-20')
  })

  it('坏值退回周一，而不是把网格算飞', () => {
    expect(normalizeWeekStart(undefined)).toBe(DEFAULT_WEEK_START)
    expect(normalizeWeekStart(9)).toBe(DEFAULT_WEEK_START)
    expect(normalizeWeekStart(-1)).toBe(DEFAULT_WEEK_START)
    expect(normalizeWeekStart(1.5)).toBe(DEFAULT_WEEK_START)
    expect(weekStartKeyOf(wednesday, 42)).toBe('2026-09-21')
  })

  it('表头标签跟着起始日轮转，和网格同一个顺序', () => {
    expect(weekdayLabels(1)[0]).toBe('周一')
    expect(weekdayLabels(0)[0]).toBe('周日')
    expect(weekdayLabels(0)[6]).toBe('周六')
    expect(weekdayLabels(6)).toEqual(['周六', '周日', '周一', '周二', '周三', '周四', '周五'])
    expect(weekdayLabels(0)).toHaveLength(7)
  })

  it('月视图网格的第一格就是该月 1 号所在周的起始日', () => {
    // 2026-10-01 是周四。
    const mon = monthGrid([], '2026-10-01', undefined, 1)
    expect(mon[0][0].key).toBe('2026-09-28') // 周一
    const sun = monthGrid([], '2026-10-01', undefined, 0)
    expect(sun[0][0].key).toBe('2026-09-27') // 周日
    // 6 周 × 7 天，永远盖得住整月。
    expect(sun).toHaveLength(6)
    expect(sun[5]).toHaveLength(7)
    expect(sun[0][4].inMonth).toBe(true) // 10-01
  })
})

describe('isUpcoming：未来的事件要能被标出来', () => {
  const now = new Date('2026-09-20T12:00:00Z')

  it('填在未来的时间 = 待发生', () => {
    expect(isUpcoming('2026-12-25T02:00:00Z', now)).toBe(true)
  })

  it('已经发生的不标', () => {
    expect(isUpcoming('2026-09-20T11:59:00Z', now)).toBe(false)
  })

  it('时间未定不算「待发生」——它照常决定当前状态（面板也把它画在最后）', () => {
    expect(isUpcoming(null, now)).toBe(false)
    expect(isUpcoming('', now)).toBe(false)
  })

  it('解析不了的时间不乱标', () => {
    expect(isUpcoming('不是时间', now)).toBe(false)
  })
})

describe('importPreviewCopy：重复候选必须说出后果', () => {
  // 原始 bug：预检只在文件内部两两比较，把自己导出的 CSV 原样导回去会报
  // 「疑似重复 0 行」，确认之后每个岗位都变成两条。现在查库了，文案也得把
  // 「重复项默认新建」的后果讲清楚，而不是只报一个数字。
  it('没有重复时不啰嗦', () => {
    const msg = importPreviewCopy(10, 10, 0, 0)
    expect(msg).toContain('共 10 行')
    expect(msg).toContain('疑似重复 0 行')
    expect(msg).not.toContain('新记录')
  })

  it('有重复时说明会再建 N 条新记录', () => {
    const msg = importPreviewCopy(10, 10, 0, 3)
    expect(msg).toContain('疑似重复 3 行')
    expect(msg).toContain('不会覆盖')
    expect(msg).toContain('再建 3 条新记录')
  })

  it('有错误行时才提修正 CSV', () => {
    expect(importPreviewCopy(10, 8, 2, 0)).toContain('跳过')
    expect(importPreviewCopy(10, 10, 0, 0)).not.toContain('跳过')
  })
})
