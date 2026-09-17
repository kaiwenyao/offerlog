import type { AppRow } from '../../lib/types'

/**
 * 岗位基础信息的编辑载荷（纯函数）。
 *
 * 创建岗位时只要求「公司 + 岗位」，其余（城市、JD 链接、薪资、渠道、截止日期）
 * 必须能在详情页补齐——这个模块把「表单值 → PATCH /applications/:id 请求体」的
 * 规则集中起来，好让它与 React 无关地被单测覆盖：
 *
 * - 文本字段 trim 后原样发送（空字符串表示清空，与后端 setStr 语义一致）；
 * - deadline 用日历日 YYYY-MM-DD，留空发空字符串（后端解析为 null = 清除）；
 * - 薪资是月薪整数：留空发 null（**清除**，不是填 0），负数/非数字直接拒绝；
 * - 必须带 version：PATCH 走乐观锁，缺了会被后端当成 0 而必然冲突。
 */
export interface ApplicationEditFields {
  company_name: string
  position: string
  location: string
  job_url: string
  remote_policy: string
  priority: string
  channel: string
  deadline: string
  salary_min: string
  salary_max: string
  salary_currency: string
  notes: string
}

/** 详情页返回的岗位行 → 编辑表单的初始值（null 一律落成空字符串）。 */
export function editFieldsFromApp(app: AppRow): ApplicationEditFields {
  return {
    company_name: app.company_name ?? '',
    position: app.position ?? '',
    location: app.location ?? '',
    job_url: app.job_url ?? '',
    remote_policy: app.remote_policy ?? '',
    priority: app.priority || 'medium',
    channel: app.channel ?? '',
    deadline: app.deadline ?? '',
    salary_min: app.salary_min == null ? '' : String(app.salary_min),
    salary_max: app.salary_max == null ? '' : String(app.salary_max),
    salary_currency: app.salary_currency ?? '',
    notes: app.notes ?? '',
  }
}

/** 薪资输入解析：'' → null（清空）；非负整数 → 数字；其余抛错。 */
function parseSalary(raw: string, label: string): number | null {
  const t = raw.trim()
  if (t === '') return null
  const n = Number(t)
  if (!Number.isFinite(n) || n < 0) throw new Error(`${label}请填写非负数字`)
  return Math.round(n)
}

export function buildApplicationPatch(app: AppRow, f: ApplicationEditFields): Record<string, unknown> {
  const company = f.company_name.trim()
  const position = f.position.trim()
  if (company === '') throw new Error('请填写公司名称')
  if (position === '') throw new Error('请填写岗位名称')
  const salaryMin = parseSalary(f.salary_min, '薪资下限')
  const salaryMax = parseSalary(f.salary_max, '薪资上限')
  if (salaryMin != null && salaryMax != null && salaryMin > salaryMax) {
    throw new Error('薪资下限不能高于上限')
  }
  return {
    version: app.version,
    company_name: company,
    position,
    location: f.location.trim(),
    job_url: f.job_url.trim(),
    remote_policy: f.remote_policy,
    priority: f.priority,
    channel: f.channel.trim(),
    deadline: f.deadline.trim(),
    salary_min: salaryMin,
    salary_max: salaryMax,
    salary_currency: f.salary_currency.trim(),
    notes: f.notes,
  }
}
