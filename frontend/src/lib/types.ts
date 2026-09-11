// Typed API contract shared by the frontend (mirrors the OpenAPI-lite design).
export interface AppRow {
  id: number
  company_id: number
  company_name: string
  position: string
  job_url: string
  jd_snapshot: string
  location: string
  remote_policy: string
  employment_type: string
  salary_min: number | null
  salary_max: number | null
  salary_currency: string
  channel: string
  status: string
  /**
   * 大阶段内的具体进度（方案 §3）。"" = 未细分（旧数据与无细分阶段），
   * 绝不能把空值当成「准备 OA」。展示请用 comboLabel(status, substatus, ...)。
   */
  substatus: string
  /** 当前关注的活动轮次（方案 §3.3）；kind 为 assessment / interviewing / ""。 */
  focus_activity_kind: string
  focus_activity_id: number | null
  priority: string
  tags: string[]
  custom_values: Record<string, unknown>
  notes: string
  saved_at: string | null
  submitted_at: string | null
  first_response_at: string | null
  /** calendar day YYYY-MM-DD (DATE column) — render as a day, never new Date() */
  deadline: string | null
  accepted_at: string | null
  rejected_at: string | null
  reason: string
  next_action: string
  /** calendar day YYYY-MM-DD (DATE column) — render as a day, never new Date() */
  next_action_due_at: string | null
  version: number
  archived: boolean
  deleted: boolean
  created_at: string
  updated_at: string
  /**
   * include=stage_history 时附加：已到达状态 → 用户时区最早日历日 YYYY-MM-DD。
   * 工序线（StageTrail/StageRail）把它当 path 用，终态岗位也能画出灰色真实进度。
   */
  stage_history?: Record<string, string>
  /** include=stage_history 时附加：当前进度是在哪一天进入的（用户时区 YYYY-MM-DD）。 */
  progress_since?: string
}

export interface AppEvent {
  id: number
  sequence: number
  event_type: string
  from_status: string | null
  to_status: string | null
  /** 子状态的前后值（方案 §6.2）；旧事件为 null，按「未知」处理。 */
  from_substatus: string | null
  to_substatus: string | null
  /** 关联活动：assessment / interview + 轮次 id。 */
  activity_kind: string
  activity_id: number | null
  /** advance / rollback / reopen / correct —— 只用于展示标签，不决定权限。 */
  change_type: string
  note: string
  reason: string
  occurred_at: string
  recorded_at: string
  corrects_event_id: number | null
}

export interface FileItem {
  id: string
  name: string
  content_type: string
  size_bytes: number
  sha256: string
  status: string
  category: string
  application_id: number | null
  /** 归属的面试轮次（截图/附件按轮次归档）；无轮次为 null */
  interview_id: number | null
  created_at: string
}

export interface Interview {
  id: number
  application_id: number
  round_name: string
  format: string
  scheduled_at: string | null
  timezone: string
  duration_minutes: number | null
  result: string
  /** 活动进度：""(未细分) / awaiting_schedule / preparing / completed / cancelled。 */
  progress: string
  invited_at: string | null
  completed_at: string | null
  /** 完成但时间不详（方案 §3.2）——不伪造精确时间。 */
  completed_unknown: boolean
  feedback: string
  notes: string
  created_at: string
  schedule?: InterviewSchedule | null
}

/** OA / 作业轮次（方案 §3.2）：一轮一笔，保留此前提交时间与结果。 */
export interface AssessmentRound {
  id: number
  application_id: number
  /** online_test / take_home / other */
  kind: string
  name: string
  /** awaiting_schedule / preparing / completed / cancelled */
  progress: string
  /** unknown / passed / failed */
  result: string
  /** 收到邀请时间 */
  invited_at: string | null
  /** 计划开做时间 */
  planned_at: string | null
  /** 截止时间 */
  due_at: string | null
  completed_at: string | null
  completed_unknown: boolean
  link: string
  notes: string
  created_at: string
}

export interface ActionItem {
  id: number
  application_id: number | null
  title: string
  /** calendar day YYYY-MM-DD (DATE column) — render as a day, never new Date() */
  due_date: string | null
  /** precise instant (TIMESTAMPTZ column) */
  due_ts: string | null
  done_at: string | null
  remind_me: boolean
  priority: string
  created_at: string
}

export interface Note {
  id: number
  application_id: number
  content_md: string
  created_at: string
  updated_at: string
}

export interface SavedView {
  id: number
  name: string
  layout: string
  columns: unknown
  filter_ast: unknown
  sort: unknown
  group_by: unknown
  is_builtin: boolean
  schema_version: number
}

export interface PropertyDef {
  id: number
  name: string
  key: string
  data_type: string
  options: Array<{ id: string; label: string }> | unknown
  required: boolean
}

