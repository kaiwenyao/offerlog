// 数据库表格列宽：自适应量宽 + 手动拖宽的纯逻辑（columns.ts）。
//
// 用户反馈：公司与岗位两列固定 150px，稍长的公司名 / 岗位名被 .ellipsis 截断，
// 看不到全称。这里锁住三件事：
//   1. 默认宽度按本页最长内容量出来（不再截断）；
//   2. 拖过手柄的列以显式宽度覆盖，且跨会话保留；
//   3. 坏 localStorage 数据绝不产生 NaN / 越界宽度。
import { describe, expect, it } from 'vitest'
import {
  clampColWidth,
  COL_WIDTHS_STORAGE_KEY,
  columnByKey,
  DB_COLUMNS,
  estimateTextWidth,
  fitWidths,
  loadColWidths,
  resolveWidths,
  totalWidth,
  visibleColumnKeys,
  type DbColumnKey,
  type TextMeasure,
} from '../src/features/database/columns'
import type { AppRow } from '../src/lib/types'

/** 1em = px 的确定性测量器：CJK 按全宽、ASCII 按 0.6em，单测不依赖 DOM 布局。 */
const measure: TextMeasure = (text, px) => {
  let em = 0
  for (const ch of text) em += /[\u2e80-\u9fff\uff00-\uff60]/.test(ch) ? 1 : 0.6
  return Math.ceil(em * px)
}

function row(company: string, position: string, location = ''): AppRow {
  return { company_name: company, position, location } as AppRow
}

describe('column geometry', () => {
  it('exports the two fit columns and keeps checkbox / actions locked', () => {
    expect(columnByKey('company').fit).toBe(true)
    expect(columnByKey('position').fit).toBe(true)
    expect(columnByKey('select').locked).toBe(true)
    expect(columnByKey('actions').locked).toBe(true)
    expect(columnByKey('next').flex).toBe(true)
    // 每个键唯一，且都有合法的 min <= width <= max。
    const keys = DB_COLUMNS.map((c) => c.key)
    expect(new Set(keys).size).toBe(keys.length)
    for (const c of DB_COLUMNS) {
      expect(c.min).toBeLessThanOrEqual(c.width)
      expect(c.width).toBeLessThanOrEqual(c.max)
    }
  })

  it('hides the trash 操作 column outside the recycle bin', () => {
    expect(visibleColumnKeys(false)).not.toContain('actions')
    expect(visibleColumnKeys(true)).toContain('actions')
    expect(visibleColumnKeys(false)).toContain('select')
    expect(visibleColumnKeys(true)[0]).toBe('select')
  })

  it('clamps widths into the column range and rejects NaN', () => {
    expect(clampColWidth(columnByKey('company'), 10)).toBe(120)
    expect(clampColWidth(columnByKey('company'), 9999)).toBe(460)
    expect(clampColWidth(columnByKey('company'), 200.4)).toBe(200)
    expect(clampColWidth(columnByKey('company'), Number.NaN)).toBe(columnByKey('company').width)
  })
})

describe('auto-fit widths', () => {
  it('fits 公司 and 岗位 to the widest cell instead of the old fixed 150px', () => {
    const rows = [row('字节跳动', '后端工程师', '北京'), row('某某某科技有限公司', '高级后端开发工程师（Go / 云原生）')]
    const fit = fitWidths(rows, measure)
    expect(fit.company).toBeGreaterThan(150) // 旧固定宽度会截断 8 个字的公司名
    expect(fit.position).toBeGreaterThan(150)
    // 量宽必须真的容得下最长的那条：文字 + 图标 + 内边距 + 余量。
    expect(fit.company!).toBeGreaterThanOrEqual(measure('某某某科技有限公司', 14, 500) + 22 + 9)
    expect(fit.position!).toBeGreaterThanOrEqual(measure('高级后端开发工程师（Go / 云原生）', 13, 500))
  })

  it('also fits the location sub-line under 岗位', () => {
    const short = fitWidths([row('A', 'B')], measure).position!
    const withLoc = fitWidths([row('A', 'B', '上海市浦东新区张江高科技园区')], measure).position!
    expect(withLoc).toBeGreaterThan(short)
  })

  it('respects the max cap for absurdly long names', () => {
    const fit = fitWidths([row('公'.repeat(200), '岗'.repeat(200))], measure)
    expect(fit.company).toBe(columnByKey('company').max)
    expect(fit.position).toBe(columnByKey('position').max)
  })

  it('returns no fit widths for an empty page (falls back to base widths)', () => {
    expect(fitWidths([], measure)).toEqual({})
  })

  it('estimates CJK wider than latin at the same length', () => {
    expect(estimateTextWidth('中文字符', 14, 400)).toBeGreaterThan(estimateTextWidth('abcdef', 14, 400))
  })
})

describe('resolveWidths', () => {
  const keys = visibleColumnKeys(false)
  const fit = fitWidths([row('某某某科技有限公司', '高级后端开发工程师')], measure)

  it('uses fit widths by default and lets the flex column fill the card', () => {
    const container = 1600
    const w = resolveWidths(keys, fit, {}, container)
    expect(w.company).toBe(fit.company)
    expect(w.position).toBe(fit.position)
    // 表格正好铺满容器：没有右侧留白，也不溢出。
    expect(totalWidth(w, keys)).toBe(container)
    expect(w.next).toBeGreaterThan(columnByKey('next').width)
  })

  it('keeps the base widths when the container is narrower than the columns', () => {
    const w = resolveWidths(keys, fit, {}, 400)
    expect(totalWidth(w, keys)).toBeGreaterThan(400) // 溢出走横向滚动
    expect(w.next).toBe(columnByKey('next').width)
  })

  it('lets a manual width win over the auto-fit width', () => {
    const w = resolveWidths(keys, fit, { company: 320 }, 1600)
    expect(w.company).toBe(320)
    expect(w.position).toBe(fit.position)
  })

  it('pins the flex column once the user resizes it (no silent snap back)', () => {
    const w = resolveWidths(keys, fit, { next: 140 }, 1600)
    expect(w.next).toBe(140)
    expect(totalWidth(w, keys)).toBeLessThan(1600)
  })

  it('clamps manual widths to the column min/max', () => {
    const w = resolveWidths(keys, {}, { company: 5, position: 5000 }, 1600)
    expect(w.company).toBe(columnByKey('company').min)
    expect(w.position).toBe(columnByKey('position').max)
  })
})

describe('loadColWidths (localStorage is untrusted)', () => {
  it('round-trips a saved override', () => {
    const raw = JSON.stringify({ company: 275, position: 340 })
    expect(loadColWidths(raw)).toEqual({ company: 275, position: 340 })
  })

  it('ignores broken JSON, arrays and non-numeric / unknown keys', () => {
    expect(loadColWidths('{oops')).toEqual({})
    expect(loadColWidths('[1,2]')).toEqual({})
    expect(loadColWidths('null')).toEqual({})
    expect(loadColWidths(JSON.stringify({ company: 'wide', nope: 200, position: null }))).toEqual({})
  })

  it('clamps out-of-range values and never restores locked columns', () => {
    const raw = JSON.stringify({ company: 9999, position: 1, select: 500, actions: 150 })
    expect(loadColWidths(raw)).toEqual({ company: 460, position: 120 })
  })

  it('exposes a stable storage key', () => {
    expect(COL_WIDTHS_STORAGE_KEY).toBe('offerlog:db-col-widths')
  })
})
