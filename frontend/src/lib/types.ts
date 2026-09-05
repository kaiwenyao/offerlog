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
}

export interface ActionItem {
  id: number
  application_id: number | null
  title: string
  due_date: string | null
  due_ts: string | null
  done_at: string | null
  remind_me: boolean
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
  response_rate: number
  interview_rate: number
  offer_rate: number
}

export interface Metrics {
  total_all: number
  to_apply: number
  submitted_count: number
  in_progress: number
  with_result: number
  by_status: Record<string, number>
  by_channel: ChannelRow[]
  response_rate: number
  interview_rate: number
  offer_rate: number
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
}

export interface Me {
  id: number
  email: string
  display_name: string
  timezone: string
  locale: string
  csrf_token?: string
}