export interface ChannelRow {
  channel: string
  submitted: number
  responded: number
  response_rate: number | null
  interview_rate: number | null
  offer_rate: number | null
}

export interface Metrics {
  total_all: number
  to_apply: number
  submitted_count: number
  in_progress: number
  with_result: number
  by_status: Record<string, number>
  by_channel: ChannelRow[]
  response_rate: number | null
  interview_rate: number | null
  offer_rate: number | null
  responded: number
  reached_interview: number
  received_offer: number
  response_median_hours: number
  pending_response: number
  replied_sample: number
  denominator: number
  small_sample: boolean
  /** 方案 §5：OA 准备中 / 已完成等结果的具体数量（未细分的旧数据不计入）。 */
  preparing_assessment: number
  awaiting_oa_result: number
}

export interface SankeyData {
  nodes: Array<{ name: string; label?: string }>
  links: Array<{ source: string; target: string; value: number }>
  cohort_count: number
  as_of: string
  definition_version: number
  mode: string
  notes: string
  drilldown_token?: string
}

export interface Me {
  id: number
  email: string
  display_name: string
  timezone: string
  locale: string
  csrf_token?: string
}

// Preferences is the persisted account + reminder preference set from
// /api/v1/preferences (plan §5.1).
export interface Preferences {
  user_id: number
  display_name: string
  timezone: string
  week_start: number
  remind_overdue: boolean
  remind_interview: boolean
  remind_stale_days: number
  remind_weekly: boolean
  locale: string
}

// Notification is one in-app reminder row from /api/v1/notifications.
export interface Notification {
  id: number
  owner_id: number
  kind: 'overdue' | 'interview' | 'assessment_due' | 'stale' | 'weekly'
  title: string
  body: string
  application_id: number | null
  read_at: string | null
  dismissed_at: string | null
  created_at: string
}

// HomeSummary is the today dashboard payload (/api/v1/home/summary).
// Every count is computed server-side over the full dataset — never from a
// paginated page — and weeks are half-open [start, end) in the user's zone.
export interface HomeSummary {
  total: number
  active: number
  to_apply: number
  in_progress: number
  with_result: number
  archived: number
  submitted_week: number
  replied_week: number
  awaiting_reply: number
  interviews_week: number
  interviews_done_week: number
  todos: { open: number; overdue: number; due_today: number }
  todo_items: Array<{
    id: number
    action_id: number | null
    application_id: number
    title: string
    company_name: string
    position: string
    status: string
    /** calendar day YYYY-MM-DD when date-only due */
    due_day: string | null
    due_ts: string | null
  }>
  week: { start: string; end: string }
  /** user's 每周起始日 preference (0=周日..6=周六); strip day 0 = this weekday */
  week_start: number
  week_items: Array<{ day: number; kind: string; who: string; tone: 'info' | 'warn' | 'good' | 'acc' | 'bad' }>
  upcoming: Array<{
    id: number
    application_id: number
    company_name: string
    position: string
    round_name: string
    format: string
    scheduled_at: string
    timezone: string
    duration_minutes: number | null
    location: string
    meeting_url: string
    cancelled: boolean
    result: string
  }>
  /** 有计划时间的 OA / 作业轮次（跨申请，按计划时间排序） */
  upcoming_assessments: Array<{
    id: number
    application_id: number
    company_name: string
    position: string
    name: string
    kind: 'online_test' | 'take_home' | 'other'
    planned_at: string
    due_at: string | null
    progress: string
  }>
  recent: Array<{
    id: number
    company_name: string
    position: string
    status: string
    next_action: string
    updated_at: string
  }>
  as_of: string
  timezone: string
  scope_note: string
}

// InterviewSchedule is the per-interview scheduling metadata attached to an
// interview (meeting URL, contacts, cancellation).
export interface InterviewSchedule {
  meeting_url: string
  location: string
  contact_name: string
  contact_email: string
  notes: string
  cancelled: boolean
  cancelled_reason: string
  original_timezone: string
}

// CalendarEvent is one cross-application agenda row (/api/v1/calendar).
// `start` is the event instant (RFC3339). For all-day rows the server emits it
// at the *user's* local midnight of the calendar day, so the client can bucket
// it in the user zone consistently; date-only display should go through
// fmtDay/toDayString (never new Date() on a bare day).
export interface CalendarEvent {
  id: number
  kind: 'interview' | 'assessment' | 'assessment_due' | 'action' | 'deadline' | 'offer_decision'
  application_id: number
  company_name: string
  position: string
  title: string
  start: string | null
  timezone: string
  all_day: boolean
  location: string
  meeting_url: string
  cancelled: boolean
  done: boolean
  round_name: string
  format: string
}
