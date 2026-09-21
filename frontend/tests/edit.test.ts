// 岗位基础信息编辑（edit.ts）：创建岗位时只填了公司和岗位，其余必须能在详情页
// 补齐/改错。这里锁住「表单值 → PATCH /applications/:id」的载荷规则：
//   * 必填校验（公司 / 岗位）；
//   * 文本 trim，留空发空字符串（后端按「清空」处理）；
//   * 薪资留空发 null（清空），不是 0；负数 / 非数字 / 下限高于上限直接拒绝；
//   * 一定带上 version——PATCH 走乐观锁，缺了会被后端当成 0 而必然 409。
import { describe, expect, it } from 'vitest'
import { buildApplicationPatch, editFieldsFromApp, rebaseEditFields } from '../src/features/database/edit'
import type { AppRow } from '../src/lib/types'

function app(over: Partial<AppRow> = {}): AppRow {
  return {
    id: 7,
    version: 3,
    company_name: 'Acme',
    position: 'Backend',
    location: '上海',
    job_url: 'https://acme.test/jd',
    remote_policy: '混合',
    priority: 'high',
    channel: '内推',
    deadline: '2026-10-01',
    salary_min: 25000,
    salary_max: 35000,
    salary_currency: 'CNY',
    notes: '已有备注',
    ...over,
  } as AppRow
}

describe('editFieldsFromApp', () => {
  it('turns nulls into empty strings and numbers into editable text', () => {
    const f = editFieldsFromApp(app({ salary_min: null, salary_max: null, deadline: null, notes: '', channel: '' }))
    expect(f.salary_min).toBe('')
    expect(f.salary_max).toBe('')
    expect(f.deadline).toBe('')
    expect(f.channel).toBe('')
    expect(editFieldsFromApp(app()).salary_min).toBe('25000')
  })

  it('falls back to 中 priority when the row has none', () => {
    expect(editFieldsFromApp(app({ priority: '' })).priority).toBe('medium')
  })
})

describe('buildApplicationPatch', () => {
  it('sends the optimistic-lock version and trimmed base fields', () => {
    const body = buildApplicationPatch(
      app(),
      editFieldsFromApp(app({ company_name: '  Acme  ', position: ' Backend ', location: ' 上海 ' })),
    )
    expect(body.version).toBe(3)
    expect(body.company_name).toBe('Acme')
    expect(body.position).toBe('Backend')
    expect(body.location).toBe('上海')
    expect(body.channel).toBe('内推')
    expect(body.deadline).toBe('2026-10-01')
    expect(body.salary_min).toBe(25000)
    expect(body.salary_max).toBe(35000)
    expect(body.salary_currency).toBe('CNY')
  })

  it('keeps a filled salary as an integer and rounds decimals', () => {
    const f = { ...editFieldsFromApp(app()), salary_min: '25000.4', salary_max: ' 35000 ' }
    const body = buildApplicationPatch(app(), f)
    expect(body.salary_min).toBe(25000)
    expect(body.salary_max).toBe(35000)
  })

  it('clears salary with null instead of 0 when the inputs are emptied', () => {
    const f = { ...editFieldsFromApp(app()), salary_min: '', salary_max: '' }
    const body = buildApplicationPatch(app(), f)
    expect(body.salary_min).toBeNull()
    expect(body.salary_max).toBeNull()
  })

  it('clears the deadline with an empty string (backend parses it to null)', () => {
    const body = buildApplicationPatch(app(), { ...editFieldsFromApp(app()), deadline: '' })
    expect(body.deadline).toBe('')
  })

  it('rejects a negative, non-numeric or inverted salary', () => {
    const base = editFieldsFromApp(app())
    expect(() => buildApplicationPatch(app(), { ...base, salary_min: '-1' })).toThrow(/薪资下限/)
    expect(() => buildApplicationPatch(app(), { ...base, salary_max: 'abc' })).toThrow(/薪资上限/)
    expect(() => buildApplicationPatch(app(), { ...base, salary_min: '40000', salary_max: '30000' })).toThrow(
      /下限不能高于上限/,
    )
  })

  it('requires company and position', () => {
    const base = editFieldsFromApp(app())
    expect(() => buildApplicationPatch(app(), { ...base, company_name: '   ' })).toThrow(/公司/)
    expect(() => buildApplicationPatch(app(), { ...base, position: '' })).toThrow(/岗位/)
  })

  it('sends an empty string when a text field is emptied (clearing it)', () => {
    const body = buildApplicationPatch(app(), { ...editFieldsFromApp(app()), job_url: '  ', location: '', channel: '' })
    expect(body.job_url).toBe('')
    expect(body.location).toBe('')
    expect(body.channel).toBe('')
  })
})

describe('rebaseEditFields: 409 之后只保住自己改过的字段', () => {
  // PATCH 会把整张表单发出去。只换 version、字段还停在打开时的旧值，
  // 别人改过、自己没动的字段会被写回去，乐观锁等于没锁。
  it('keeps dirty fields and takes incoming values for the rest', () => {
    const baseline = editFieldsFromApp(app())
    const current = { ...baseline, location: '北京' }
    const incoming = editFieldsFromApp(app({ version: 4, location: '深圳', notes: '别人加的备注', company_name: 'Acme' }))
    const next = rebaseEditFields(current, baseline, incoming)
    expect(next.location).toBe('北京')
    expect(next.notes).toBe('别人加的备注')
    expect(next.company_name).toBe('Acme')
  })

  it('takes the whole incoming row when nothing was edited', () => {
    const baseline = editFieldsFromApp(app())
    const incoming = editFieldsFromApp(app({ location: 'Berlin', salary_min: 30000 }))
    expect(rebaseEditFields(baseline, baseline, incoming)).toEqual(incoming)
  })

  it('keeps every locally edited field even if the incoming row changed them too', () => {
    const baseline = editFieldsFromApp(app())
    const current = { ...baseline, location: '北京', notes: '我的备注' }
    const incoming = editFieldsFromApp(app({ location: '深圳', notes: '别人的备注' }))
    const next = rebaseEditFields(current, baseline, incoming)
    expect(next.location).toBe('北京')
    expect(next.notes).toBe('我的备注')
  })
})
