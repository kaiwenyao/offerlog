/**
 * 活动轮次 / 待办 / 备注的编辑载荷（纯函数）。
 *
 * 为什么需要这一层：后端的 PATCH 大多是「合并式」，但合并的字段各不相同——
 * 漏掉某个已存在的字段，一次「只改时间」的编辑就会把它清成空值：
 *
 *   - `PATCH /interviews/:id`：scheduled_at / round_name / format / timezone /
 *     feedback / notes 是**整体覆盖**（请求里没带就写 null / ""），而
 *     progress / result / completed_at / invited_at 缺省时才回退到现有值；
 *   - `PATCH /assessments/:id`：kind / name / progress / result / link / notes
 *     整体覆盖，四种时间（invited/planned/due/completed）缺省时回退；
 *   - `PATCH /actions/:id`：**完全不合并**，请求体就是那一行的全部字段。
 *
 * 所以编辑表单不能只发「用户改过的那个字段」，必须把整行拼回请求体——这些
 * builder 就是干这个的，且与 React 无关，便于单测覆盖。
 *
 * 时间规则：
 *   - 面试时间 / OA 截止时间来自 `datetime-local`（无时区挂墙时间），按用户
 *     配置时区换算成 UTC 瞬间，并把时区标签一并写回（与新建时同一规则）；
 *   - 待办截止日是日历日 YYYY-MM-DD，原样发送，绝不做时区换算（DATE 列）。
 */
import { toInstantInUserZone, toLocalDateTimeInput } from '../../lib/tz'
import type { ActionItem, AssessmentRound, Interview } from '../../lib/types'

/* ---------------------------------- 面试 ---------------------------------- */

export interface InterviewEditFields {
  round_name: string
  format: string
  /** datetime-local 的挂墙时间；'' = 时间未定（清空） */
  scheduled: string
}

/** 现有面试行 → 编辑表单初始值（时间按用户时区渲染成 datetime-local）。 */
export function interviewEditFields(it: Interview): InterviewEditFields {
  return {
    round_name: it.round_name ?? '',
    format: it.format ?? '',
    scheduled: it.scheduled_at ? toLocalDateTimeInput(it.scheduled_at) : '',
  }
}

/**
 * 表单值 + 现有行 → PATCH /interviews/:id 请求体。
 *
 * 除了用户能改的轮次 / 形式 / 时间，其余字段全部回填现有值：后端对它们不做
 * 合并，漏一个就等于「顺手清掉」——尤其 feedback 与 notes（用户没在编辑它们，
 * 不该因为改了时间而丢失）。
 */
export function buildInterviewPatch(it: Interview, f: InterviewEditFields): Record<string, unknown> {
  const roundName = f.round_name.trim()
  if (roundName === '') throw new Error('请填写轮次名称')
  let scheduledAt: string | null = null
  let zone = it.timezone ?? ''
  if (f.scheduled) {
    const { iso, zone: z } = toInstantInUserZone(f.scheduled)
    if (iso == null) throw new Error('时间格式不正确')
    scheduledAt = iso
    zone = z
  }
  return {
    round_name: roundName,
    format: f.format,
    scheduled_at: scheduledAt,
    timezone: zone,
    duration_minutes: it.duration_minutes,
    progress: it.progress ?? '',
    result: it.result,
    invited_at: it.invited_at,
    completed_at: it.completed_at,
    completed_unknown: it.completed_unknown,
    feedback: it.feedback ?? '',
    notes: it.notes ?? '',
  }
}

/* ----------------------------------- OA ----------------------------------- */

export interface AssessmentEditFields {
  name: string
  /** datetime-local 的挂墙时间；'' = 清空截止时间 */
  due: string
  link: string
}

export function assessmentEditFields(a: AssessmentRound): AssessmentEditFields {
  return {
    name: a.name ?? '',
    due: a.due_at ? toLocalDateTimeInput(a.due_at) : '',
    link: a.link ?? '',
  }
}

/**
 * 表单值 + 现有轮次 → PATCH /assessments/:id 请求体。
 *
 * due_at 被清空时必须显式带 `clear_due_at`：后端把「没传」与「传 null」都当成
 * 「保留原值」，不然用户清掉截止时间后刷新一看它还在。
 */
export function buildAssessmentPatch(a: AssessmentRound, f: AssessmentEditFields): Record<string, unknown> {
  const name = f.name.trim()
  if (name === '') throw new Error('请填写名称')
  let dueAt: string | null = null
  if (f.due) {
    const { iso } = toInstantInUserZone(f.due)
    if (iso == null) throw new Error('时间格式不正确')
    dueAt = iso
  }
  return {
    kind: a.kind,
    name,
    progress: a.progress ?? '',
    result: a.result,
    invited_at: a.invited_at,
    planned_at: a.planned_at,
    due_at: dueAt,
    clear_due_at: dueAt == null,
    completed_at: a.completed_at,
    completed_unknown: a.completed_unknown,
    link: f.link.trim(),
    notes: a.notes ?? '',
  }
}

/* ---------------------------------- 待办 ---------------------------------- */

export interface ActionEditFields {
  title: string
  /** 日历日 YYYY-MM-DD；'' = 无截止日期 */
  due_date: string
}

export function actionEditFields(a: ActionItem): ActionEditFields {
  return { title: a.title ?? '', due_date: a.due_date ?? '' }
}

/**
 * 表单值 + 现有待办 → PATCH /actions/:id 请求体。
 *
 * 后端这里是整体覆盖（没有合并逻辑），所以 due_ts / done_at / remind_me /
 * remind_at / priority 必须原样回填：一次「改标题」不该顺手清掉精确截止时间
 * 或把已完成的待办变回未完成。
 */
export function buildActionPatch(a: ActionItem, f: ActionEditFields): Record<string, unknown> {
  const title = f.title.trim()
  if (title === '') throw new Error('请填写待办内容')
  const due = f.due_date.trim()
  if (due !== '' && !/^\d{4}-\d{2}-\d{2}$/.test(due)) throw new Error('截止日期格式需为 YYYY-MM-DD')
  return {
    title,
    due_date: due === '' ? null : due,
    due_ts: a.due_ts,
    done_at: a.done_at,
    remind_me: a.remind_me,
    priority: a.priority,
  }
}

/* ---------------------------------- 备注 ---------------------------------- */

/** 备注只有正文可改；空内容没有意义，直接拒绝。 */
export function buildNotePatch(content: string): { content_md: string } {
  const md = content.trim()
  if (md === '') throw new Error('备注内容不能为空')
  return { content_md: md }
}
