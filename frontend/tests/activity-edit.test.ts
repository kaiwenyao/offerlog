// 活动轮次 / 待办 / 备注的编辑载荷（activityEdit.ts）。
//
// 用户反馈：面试时间填错一位、对方改期、OA 截止日写错，前端一个都改不了——
// 只能再排一轮，错的那轮永远留在日历、首页「即将到来的面试 / OA」和每天的提醒里。
// 接上后端 PATCH 之后，最关键的不是「发了请求」，而是**请求体不能顺手清掉别的
// 字段**：后端对这些实体的合并规则各不相同，漏一个就等于「改时间=清反馈」。
//
// 这个文件锁住三类规则：
//   1. 未编辑的字段原样回填（feedback / notes / duration / 精确截止时间 / 完成事实…）；
//   2. 时间字段的语义（面试与 OA 是挂墙时间→按用户时区换算；待办是日历日不换算）；
//   3. 清空必须显式表达（OA 的 clear_due_at），否则刷新后旧值又回来。
import { describe, expect, it, beforeEach } from 'vitest'
import {
  actionEditFields,
  assessmentEditFields,
  buildActionPatch,
  buildAssessmentPatch,
  buildInterviewPatch,
  buildNotePatch,
  interviewEditFields,
} from '../src/features/database/activityEdit'
import { setUserZone } from '../src/lib/tz'
import type { ActionItem, AssessmentRound, Interview } from '../src/lib/types'

function interview(over: Partial<Interview> = {}): Interview {
  return {
    id: 11,
    application_id: 7,
    round_name: '二面',
    format: 'video',
    scheduled_at: '2026-10-01T06:30:00.000Z',
    timezone: 'Asia/Shanghai',
    duration_minutes: 45,
    result: 'unknown',
    progress: 'preparing',
    invited_at: '2026-09-20T02:00:00.000Z',
    completed_at: null,
    completed_unknown: false,
    feedback: '聊得不错',
    notes: '记得带作品集',
    created_at: '2026-09-19T02:00:00.000Z',
    ...over,
  } as Interview
}

function assessment(over: Partial<AssessmentRound> = {}): AssessmentRound {
  return {
    id: 22,
    application_id: 7,
    kind: 'take_home',
    name: 'OA 2',
    progress: 'preparing',
    result: 'unknown',
    invited_at: '2026-09-20T02:00:00.000Z',
    planned_at: '2026-09-21T02:00:00.000Z',
    due_at: '2026-09-28T09:00:00.000Z',
    completed_at: null,
    completed_unknown: false,
    link: 'https://oa.test/1',
    notes: '限时 90 分钟',
    created_at: '2026-09-19T02:00:00.000Z',
    ...over,
  } as AssessmentRound
}

function action(over: Partial<ActionItem> = {}): ActionItem {
  return {
    id: 33,
    application_id: 7,
    title: '准备二面',
    due_date: '2026-10-01',
    due_ts: null,
    done_at: null,
    remind_me: true,
    remind_at: null,
    priority: 'high',
    created_at: '2026-09-19T02:00:00.000Z',
    ...over,
  } as ActionItem
}

beforeEach(() => {
  // 时区换算必须确定：把用户时区固定成 UTC，断言才不会随 CI 所在时区漂移。
  setUserZone('UTC')
})

describe('buildInterviewPatch', () => {
  it('回填所有后端不合并的字段（改了时间不该清掉反馈 / 备注 / 时长）', () => {
    const it0 = interview()
    const body = buildInterviewPatch(it0, { round_name: '三面', format: 'onsite', scheduled: '2026-10-05T14:30' })
    expect(body.round_name).toBe('三面')
    expect(body.format).toBe('onsite')
    expect(body.duration_minutes).toBe(45)
    expect(body.feedback).toBe('聊得不错')
    expect(body.notes).toBe('记得带作品集')
    expect(body.invited_at).toBe('2026-09-20T02:00:00.000Z')
    expect(body.progress).toBe('preparing')
    expect(body.result).toBe('unknown')
  })

  it('把 datetime-local 按用户时区换算成 UTC 瞬间并写回时区标签', () => {
    const body = buildInterviewPatch(interview(), { round_name: '二面', format: 'video', scheduled: '2026-10-05T14:30' })
    expect(body.scheduled_at).toBe('2026-10-05T14:30:00.000Z')
    expect(body.timezone).toBe('UTC')
  })

  it('时间留空 = 回到「时间未定」，同时保留原时区标签', () => {
    const body = buildInterviewPatch(interview(), { round_name: '二面', format: 'video', scheduled: '' })
    expect(body.scheduled_at).toBeNull()
    expect(body.timezone).toBe('Asia/Shanghai')
  })

  it('轮次名为空 / 时间格式非法时直接拒绝，不发请求', () => {
    expect(() => buildInterviewPatch(interview(), { round_name: '  ', format: 'video', scheduled: '' })).toThrow('轮次')
    expect(() => buildInterviewPatch(interview(), { round_name: '二面', format: 'video', scheduled: '2026/10/05' })).toThrow(
      '时间格式',
    )
  })

  it('interviewEditFields 把现有时间渲染成表单可显示的值（不留在空白）', () => {
    const f = interviewEditFields(interview())
    expect(f.round_name).toBe('二面')
    expect(f.scheduled).toBe('2026-10-01T06:30')
    expect(interviewEditFields(interview({ scheduled_at: null })).scheduled).toBe('')
  })
})

