// 数据库表格的列几何 + 列宽偏好。
//
// 为什么需要这个模块：公司和岗位是两列自由文本，固定 150px 时稍长的公司名
// （「某某某科技有限公司」）或岗位名（「高级后端开发工程师（Go/云原生）」）
// 一律被 .ellipsis 截断，用户看不到全称。这里做的两件事：
//
//   1. 默认宽度按当前页内容的实际文字宽度量出来（fit），所以开箱即用就能显示全；
//   2. 允许用户拖表头分隔线手动改宽度，偏好写 localStorage，跨会话保留。
//
// 逻辑保持纯函数（文字宽度由外部注入 measure），便于单测覆盖。
import type { AppRow } from '../../lib/types'

export type DbColumnKey =
  | 'select'
  | 'company'
  | 'position'
  | 'stage'
  | 'status'
  | 'next'
  | 'due'
  | 'channel'
  | 'salary'
  | 'priority'
  | 'actions'

export interface DbColumn {
  key: DbColumnKey
  /** 表头文字；勾选列没有表头文字。 */
  label: string
  /** 未 fit、无用户覆盖时使用的宽度。 */
  width: number
  min: number
  max: number
  /** 按本页最宽单元格自适应（公司与岗位）。 */
  fit?: boolean
  /** 勾选框 / 回收站操作列：宽度固定，不给拖拽手柄。 */
  locked?: boolean
  /** 吸收卡片剩余宽度，保证表格铺满容器（下一步）。 */
  flex?: boolean
}

/**
 * 列顺序与宽度上下限。min/max 的下限保证关键内容（工序条、日期、状态芯片）
 * 在拖到最窄时仍然可读，上限避免一条超长备注把整张表撑到屏幕外。
 */
export const DB_COLUMNS: readonly DbColumn[] = [
  { key: 'select', label: '', width: 36, min: 36, max: 36, locked: true },
  { key: 'company', label: '公司', width: 150, min: 120, max: 460, fit: true },
  { key: 'position', label: '岗位', width: 150, min: 120, max: 520, fit: true },
  { key: 'stage', label: '阶段推进', width: 176, min: 150, max: 320 },
  { key: 'status', label: '状态', width: 116, min: 96, max: 260 },
  { key: 'next', label: '下一步', width: 160, min: 110, max: 1200, flex: true },
  { key: 'due', label: '截止', width: 92, min: 78, max: 220 },
  { key: 'channel', label: '渠道', width: 88, min: 70, max: 220 },
  { key: 'salary', label: '薪资', width: 104, min: 84, max: 240 },
  { key: 'priority', label: '优先', width: 56, min: 52, max: 140 },
  { key: 'actions', label: '操作', width: 80, min: 80, max: 200, locked: true },
]

export type ColWidths = Partial<Record<DbColumnKey, number>>

export const COL_WIDTHS_STORAGE_KEY = 'offerlog:db-col-widths'

/** `.tbl td, .tbl th { padding: 8px var(--space-3) }` → 左右各 12px。 */
const CELL_PADDING = 24
/** 公司名左侧的 CompanyMark 方块 + 间距（见 DatabasePage 的 gap: 9）。 */
const COMPANY_MARK = 22
const COMPANY_MARK_GAP = 9
/** 量出来的宽度再留一点余量，避免亚像素舍入把最后一个字截掉。 */
const FIT_SLACK = 8

export function columnByKey(key: DbColumnKey): DbColumn {
  const col = DB_COLUMNS.find((c) => c.key === key)
  if (!col) throw new Error(`unknown column: ${key}`)
  return col
}

/** 勾选列永远在；回收站操作列只在回收站模式下出现。 */
export function visibleColumnKeys(trashMode: boolean): DbColumnKey[] {
  return DB_COLUMNS.filter((c) => c.key !== 'actions' || trashMode).map((c) => c.key)
}

export function clampColWidth(col: DbColumn, width: number): number {
  if (!Number.isFinite(width)) return col.width
  return Math.round(Math.min(col.max, Math.max(col.min, width)))
}

/**
 * 文字宽度测量器：输入文字、字号、字重，返回像素宽度。
 * 浏览器里由隐藏探针 DOM 提供，量不到时（jsdom / 字体未加载）回退到估算。
 */
export type TextMeasure = (text: string, px: number, weight: number) => number

