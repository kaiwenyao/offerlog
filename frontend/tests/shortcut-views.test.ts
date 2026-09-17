// 侧栏快捷视图的筛选语义（views.ts 的 shortcutConditions）。
//
// 原始 bug：三个名字与结果全对不上——「本周面试」查的是全部进行中岗位、
// 「待跟进」就是全部机会、「已归档」筛的是「已结束状态」，于是归档一个进行中的
// 岗位后，它在「已归档」里反而找不到。这里把三条语义钉死，防止再漂移。
import { describe, expect, it } from 'vitest'
import {
  ARCHIVED_VIEW,
  BUILTIN,
  FOLLOW_UP_VIEW,
  SHORTCUT_VIEWS,
  WEEK_INTERVIEWS_VIEW,
  boardBuckets,
  buildFilters,
  isShortcutView,
  pruneSelection,
  shortcutConditions,
  statusesForView,
  trashQueryPath,
} from '../src/features/database/views'

const ctx = { today: '2026-09-17', weekInterviewIds: [11, 42] }

describe('shortcut views are wired to the sidebar ids', () => {
  it('exposes exactly the three sidebar shortcuts', () => {
    expect(SHORTCUT_VIEWS.map((v) => [v.id, v.name])).toEqual([
      [WEEK_INTERVIEWS_VIEW, '本周面试'],
      [FOLLOW_UP_VIEW, '待跟进'],
      [ARCHIVED_VIEW, '已归档'],
    ])
  })

  it('does not collide with the built-in status views', () => {
    const builtinIds = new Set(BUILTIN.map((v) => v.id))
    for (const v of SHORTCUT_VIEWS) expect(builtinIds.has(v.id)).toBe(false)
  })

  it('isShortcutView distinguishes filtered shortcuts from the built-in lists', () => {
    // 用于决定空列表时要不要给「＋ 新增岗位」：快捷视图都有筛选，新建的岗位
    // 不会出现在里面。
    for (const v of SHORTCUT_VIEWS) expect(isShortcutView(v.id)).toBe(true)
    for (const v of BUILTIN) expect(isShortcutView(v.id)).toBe(false)
    expect(isShortcutView(0)).toBe(false)
  })
})

describe('本周面试', () => {
  it('filters by the week interview application ids, and excludes archived rows', () => {
    expect(shortcutConditions(WEEK_INTERVIEWS_VIEW, ctx)).toEqual([
      { field: 'id', op: 'in', value: [11, 42] },
      { field: 'archived', op: 'eq', value: false },
    ])
  })

  it('matches nothing (empty id set) when the week has no interview yet', () => {
    expect(shortcutConditions(WEEK_INTERVIEWS_VIEW, { today: ctx.today })).toEqual([
      { field: 'id', op: 'in', value: [] },
      { field: 'archived', op: 'eq', value: false },
    ])
  })
})

describe('待跟进', () => {
  it('is 进行中 + 未归档 + (没有任何未完成待办 或 最早的未完成待办已到期)', () => {
    const conds = shortcutConditions(FOLLOW_UP_VIEW, ctx)!
    expect(conds[0]).toMatchObject({ op: 'or' })
    expect(conds[1]).toEqual({ field: 'archived', op: 'eq', value: false })
    expect(conds[2]).toEqual({
      op: 'or',
      conditions: [
        { field: 'open_todo_count', op: 'eq', value: 0 },
        { field: 'next_open_todo_due', op: 'lte', value: '2026-09-17' },
      ],
    })
  })

  it('derives 「有没有下一步」 from the authoritative todo fields, never the next_action mirror', () => {
    // 回归（PR #42 review P2）：镜像会在「完成最后一个待办」时被清空、在 reopen 时
    // 不复活、也会因 best-effort 同步失败而过期。只看镜像会把「已排了未来一步」的
    // 岗位算成待跟进，或让已完成的待办看起来像安排。
    const json = JSON.stringify(shortcutConditions(FOLLOW_UP_VIEW, ctx))
    expect(json).not.toContain('next_action')
  })

  it('anchors the overdue comparison on the given today (user zone), not a fixed date', () => {
    const conds = shortcutConditions(FOLLOW_UP_VIEW, { today: '2026-01-02' })!
    const group = conds[2] as { conditions: Array<{ value?: unknown }> }
    expect(group.conditions[1].value).toBe('2026-01-02')
  })

  it('never uses is_not_empty on a DATE field (it compiles to `<> \'\'` and Postgres rejects it)', () => {
    const json = JSON.stringify(shortcutConditions(FOLLOW_UP_VIEW, ctx))
    expect(json).not.toContain('is_not_empty')
  })

  it('only covers in-progress stages, so 已结束 / 收到 Offer rows never appear', () => {
    expect(statusesForView(FOLLOW_UP_VIEW)).toEqual(['applied', 'screening', 'assessment', 'interviewing'])
    const bucketStatuses = boardBuckets(FOLLOW_UP_VIEW).flatMap((b) => b.statuses)
    expect(bucketStatuses).not.toContain('accepted')
    expect(bucketStatuses).not.toContain('offer')
    expect(bucketStatuses).toContain('interviewing')
  })
})