describe('buildAssessmentPatch', () => {
  it('回填后端不合并的字段（改了截止时间不该清掉链接 / 备注 / 类型 / 进度）', () => {
    const body = buildAssessmentPatch(assessment(), { name: 'OA 二轮', due: '2026-10-02T18:00', link: 'https://oa.test/2' })
    expect(body.name).toBe('OA 二轮')
    expect(body.kind).toBe('take_home')
    expect(body.progress).toBe('preparing')
    expect(body.result).toBe('unknown')
    expect(body.invited_at).toBe('2026-09-20T02:00:00.000Z')
    expect(body.planned_at).toBe('2026-09-21T02:00:00.000Z')
    expect(body.notes).toBe('限时 90 分钟')
    expect(body.due_at).toBe('2026-10-02T18:00:00.000Z')
    expect(body.clear_due_at).toBe(false)
  })

  it('清空截止时间要带 clear_due_at，否则后端会把旧值合并回来', () => {
    const body = buildAssessmentPatch(assessment(), { name: 'OA 2', due: '', link: '' })
    expect(body.due_at).toBeNull()
    expect(body.clear_due_at).toBe(true)
  })

  it('名称必填；assessmentEditFields 预填名称与截止时间', () => {
    expect(() => buildAssessmentPatch(assessment(), { name: ' ', due: '', link: '' })).toThrow('名称')
    const f = assessmentEditFields(assessment())
    expect(f.name).toBe('OA 2')
    expect(f.due).toBe('2026-09-28T09:00')
    expect(assessmentEditFields(assessment({ due_at: null })).due).toBe('')
  })
})

describe('buildActionPatch', () => {
  it('due_date 是日历日：原样发送，绝不做时区换算', () => {
    const body = buildActionPatch(action(), { title: '准备三面', due_date: '2026-11-03' })
    expect(body.due_date).toBe('2026-11-03')
    expect(body.title).toBe('准备三面')
  })

  it('回填整体覆盖的字段（改标题不该清掉精确截止时间 / 提醒 / 已完成事实）', () => {
    const a = action({ due_ts: '2026-10-01T02:00:00.000Z', done_at: '2026-09-30T02:00:00.000Z' })
    const body = buildActionPatch(a, { title: '改个标题', due_date: '2026-10-01' })
    // 截止日没动 → 精确时间原样保留。
    expect(body.due_ts).toBe('2026-10-01T02:00:00.000Z')
    expect(body.done_at).toBe('2026-09-30T02:00:00.000Z')
    expect(body.remind_me).toBe(true)
    expect(body.priority).toBe('high')
  })

  it('remind_at 原样回填（整体覆盖，漏了就会被写成 NULL）', () => {
    const a = action({ remind_at: '2026-09-30T22:00:00.000Z' })
    expect(buildActionPatch(a, { title: 'x', due_date: '2026-10-01' }).remind_at).toBe('2026-09-30T22:00:00.000Z')
  })

  // 老实例的待办普遍带着迁移 00002 回填的 due_ts（它优先于 due_date 被所有读取端
  // 使用：详情、日历、首页、提醒）。只改 due_date 而把旧 due_ts 送回去 = 改了没反应。
  describe('改了截止日就必须让日历日成为唯一真相', () => {
    it('日期变了 → 清掉 due_ts（否则显示 / 日历 / 提醒纹丝不动）', () => {
      const a = action({ due_ts: '2026-10-01T02:00:00.000Z', due_date: '2026-10-01' })
      const body = buildActionPatch(a, { title: '准备二面', due_date: '2026-10-09' })
      expect(body.due_date).toBe('2026-10-09')
      expect(body.due_ts).toBeNull()
    })

    it('日期清空 → due_ts 也清掉（没有截止就是真的没有）', () => {
      const a = action({ due_ts: '2026-10-01T02:00:00.000Z', due_date: '2026-10-01' })
      const body = buildActionPatch(a, { title: '准备二面', due_date: '' })
      expect(body.due_date).toBeNull()
      expect(body.due_ts).toBeNull()
    })

    it('日期没变 → due_ts 保留（改标题不该降级成只有日历日）', () => {
      const a = action({ due_ts: '2026-10-01T02:00:00.000Z', due_date: '2026-10-01' })
      expect(buildActionPatch(a, { title: '改标题', due_date: '2026-10-01' }).due_ts).toBe('2026-10-01T02:00:00.000Z')
      // 只有 due_ts、没有日历日的待办（同日再次保存）也不该被顺手清掉。
      const tsOnly = action({ due_ts: '2026-10-01T02:00:00.000Z', due_date: null })
      expect(buildActionPatch(tsOnly, { title: '改标题', due_date: '' }).due_ts).toBe('2026-10-01T02:00:00.000Z')
    })
  })

  it('截止日留空 = 没有截止日期；格式非法直接拒绝', () => {
    expect(buildActionPatch(action(), { title: 'x', due_date: '' }).due_date).toBeNull()
    expect(() => buildActionPatch(action(), { title: 'x', due_date: '2026/11/03' })).toThrow('YYYY-MM-DD')
    expect(() => buildActionPatch(action(), { title: '  ', due_date: '' })).toThrow('待办')
  })

  it('actionEditFields 预填标题与日历日截止', () => {
    expect(actionEditFields(action())).toEqual({ title: '准备二面', due_date: '2026-10-01' })
    expect(actionEditFields(action({ due_date: null })).due_date).toBe('')
  })
})

describe('buildNotePatch', () => {
  it('trim 后发送正文；空白内容没有意义，直接拒绝', () => {
    expect(buildNotePatch('  已改好的备注  ')).toEqual({ content_md: '已改好的备注' })
    expect(() => buildNotePatch('   ')).toThrow('不能为空')
  })
})