/** 全角字符码点：CJK、假名、谚文、全角标点、常见 emoji 区段。 */
function isWide(ch: string): boolean {
  const cp = ch.codePointAt(0) ?? 0
  return (
    (cp >= 0x1100 && cp <= 0x115f) ||
    (cp >= 0x2e80 && cp <= 0xa4cf) ||
    (cp >= 0xac00 && cp <= 0xd7a3) ||
    (cp >= 0xf900 && cp <= 0xfaff) ||
    (cp >= 0xfe30 && cp <= 0xfe6f) ||
    (cp >= 0xff00 && cp <= 0xff60) ||
    (cp >= 0xffe0 && cp <= 0xffe6) ||
    (cp >= 0x1f300 && cp <= 0x1faff) ||
    (cp >= 0x20000 && cp <= 0x3fffd)
  )
}

/**
 * 无 DOM 时的字符宽度估算（em 系数偏保守，宁可多留几个像素也不截断）。
 * 仅用于 jsdom / 探针量不出宽度的场景，真实浏览器走实际测量。
 */
export function estimateTextWidth(text: string, px: number, weight: number): number {
  let em = 0
  for (const ch of text) {
    if (isWide(ch)) em += 1
    else if (/[A-Z0-9]/.test(ch) || 'mw@%&'.includes(ch)) em += 0.72
    else em += 0.58
  }
  return Math.ceil(em * px * (weight >= 600 ? 1.03 : 1))
}

/**
 * 公司与岗位的自适应宽度：取本页最宽的一条内容，加上图标 / 内边距 / 余量，
 * 再按列的 min/max 收口。没有数据时返回空对象（回落到基准宽度）。
 */
export function fitWidths(rows: AppRow[], measure: TextMeasure): ColWidths {
  if (rows.length === 0) return {}
  let company = 0
  let position = 0
  for (const r of rows) {
    const name = r.company_name ?? ''
    if (name) company = Math.max(company, measure(name, 14, 500) + COMPANY_MARK + COMPANY_MARK_GAP)
    const role = r.position ?? ''
    if (role) position = Math.max(position, measure(role, 13, 500))
    const loc = r.location ?? ''
    if (loc) position = Math.max(position, measure(loc, 12, 400))
  }
  const out: ColWidths = {}
  if (company > 0) out.company = clampColWidth(columnByKey('company'), company + CELL_PADDING + FIT_SLACK)
  if (position > 0) out.position = clampColWidth(columnByKey('position'), position + CELL_PADDING + FIT_SLACK)
  return out
}

/**
 * 最终列宽 = 用户覆盖 > 自适应 > 基准宽度，再让 flex 列吃掉容器剩余宽度。
 *
 * flex 列一旦被用户手动拖过（overrides 里有它）就不再自动伸展——用户明确要了
 * 那个宽度，此时表格总宽可能小于容器，右侧留白比「拖了没反应」更诚实。
 */
export function resolveWidths(
  keys: DbColumnKey[],
  autoFit: ColWidths,
  overrides: ColWidths,
  containerWidth: number,
): Record<DbColumnKey, number> {
  const widths = {} as Record<DbColumnKey, number>
  let total = 0
  for (const key of keys) {
    const col = columnByKey(key)
    const wanted = overrides[key] ?? autoFit[key] ?? col.width
    widths[key] = clampColWidth(col, wanted)
    total += widths[key]
  }
  const flexKey = keys.find((k) => columnByKey(k).flex)
  if (flexKey && overrides[flexKey] == null && containerWidth > total) {
    const flex = columnByKey(flexKey)
    widths[flexKey] = clampColWidth(flex, widths[flexKey] + (containerWidth - total))
  }
  return widths
}

export function totalWidth(widths: Record<DbColumnKey, number>, keys: DbColumnKey[]): number {
  return keys.reduce((n, k) => n + (widths[k] ?? 0), 0)
}

/**
 * 解析 localStorage 里的列宽偏好。localStorage 内容不可信：坏 JSON、未知列名、
 * 非数字、越界值一律丢弃 / 收口，绝不把脏数据变成 NaN 宽度。
 */
export function loadColWidths(raw: string | null): ColWidths {
  if (!raw) return {}
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return {}
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
  const out: ColWidths = {}
  for (const col of DB_COLUMNS) {
    if (col.locked) continue
    const v = (parsed as Record<string, unknown>)[col.key]
    if (typeof v === 'number' && Number.isFinite(v)) out[col.key] = clampColWidth(col, v)
  }
  return out
}
