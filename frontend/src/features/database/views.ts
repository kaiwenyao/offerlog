import type { SavedView } from '../../lib/types'

export interface FilterCond {
  field: string
  op: string
  // 可选：is_empty / is_not_empty 这类条件不带值（后端 FilterNode 也是 omitempty）。
  value?: unknown
}

export interface FilterGroup {
  op: 'and' | 'or'
  conditions: (FilterCond | FilterGroup)[]
}

/** A filter tree node: either a leaf condition or a nested group. */
export type FilterNode = FilterCond | FilterGroup

export type Layout = 'table' | 'board' | 'list'

/**
 * 侧栏快捷视图的 id（同样用负数，和内置视图同一个命名空间）。
 *
 * 这三个名字必须与结果一致（bug 原文）：之前「本周面试」实际查的是全部进行中
 * 岗位、「待跟进」就是全部机会、「已归档」筛的是已结束状态，于是归档一个进行中
 * 的岗位后反倒在「已归档」里找不到。
 */
export const WEEK_INTERVIEWS_VIEW = -101
export const FOLLOW_UP_VIEW = -102
export const ARCHIVED_VIEW = -103

/** 进行中的岗位（已投递 → 面试之间）：只有还没结束的岗位才谈得上「待跟进」。 */
export const IN_PROGRESS_STATUSES = ['applied', 'screening', 'assessment', 'interviewing']

/** Status sets backing the built-in views (ids are negative by convention). */
export const BUILTIN_STATUSES: Record<number, string[]> = {
  [-2]: ['saved', 'preparing'],
  [-3]: IN_PROGRESS_STATUSES,
  [-4]: ['offer', 'accepted'],
  [-5]: ['accepted', 'rejected', 'withdrawn', 'closed'],
  // 待跟进只可能是进行中的岗位，看板切到它时不该空出「Offer / 结果」整列。
  // 本周面试 / 已归档的岗位可能处在任何阶段，所以不限定看板列。
  [FOLLOW_UP_VIEW]: IN_PROGRESS_STATUSES,
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

function shortcut(id: number, name: string, layout: Layout): SavedView {
  // 快捷视图的条件由 shortcutConditions() 按上下文（今天 / 本周面试岗位）现算，
  // 所以 filter_ast 留空，避免出现「一半写死、一半现算」的两套真相。
  return { ...builtin(id, name, layout), filter_ast: null }
}

/** 侧栏「已保存视图」列表，与 layout.tsx 的导航项一一对应。 */
export const SHORTCUT_VIEWS: SavedView[] = [
  shortcut(WEEK_INTERVIEWS_VIEW, '本周面试', 'board'),
  shortcut(FOLLOW_UP_VIEW, '待跟进', 'list'),
  shortcut(ARCHIVED_VIEW, '已归档', 'table'),
]

/** 当前视图是否侧栏快捷视图（它们都是有筛选的视图，不是全量列表）。 */
export function isShortcutView(viewId: number): boolean {
  return SHORTCUT_VIEWS.some((v) => v.id === viewId)
}

/** 上下文：快捷视图里唯一不能写死的两部分——今天（按用户时区）与本周面试岗位。 */
export interface FilterContext {
  /** 用户时区的今天 YYYY-MM-DD；「待跟进」的逾期判断按日比较。 */
  today: string
  /** 本周（用户时区）有面试日程的岗位 id；undefined = 还没取到。 */
  weekInterviewIds?: number[]
}

/**
 * 侧栏快捷视图的筛选条件；非快捷视图返回 null。
 *
 * 纯函数：把「名字 → 筛选条件」的映射摆在一处，单测可以按住语义不再漂移。
 */
export function shortcutConditions(viewId: number, ctx: FilterContext): FilterNode[] | null {
  switch (viewId) {
    case WEEK_INTERVIEWS_VIEW:
      // 本周有面试日程的岗位：id 集合由前端按用户时区的周窗口（周一到下周一）
      // 从 /api/v1/calendar 取到，再交给 /views/query 过滤；空集合匹配不到任何行。
      return [
        { field: 'id', op: 'in', value: [...(ctx.weekInterviewIds ?? [])] },
        { field: 'archived', op: 'eq', value: false },
      ]
    case FOLLOW_UP_VIEW:
      // 进行中、没归档，且「该动了」：没有任何未完成待办（= 没有下一步安排），
      // 或者最早到期的那条未完成待办已经到期/逾期。
      //
      // 这里必须看 actions 表（唯一真相），而不是岗位行上的 next_action 镜像：
      // 完成最后一个待办时镜像会被清空，撤销（reopen）又不会把它复活，镜像还会因为
      // best-effort 同步失败而过期。只看镜像会出现「明明排了未来一步却被算成待跟进」
      // 「已完成的待办让岗位看起来有安排」这类名字与结果不符的错位。
      // 后端 open_todo_count / next_open_todo_due 与首页统一待办同一定义。
      // 日期比较只用 lte：NULL 的 due 在 SQL 里是 NULL（不命中），刚好由第一个分支
      // 兼底——不用 is_not_empty（它对 DATE 列会拼出 `<> ''`，PostgreSQL 直接报错）。
      return [
        statusClause(IN_PROGRESS_STATUSES),
        { field: 'archived', op: 'eq', value: false },
        {
          op: 'or',
          conditions: [
            { field: 'open_todo_count', op: 'eq', value: 0 },
            { field: 'next_open_todo_due', op: 'lte', value: ctx.today },
          ],
        },
      ]
    case ARCHIVED_VIEW:
      // 归档是可见性旗标（archived_at 非空），不是「已结束状态」。
      return [{ field: 'archived', op: 'eq', value: true }]
    default:
      return null
  }
}

export function statusesForView(viewId: number): string[] {
  return BUILTIN_STATUSES[viewId] ?? []
}

/**
 * 回收站列表的请求路径。
 *
 * 回收站也必须带当前 page：写死 page=1 时点「下一页」只改页码不换数据，第 60 条
 * 之后的删除记录永远翻不到，自然也没法恢复（后端 list 本就支持 trash=1&page）。
 */
export function trashQueryPath(page: number, pageSize: number): string {
  const p = Number.isFinite(page) && page > 0 ? Math.floor(page) : 1
  return `/api/v1/applications?trash=1&page=${p}&page_size=${pageSize}&include=stage_history`
}

/**
 * 选择集与当前查询结果的交集。
 *
 * 批量选择的 id 是按「当时屏幕上看到的那批行」勾的；翻页 / 切视图 / 加筛选 / 进
 * 回收站之后，那批行已经不在屏幕上了，顶部还写着「已选 10 条」就会让「应用」
 * 改到用户看不见的行（回收站里更是会真的改到已删除记录）。所以任何发批量请求、
 * 显示计数的地方都用这个函数把选择集收敛到当前结果集上。
 *
 * 纯函数：便于单测锁定「翻页后旧选择不会生效」。
 */
export function pruneSelection(selected: Iterable<number>, rows: Array<{ id: number }>): Set<number> {
  const visible = new Set(rows.map((r) => r.id))
  const out = new Set<number>()
  for (const id of selected) if (visible.has(id)) out.add(id)
  return out
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
  ctx: FilterContext,
): FilterNode[] {
  const conds: FilterNode[] = []
  const shortcut = view ? shortcutConditions(view.id, ctx) : null
  if (shortcut) {
    conds.push(...shortcut)
  } else if (view?.filter_ast) {
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