describe('已归档', () => {
  it('uses the archive flag, not the ended status set', () => {
    expect(shortcutConditions(ARCHIVED_VIEW, ctx)).toEqual([{ field: 'archived', op: 'eq', value: true }])
    // 一个进行中的岗位归档后必须能被筛出来：这里根本不该出现 status 条件。
    expect(JSON.stringify(shortcutConditions(ARCHIVED_VIEW, ctx))).not.toContain('status')
  })
})

describe('buildFilters with shortcut views', () => {
  it('replaces the (empty) filter_ast with the live shortcut conditions', () => {
    const view = SHORTCUT_VIEWS.find((v) => v.id === ARCHIVED_VIEW)
    expect(buildFilters(view, '', [], ctx)).toEqual([{ field: 'archived', op: 'eq', value: true }])
  })

  it('keeps search and quick-filter chips after the shortcut conditions', () => {
    const view = SHORTCUT_VIEWS.find((v) => v.id === ARCHIVED_VIEW)
    const chip = { field: 'priority', op: 'eq', value: 'high' }
    const filters = buildFilters(view, '字节', [chip], ctx)
    expect(filters).toHaveLength(3)
    expect(filters[0]).toEqual({ field: 'archived', op: 'eq', value: true })
    expect(filters[1]).toMatchObject({ op: 'or' })
    expect(filters[2]).toEqual(chip)
  })

  it('leaves ordinary views untouched (no shortcut conditions leak in)', () => {
    const saved = BUILTIN.find((v) => v.id === -3)!
    expect(buildFilters(saved, '', [], ctx)).toEqual([saved.filter_ast])
  })
})

// 回收站翻页：写死 page=1 时点「下一页」只改页码不换数据，第 60 条之后的删除记录
// 永远翻不到，也就没法恢复。
describe('trashQueryPath', () => {
  it('carries the requested page instead of hardcoding page 1', () => {
    expect(trashQueryPath(2, 60)).toContain('page=2')
    expect(trashQueryPath(2, 60)).not.toContain('page=1')
    expect(trashQueryPath(3, 60)).toBe('/api/v1/applications?trash=1&page=3&page_size=60&include=stage_history')
  })

  it('always asks for the trash view with stage history', () => {
    const path = trashQueryPath(1, 60)
    expect(path).toContain('trash=1')
    expect(path).toContain('include=stage_history')
  })

  it('falls back to page 1 for missing or nonsensical pages', () => {
    expect(trashQueryPath(0, 60)).toContain('page=1')
    expect(trashQueryPath(-3, 60)).toContain('page=1')
    expect(trashQueryPath(Number.NaN, 60)).toContain('page=1')
  })
})

// 批量选择的残留：第 1 页勾 10 条 → 翻到第 2 页，顶部还写着「已选 10 条」，
// 点「应用」改的是屏幕上看不见的行（回收站里更是真的改了已删除记录，而列表上
// 什么变化都看不到）。pruneSelection 把选择集收敛到当前查询结果上。
describe('pruneSelection', () => {
  const page1 = [{ id: 1 }, { id: 2 }, { id: 3 }]
  const page2 = [{ id: 4 }, { id: 5 }]

  it('keeps only ids present in the current result set', () => {
    const selected = new Set([1, 2, 4])
    expect([...pruneSelection(selected, page1)].sort()).toEqual([1, 2])
    expect([...pruneSelection(selected, page2)]).toEqual([4])
  })

  it('empties the selection after paging away (no invisible rows get bulk-applied)', () => {
    const selected = new Set([1, 2, 3])
    expect(pruneSelection(selected, page2).size).toBe(0)
    expect(pruneSelection(selected, []).size).toBe(0)
  })

  it('returns a fresh set and never mutates the input', () => {
    const selected = new Set([1, 9])
    const out = pruneSelection(selected, page1)
    expect(out).not.toBe(selected)
    expect([...selected]).toEqual([1, 9])
  })
})
