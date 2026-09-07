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
  priority: string
  tags: string[]
  custom_values: Record<string, unknown>
  notes: string
  saved_at: string | null
  submitted_at: string | null
  first_response_at: string | null
  deadline: string | null
  accepted_at: string | null
  rejected_at: string | null
  reason: string
  next_action: string
  next_action_due_at: string | null
  version: number
  archived: boolean
  deleted: boolean
  created_at: string
  updated_at: string
}

export interface AppEvent {
  id: number
  sequence: number
  event_type: string
  from_status: string | null
  to_status: string | null
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
  feedback: string
  notes: string
  created_at: string
  schedule?: InterviewSchedule | null
}

export interface ActionItem {
  id: number
  application_id: number | null
  title: string
  due_date: string | null
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
  kind: 'overdue' | 'interview' | 'stale' | 'weekly'
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
  week: { start: string; end: string }
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
export interface CalendarEvent {
  id: number
  kind: 'interview' | 'action' | 'deadline' | 'offer_decision'
  application_id: number
  company_name: string
  position: string
  title: string
  start: string | null
  dueDate?: string | null
  timezone: string
  all_day: boolean
  location: string
  meeting_url: string
  cancelled: boolean
  done: boolean
  round_name: string
  format: string
}
