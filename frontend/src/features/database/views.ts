import type { SavedView } from '../../lib/types'

export interface FilterCond {
  field: string
  op: string
  value: unknown
}

export interface FilterGroup {
  op: 'and' | 'or'
  conditions: (FilterCond | FilterGroup)[]
}

/** A filter tree node: either a leaf condition or a nested group. */
export type FilterNode = FilterCond | FilterGroup

export type Layout = 'table' | 'board' | 'list'

/** Status sets backing the built-in views (ids are negative by convention). */
export const BUILTIN_STATUSES: Record<number, string[]> = {
  [-2]: ['saved', 'preparing'],
  [-3]: ['applied', 'screening', 'assessment', 'interviewing'],
  [-4]: ['offer', 'accepted'],
  [-5]: ['accepted', 'rejected', 'withdrawn', 'closed'],
}

function statusClause(statuses: string[]): FilterGroup {
  return { op: 'or', conditions: statuses.map((s) => ({ field: 'status', op: 'eq', value: s })) }
}

function builtin(id: number, name: string, layout: Layout): SavedView {
  return {
    id,
    name,
    layout,
    columns: null,
    filter_ast: BUILTIN_STATUSES[id] ? statusClause(BUILTIN_STATUSES[id]) : null,
    sort: null,
    group_by: null,
    is_builtin: true,
    schema_version: 1,
  }
}

export const BUILTIN: SavedView[] = [
  builtin(-1, '全部机会', 'table'),
  builtin(-2, '待投递', 'board'),
  builtin(-3, '进行中', 'board'),
  builtin(-4, '收到 Offer', 'board'),
  builtin(-5, '已结束', 'board'),
]

export function statusesForView(viewId: number): string[] {
  return BUILTIN_STATUSES[viewId] ?? []
}

/**
 * Build the `/views/query` filter list for the active view, search term and
 * ad-hoc chips. Pure so the query shape stays testable.
 */
export function buildFilters(
  view: SavedView | undefined,
  search: string,
  extra: FilterNode[],
): FilterNode[] {
  const conds: FilterNode[] = []
  if (view?.filter_ast) {
    const ast = view.filter_ast as FilterGroup
    if (view.id >= 0 && Array.isArray(ast.conditions)) conds.push(...ast.conditions)
    else if (view.id < 0) conds.push(ast)
  }
  if (search) conds.push({ field: 'position', op: 'contains', value: search })
  return [...conds, ...extra]
}

export interface BoardBucket {
  title: string
  dot: string
  statuses: string[]
}

/**
 * The board's five columns, matching the design's 看板 artboard. Raw statuses
 * are merged so the row never wraps: 7 pipeline stages would overflow the
 * `minmax(210px, 1fr)` grid on a laptop viewport.
 */
export const BOARD_BUCKETS: BoardBucket[] = [
  { title: '待投递 / 准备', dot: 'var(--neutral)', statuses: ['saved', 'preparing'] },
  { title: '已投递', dot: 'var(--info)', statuses: ['applied'] },
  { title: '笔试 / 沟通', dot: 'var(--warning)', statuses: ['screening', 'assessment'] },
  { title: '面试中', dot: 'var(--accent)', statuses: ['interviewing'] },
  { title: 'Offer / 结果', dot: 'var(--positive)', statuses: ['offer', 'accepted', 'rejected', 'withdrawn', 'closed'] },
]

/**
 * Board columns for the active view: every bucket for "全部机会", otherwise only
 * the buckets the view's own status set can actually contain.
 */
export function boardBuckets(viewId: number): BoardBucket[] {
  const allowed = statusesForView(viewId)
  if (allowed.length === 0) return BOARD_BUCKETS
  const set = new Set(allowed)
  return BOARD_BUCKETS.map((b) => ({ ...b, statuses: b.statuses.filter((s) => set.has(s) ) })).filter(
    (b) => b.statuses.length > 0,
  )
}
