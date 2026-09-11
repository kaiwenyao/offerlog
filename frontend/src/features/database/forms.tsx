import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import { toInstantInUserZone } from '../../lib/tz'
import type { AppRow } from '../../lib/types'
import { NEXT_STEP_SUGGESTION } from '../../lib/status'
import { Button, Checkbox, Input, Select, Textarea } from '../../ds'
import { ErrorText, Modal, Spinner } from '../../components/ui'

/** Shared with the progress dialog's inline scheduler — keep one list. */
export const ROUNDS = ['一面', '二面', '三面', '终面', '技术面', 'HR 面', '其他']
export const FORMATS = [
  { value: 'phone', label: '电话' },
  { value: 'video', label: '视频' },
  { value: 'onsite', label: '到面' },
  { value: 'takehome', label: '作业' },
]

export function InterviewForm({
  appId,
  onClose,
  onDone,
}: {
  appId: number
  onClose: () => void
  onDone: () => void
}) {
  const [roundName, setRoundName] = useState('一面')
  const [format, setFormat] = useState('video')
  const [scheduled, setScheduled] = useState('')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: () => {
      // The datetime-local value is naive wall-clock; toInstantInUserZone reads
      // it in the user's configured zone and hands back the zone label to stamp
      // on the round, so the stored interview keeps a truthful timezone tag.
      let scheduledAt: string | null = null
      let zoneLabel = ''
      if (scheduled) {
        const { iso, zone } = toInstantInUserZone(scheduled)
        if (iso == null) {
          setErr('时间格式不正确')
          return Promise.reject(new ApiError('bad_scheduled_at', '时间格式不正确', 400))
        }
        scheduledAt = iso
        zoneLabel = zone
      }
      return api.post(`/api/v1/applications/${appId}/interviews`, {
        round_name: roundName,
        format,
        scheduled_at: scheduledAt,
        timezone: zoneLabel,
      })
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="安排面试"
      onClose={onClose}
      footer={
        <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
          {mut.isPending ? <Spinner size={14} /> : '保存'}
        </Button>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Select label="轮次" options={ROUNDS} value={roundName} onChange={(e) => setRoundName(e.target.value)} />
        <Select label="形式" options={FORMATS} value={format} onChange={(e) => setFormat(e.target.value)} />
        <Input
          label="时间"
          type="datetime-local"
          value={scheduled}
          onChange={(e) => setScheduled(e.target.value)}
        />
      </div>
    </Modal>
  )
}

export function ActionForm({ app, onClose, onDone }: { app: AppRow; onClose: () => void; onDone: () => void }) {
  const [title, setTitle] = useState(app.next_action || NEXT_STEP_SUGGESTION[app.status] || '')
  // next_action_due_at is a date-only YYYY-MM-DD string (never a timestamp).
  const [due, setDue] = useState(app.next_action_due_at ?? '')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: async () => {
      // The standalone action is the source of truth for the unified todo
      // list (§5.3). Creating one also mirrors it onto the application's
      // legacy next_action fields so older surfaces (table column, list
      // view) stay in sync; completing/undoing happens on the action row.
      //
      // due_date is a calendar day: send it as the plain YYYY-MM-DD string
      // (never a browser-local-midnight instant — that shifts the stored day
      // for non-UTC users). The backend stores it in a DATE column.
      const created = await api.post<{ id: number }>(`/api/v1/applications/${app.id}/actions`, {
        title,
        due_date: due || null,
        priority: app.priority,
      })
      // Mirror onto the row (best-effort; the action is authoritative).
      try {
        const fresh = await api.get<AppRow>(`/api/v1/applications/${app.id}`)
        await api.patch(`/api/v1/applications/${app.id}`, {
          version: fresh.version,
          next_action: title || null,
          next_action_due_at: due || null,
        })
      } catch {
        /* the standalone action still exists — surface stays consistent via it */
      }
      return created
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="设置下一步行动"
      onClose={onClose}
      footer={
        <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
          {mut.isPending ? <Spinner size={14} /> : '保存'}
        </Button>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Input label="行动内容" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="例如：准备二面" />
        <Input label="截止日期" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
      </div>
    </Modal>
  )
}

export function NoteForm({ appId, onClose, onDone }: { appId: number; onClose: () => void; onDone: () => void }) {
  const [content, setContent] = useState('')
  const [err, setErr] = useState('')
  const mut = useMutation({
    mutationFn: () => api.post(`/api/v1/applications/${appId}/notes`, { content_md: content }),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })
  return (
    <Modal
      title="记录备注"
      onClose={onClose}
      footer={
        <Button variant="primary" size="sm" disabled={!content.trim() || mut.isPending} onClick={() => mut.mutate()}>
          {mut.isPending ? <Spinner size={14} /> : '保存'}
        </Button>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <Textarea
        rows={6}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder="沟通要点、联系人、后续计划…"
      />
    </Modal>
  )
}

/** 测评类型（方案 §3.2）：作业显示「作业」，不强迫都叫 OA。 */
export const ASSESSMENT_KIND_OPTIONS = [
  { value: 'online_test', label: '在线测试 (OA)' },
  { value: 'take_home', label: 'Take-home 作业' },
  { value: 'other', label: '其他测评' },
]

export const ASSESSMENT_PROGRESS_OPTIONS = [
  { value: 'preparing', label: '准备中' },
  { value: 'completed', label: '已完成 · 等结果' },
]

/**
 * 新增一轮 OA / 作业（方案 §3.2）。收到、计划、截止、完成四种时间各自保存，
 * 互不覆盖；完成时间不详时只标「已完成」，不伪造精确时间。
 */
export function AssessmentForm({
  appId,
  suggestedName,
  onClose,
  onDone,
}: {
  appId: number
  /** 已有轮次数决定默认名称：第一轮 OA，之后 OA 2、OA 3… */
  suggestedName?: string
  onClose: () => void
  onDone: () => void
}) {
  const [kind, setKind] = useState('online_test')
  const [name, setName] = useState(suggestedName || 'OA')
  const [progress, setProgress] = useState('preparing')
  const [invited, setInvited] = useState('')
  const [planned, setPlanned] = useState('')
  const [due, setDue] = useState('')
  const [completed, setCompleted] = useState('')
  const [completedUnknown, setCompletedUnknown] = useState(false)
  const [link, setLink] = useState('')
  const [notes, setNotes] = useState('')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: () => {
      const at = (v: string) => {
        if (!v) return null
        const { iso } = toInstantInUserZone(v)
        if (iso == null) throw new ApiError('bad_time', '时间格式不正确', 400)
        return iso
      }
      return api.post(`/api/v1/applications/${appId}/assessments`, {
        kind,
        name,
        progress,
        result: 'unknown',
        invited_at: at(invited),
        planned_at: at(planned),
        due_at: at(due),
        completed_at: progress === 'completed' ? at(completed) : null,
        completed_unknown: progress === 'completed' ? completedUnknown || !completed : false,
        link,
        notes,
      })
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="新增一轮测评"
      onClose={onClose}
      footer={
        <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
          {mut.isPending ? <Spinner size={14} /> : '保存'}
        </Button>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Select
          label="类型"
          options={ASSESSMENT_KIND_OPTIONS}
          value={kind}
          onChange={(e) => {
            setKind(e.target.value)
            setName((n) => (n === 'OA' || n.startsWith('OA ') ? (e.target.value === 'take_home' ? '作业' : 'OA') : n))
          }}
        />
        <Input label="名称" value={name} onChange={(e) => setName(e.target.value)} />
        <Select
          label="当前进度"
          options={ASSESSMENT_PROGRESS_OPTIONS}
          value={progress}
          onChange={(e) => setProgress(e.target.value)}
        />
        <Input label="收到邀请" type="datetime-local" value={invited} onChange={(e) => setInvited(e.target.value)} />
        <Input label="计划开做" type="datetime-local" value={planned} onChange={(e) => setPlanned(e.target.value)} />
        <Input
          label="截止时间"
          type="datetime-local"
          value={due}
          onChange={(e) => setDue(e.target.value)}
          hint="完成后不再提醒这个截止时间"
        />
        {progress === 'completed' && (
          <>
            <Input
              label="完成时间"
              type="datetime-local"
              value={completed}
              onChange={(e) => setCompleted(e.target.value)}
              hint="留空则标为「完成时间不详」"
            />
            <Checkbox
              label="完成时间不详（只标「已完成」，不伪造精确时间）"
              checked={completedUnknown}
              onChange={setCompletedUnknown}
            />
          </>
        )}
        <Input label="测试链接" value={link} onChange={(e) => setLink(e.target.value)} />
        <Textarea label="备注" value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} />
      </div>
    </Modal>
  )
}
