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
  /** datetime-local 的挂墙时间；'' = 清空 */
  invited: string
  /** datetime-local 的挂墙时间；'' = 清空 */
  planned: string
  /** datetime-local 的挂墙时间；'' = 清空截止时间 */
  due: string
  link: string
}

export function assessmentEditFields(a: AssessmentRound): AssessmentEditFields {
  return {
    name: a.name ?? '',
    invited: a.invited_at ? toLocalDateTimeInput(a.invited_at) : '',
    planned: a.planned_at ? toLocalDateTimeInput(a.planned_at) : '',
    due: a.due_at ? toLocalDateTimeInput(a.due_at) : '',
    link: a.link ?? '',
  }
}

/**
 * 表单值 + 现有轮次 → PATCH /assessments/:id 请求体。
 *
 * 四种时间（invited/planned/due/completed）后端都是合并语义：没传或传 null 都
 * 当成「保留原值」。所以清空必须显式带 `clear_*` 旗标，否则用户清掉后刷新一看
 * 旧值还在。
 *
 * planned_at 尤其重要：它才是首页「即将到来的面试 / OA」与日历（kind='assessment'）
 * 的时间源，due_at 只负责 assessment_due 那条截止线。只能改 due_at 的话，
 * 「计划开做」填错一位就仍然只能删了重建。
 */
export function buildAssessmentPatch(a: AssessmentRound, f: AssessmentEditFields): Record<string, unknown> {
  const name = f.name.trim()
  if (name === '') throw new Error('请填写名称')
  const at = (v: string, label: string): string | null => {
    if (!v) return null
    const { iso } = toInstantInUserZone(v)
    if (iso == null) throw new Error(`${label}格式不正确`)
    return iso
  }
  const invitedAt = at(f.invited, '收到邀请时间')
  const plannedAt = at(f.planned, '计划开做时间')
  const dueAt = at(f.due, '截止时间')
  return {
    kind: a.kind,
    name,
    progress: a.progress ?? '',
    result: a.result,
    invited_at: invitedAt,
    clear_invited_at: invitedAt == null,
    planned_at: plannedAt,
    clear_planned_at: plannedAt == null,
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
 * remind_at / priority 必须原样回填：一次「改标题」不该顺手清掉精确截止时间、
 * 提醒时间或把已完成的待办变回未完成。
 *
 * due_ts 是例外，它必须跟着用户改的日期走：所有读取端都是 due_ts 优先
 * （详情 tabs.tsx 的 shown、日历的 ORDER BY COALESCE(due_ts,…)、首页的
 * CASE WHEN due_ts IS NOT NULL、以及提醒生成器）。只改 due_date 却把旧的 due_ts
 * 原样送回去，对任何带 due_ts 的待办都是「改了没反应」——显示、日历、提醒全部
 * 纹丝不动，正是这个 PR 要消灭的那类 bug。而迁移 00002 把老的
 * applications.next_action_due_ts 回填进了 actions.due_ts，所以老实例的待办普遍
 * 带着它（新装实例没有，本地测不出来）。
 * 所以：用户动了截止日 → due_ts 置 null，让日历日成为唯一真相；没动就原样保留。
 */
export function buildActionPatch(a: ActionItem, f: ActionEditFields): Record<string, unknown> {
  const title = f.title.trim()
  if (title === '') throw new Error('请填写待办内容')
  const due = f.due_date.trim()
  if (due !== '' && !/^\d{4}-\d{2}-\d{2}$/.test(due)) throw new Error('截止日期格式需为 YYYY-MM-DD')
  const dueChanged = due !== (a.due_date ?? '').trim()
  return {
    title,
    due_date: due === '' ? null : due,
    due_ts: dueChanged ? null : a.due_ts,
    done_at: a.done_at,
    remind_me: a.remind_me,
    // 现在没有生成器读它，但 updateAction 是整体覆盖：不回填就会把它写成 NULL。
    remind_at: a.remind_at,
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
