// 这一轮修的三个「文案/控件与实际行为对不上」的 bug，用纯函数钉住语义。
//
// 其余修复（抽屉 Esc、弹窗 ✕、整页详情的更多操作菜单、CSV 重复提交、桑基图
// 失败态、移动端侧栏）都发生在组件的交互路径上，由 Playwright e2e 覆盖。
import { describe, expect, it } from 'vitest'
import { assessmentTiming } from '../src/features/database/tabs'
import { stepLabels } from '../src/features/calendar/grid'

describe('assessmentTiming: 截止时间只说一次', () => {
  // 原始 bug：「只填了截止时间、还没开做」的 OA（最常见的一种）会先在主时间位
  // 渲染一遍「截止 X」，后面再追加一遍「· 截止 X」，同一行里出现两个同样的时间。
  const due = '2026-09-25T10:00:00Z'

  it('只有截止时间时，截止只出现一次', () => {
    const text = assessmentTiming({
      completed_at: null,
      planned_at: null,
      due_at: due,
      progress: 'preparing',
    })
    expect(text.match(/截止/g)).toHaveLength(1)
    expect(text.startsWith('截止 ')).toBe(true)
  })

  it('有计划时间时，计划在前、截止在后，各一次', () => {
    const text = assessmentTiming({
      completed_at: null,
      planned_at: '2026-09-24T09:00:00Z',
      due_at: due,
      progress: 'preparing',
    })
    expect(text.match(/计划/g)).toHaveLength(1)
    expect(text.match(/截止/g)).toHaveLength(1)
    expect(text.indexOf('计划')).toBeLessThan(text.indexOf('截止'))
  })

  it('已完成后不再重复截止时间', () => {
    const text = assessmentTiming({
      completed_at: '2026-09-24T20:00:00Z',
      planned_at: null,
      due_at: due,
      progress: 'completed',
    })
    expect(text).toContain('完成于')
    expect(text).not.toContain('截止')
  })

  it('已完成但完成时间不详时，截止仍然显示（否则这行没有任何时间信息）', () => {
    const text = assessmentTiming({
      completed_at: null,
      planned_at: null,
      due_at: due,
      progress: 'completed',
    })
    expect(text.match(/截止/g)).toHaveLength(1)
  })

  it('四种时间全空时说「时间未定」，不伪造时间', () => {
    expect(
      assessmentTiming({ completed_at: null, planned_at: null, due_at: null, progress: 'preparing' }),
    ).toBe('时间未定')
  })
})

describe('stepLabels: 翻页按钮的文案跟着视图走', () => {
  // 原始 bug：两个按钮写死「上一周 / 下一周」。月视图点一下翻的是一整个月，
  // 议程视图压根没有翻页分支——三个按钮亮着，点了什么都不发生。
  it('周视图说「周」', () => {
    expect(stepLabels('week')).toEqual({ prev: '上一周', next: '下一周' })
  })

  it('月视图说「月」，不能再说「周」', () => {
    expect(stepLabels('month')).toEqual({ prev: '上个月', next: '下个月' })
  })

  it('议程视图没有可翻的上下页，返回 null（不画按钮）', () => {
    expect(stepLabels('agenda')).toBeNull()
  })
})
