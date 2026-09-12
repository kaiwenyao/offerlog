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
 * Fields the database-page search box matches. Mirrors the ⌘K search
 * endpoint (position / company_name / notes) so the topbar placeholder
 * 「搜岗位、公司、备注…」 is honest about both surfaces.
 */
const SEARCH_FIELDS = ['company_name', 'position', 'notes'] as const

/**
 * 空白分词后最多这么多组条件：后端 /views/query 有 MaxConditions=30 的硬限
 * 制（每组 3 个叶子），5 个词封顶也到不了 30，且人类搜索词几乎不会更多。
 */
const SEARCH_TERM_LIMIT = 5

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
  // Search matches on ANY of the searched fields (OR inside a term), and a
  // multi-word query like「字节 后端」requires EVERY word to match (AND across
  // terms) — same semantics as the ⌘K endpoint. Typing a company name must
  // find its rows; a two-word query must not silently return nothing.
  for (const term of search.trim().split(/\s+/).filter(Boolean).slice(0, SEARCH_TERM_LIMIT)) {
    conds.push({
      op: 'or',
      conditions: SEARCH_FIELDS.map((field) => ({ field, op: 'contains', value: term })),
    })
  }
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

// ---------------------------------------------------------------- Sorting --
// 用户在数据库页自选列表排序（按最后更新时间 / 创建岗位时间）。字段名与
// 后端 views.CoreFields 的可排序字段一一对应（updated_at / created_at 都在
// 白名单里），方向沿用后端约束 asc|desc。

export type SortField = 'updated_at' | 'created_at'
export type SortDir = 'asc' | 'desc'

/** Wire shape of one `/views/query` sort clause. */
export interface SortClause {
  field: SortField
  dir: SortDir
}

export const DB_SORT_STORAGE_KEY = 'offerlog:db-sort'

export const DEFAULT_DB_SORT: SortClause = { field: 'updated_at', dir: 'desc' }

export const DB_SORT_FIELDS: Array<{ value: SortField; label: string }> = [
  { value: 'updated_at', label: '最后更新时间' },
  { value: 'created_at', label: '创建岗位时间' },
]

export function sortDirLabel(dir: SortDir): string {
  return dir === 'desc' ? '新 → 旧' : '旧 → 新'
}

/**
 * Parse + validate the persisted sort preference. localStorage 内容不可信，
 * 任何不认识/残缺的形状都静默回退到默认（最新更新在前），不让坏数据
 * 变成一次 400。
 */
export function loadDbSort(raw: string | null): SortClause {
  if (!raw) return DEFAULT_DB_SORT
  try {
    const v = JSON.parse(raw) as { field?: unknown; dir?: unknown }
    const field = DB_SORT_FIELDS.some((f) => f.value === v.field) ? (v.field as SortField) : null
    if (!field) return DEFAULT_DB_SORT
    const dir: SortDir = v.dir === 'asc' ? 'asc' : v.dir === 'desc' ? 'desc' : DEFAULT_DB_SORT.dir
    return { field, dir }
  } catch {
    return DEFAULT_DB_SORT
  }
}
