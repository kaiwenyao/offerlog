import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import React from 'react'
import { api, ApiError, fmtBytes, fmtDate, fmtDateTime } from '../../lib/api'
import type { ActionItem, AppEvent, AppRow, FileItem, Interview, Note } from '../../lib/types'
import { STATUSES, statusMeta, NEXT_STEP_SUGGESTION, ENDED } from '../../lib/status'
import { StatusChip, Spinner, Modal } from '../../components/ui'
import { StageTrail } from '../../components/StageTrail'

// AppDetailContent renders the full detail panel content; used inside the
// drawer overlay and the full-page route.
export function AppDetailContent({ appId, onClose, embedded }: { appId: number; onClose: () => void; embedded?: boolean }) {
  const qc = useQueryClient()
  const [tab, setTab] = useState<'overview' | 'files' | 'timeline'>('overview')
  const [showTransition, setShowTransition] = useState(false)
  const appQ = useQuery({
    queryKey: ['app', appId],
    queryFn: () => api.get<AppRow>(`/api/v1/applications/${appId}`),
  })
  const eventsQ = useQuery({
    queryKey: ['events', appId],
    queryFn: () => api.get<{ items: AppEvent[] }>(`/api/v1/applications/${appId}/events`),
  })
  const filesQ = useQuery({
    queryKey: ['files', appId],
    queryFn: () => api.get<{ items: FileItem[] }>('/api/v1/files?application_id=' + appId),
  })
  const interviewsQ = useQuery({
    queryKey: ['interviews', appId],
    queryFn: () => api.get<{ items: Interview[] }>(`/api/v1/applications/${appId}/interviews`),
  })
  const notesQ = useQuery({
    queryKey: ['notes', appId],
    queryFn: () => api.get<{ items: Note[] }>(`/api/v1/applications/${appId}/notes`),
  })

  const app = appQ.data
  const events = eventsQ.data?.items ?? []
  const files = filesQ.data?.items ?? []
  const interviews = interviewsQ.data?.items ?? []
  const notes = notesQ.data?.items ?? []

  const path = useMemo(() => {
    const p: string[] = []
    for (const e of events) {
      if (e.event_type === 'created' && e.to_status) p.push(e.to_status)
      else if (e.event_type === 'status_change' && e.to_status) p.push(e.to_status)
    }
    return p
  }, [events])

  if (!app) return appQ.isError ? (
    <div className="drawer-backdrop" onClick={onClose}><div className="drawer" onClick={(e) => e.stopPropagation()}>加载失败</div></div>
  ) : <Spinner />

  const head = (
    <>
      <div className="grow">
        <div className="drawer-title">
          {app.position} <span className="muted" style={{ fontWeight: 400 }}>· {app.company_name}</span>
        </div>
        <div className="row mt8" style={{ gap: 8 }}>
          <StatusChip status={app.status} />
          {app.channel && <span className="small muted">{app.channel}</span>}
        </div>
      </div>
      <div className="row">
        {!embedded && <Link className="btn btn-ghost btn-small" to={`/apps/${app.id}`}>完整详情 ↗</Link>}
        {!embedded && <RowMenu appId={app.id} app={app} onChanged={() => { qc.invalidateQueries(); onClose() }} />}
        <button className="btn btn-ghost btn-small" onClick={onClose} aria-label="关闭">✕</button>
      </div>
    </>
  )
  const body = (
    <div className="drawer-body">
          <StageTrail current={app.status} path={path} />
          <div className="row mt8" style={{ gap: 12, flexWrap: 'wrap' }}>
            {app.location && <span className="small muted">📍 {app.location}</span>}
            {app.remote_policy && <span className="small muted">{app.remote_policy}</span>}
            {app.employment_type && <span className="small muted">{app.employment_type}</span>}
            {app.salary_max != null && (
              <span className="small muted num">
                💰 {app.salary_min ?? '—'}–{app.salary_max} {app.salary_currency}
              </span>
            )}
            {app.deadline && <span className="small muted">截止 {fmtDate(app.deadline)}</span>}
            {app.submitted_at && <span className="small muted">投递 {fmtDate(app.submitted_at)}</span>}
            {app.first_response_at && <span className="small muted">首次回复 {fmtDate(app.first_response_at)}</span>}
          </div>
          {app.job_url && (
            <div className="mt8">
              <a className="small" href={app.job_url} target="_blank" rel="noreferrer">查看岗位链接 ↗</a>
            </div>
          )}
          {app.reason && (
            <div className="card mt8 small" style={{ padding: 8 }}>
              <b>原因：</b>
              {app.reason}
            </div>
          )}

          <div className="row mt16" role="tablist">
            {(['overview', 'files', 'timeline'] as const).map((t) => (
              <button key={t} role="tab" aria-selected={tab === t} className={'btn btn-ghost btn-small ' + (tab === t ? 'active-layout' : '')} onClick={() => setTab(t)}>
                {t === 'overview' ? '概览' : t === 'files' ? `附件 (${files.length})` : '时间线'}
              </button>
            ))}
            <span className="grow" />
            <button className="btn btn-primary btn-small" disabled={ENDED.has(app.status)} onClick={() => setShowTransition(true)}>
              更新进度
            </button>
          </div>

          {tab === 'overview' && (
            <Overview
              app={app}
              interviews={interviews}
              notes={notes}
              filesCount={files.length}
              refetchAll={() => {
                qc.invalidateQueries({ queryKey: ['app', appId] })
                qc.invalidateQueries({ queryKey: ['events', appId] })
                qc.invalidateQueries({ queryKey: ['interviews', appId] })
              }}
            />
          )}
          {tab === 'files' && (
            <FilesTab appId={app.id} files={files} />
          )}
          {tab === 'timeline' && (
            <TimelineTab appId={app.id} events={events} status={app.status} />
          )}
    </div>
  )
  if (embedded) {
    return (
      <div>
        <div className="row" style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>{head}</div>
        <div className="mt8">{body}</div>
        {showTransition && (
          <TransitionModal appId={app.id} currentStatus={app.status} version={app.version} onClose={() => setShowTransition(false)} />
        )}
      </div>
    )
  }
  return (
    <div className="drawer-backdrop" onClick={onClose}>
      <aside className="drawer" role="dialog" aria-modal="true" aria-label="岗位详情" onClick={(e) => e.stopPropagation()}>
        <div className="drawer-head">{head}</div>
        {body}
        {showTransition && (
          <TransitionModal appId={app.id} currentStatus={app.status} version={app.version} onClose={() => setShowTransition(false)} />
        )}
      </aside>
    </div>
  )
}

export function Drawer({ appId, onClose }: { appId: number; onClose: () => void }) {
  return <AppDetailContent appId={appId} onClose={onClose} />
}

// RowMenu exposes archive / unarchive / soft-delete / restore (trash & archive
// are visibility flags, not statuses — plan §2.2).
function RowMenu({ appId, app, onChanged }: { appId: number; app: AppRow; onChanged: () => void }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [err, setErr] = useState('')
  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      setOpen(false)
      qc.invalidateQueries({ queryKey: ['app', appId] })
      onChanged()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '操作失败')
    }
  }
  void qc
  return (
    <div style={{ position: 'relative' }}>
      <button className="btn btn-ghost btn-small" aria-label="更多操作" onClick={() => setOpen((v) => !v)}>⋯</button>
      {open && (
        <>
          <div style={{ position: 'fixed', inset: 0 }} onClick={() => setOpen(false)} />
          <div className="card" style={{ position: 'absolute', right: 0, top: '100%', zIndex: 60, minWidth: 150, padding: 6 }}>
            {err && <p role="alert" className="err small">{err}</p>}
            {!app.archived ? (
              <button className="menu-item" onClick={() => act(() => api.post(`/api/v1/applications/${appId}/archive`))}>🗄 归档</button>
            ) : (
              <button className="menu-item" onClick={() => act(() => api.post(`/api/v1/applications/${appId}/unarchive`))}>📂 取消归档</button>
            )}
            {!app.deleted ? (
              <button className="menu-item danger" onClick={() => act(() => api.del(`/api/v1/applications/${appId}`))}>🗑 移到回收站</button>
            ) : (
              <button className="menu-item" onClick={() => act(() => api.post(`/api/v1/applications/${appId}/restore`))}>♻️ 恢复</button>
            )}
          </div>
        </>
      )}
    </div>
  )
}

function Overview({
  app,
  interviews,
  notes,
  filesCount,
  refetchAll,
}: {
  app: AppRow
  interviews: Interview[]
  notes: Note[]
  filesCount: number
  refetchAll: () => void
}) {
  const qc = useQueryClient()
  const [showInterview, setShowInterview] = useState(false)
  const [showAction, setShowAction] = useState(false)
  const [showNote, setShowNote] = useState(false)

  const nextStep = NEXT_STEP_SUGGESTION[app.status]

  return (
    <div>
      <div className="card mt16" style={{ padding: 12 }}>
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <b>下一步行动</b>
          <button className="btn btn-ghost btn-small" onClick={() => setShowAction(true)}>＋ 添加</button>
        </div>
        {app.next_action ? (
          <p className="mb8">
            {app.next_action}
            {app.next_action_due_at && <span className="small muted"> · 截止 {fmtDate(app.next_action_due_at)}</span>}
          </p>
        ) : (
          <p className="muted small">{nextStep ?? '填写下一步行动以在今日待办中提醒自己'}</p>
        )}
        {app.notes && (
          <>
            <hr className="hr" />
            <p className="small" style={{ whiteSpace: 'pre-wrap' }}>{app.notes}</p>
          </>
        )}
      </div>

      <div className="card mt16" style={{ padding: 12 }}>
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <b>面试 ({interviews.length})</b>
          <button className="btn btn-ghost btn-small" onClick={() => setShowInterview(true)}>＋ 安排</button>
        </div>
        {interviews.length === 0 ? (
          <p className="muted small">还没有面试安排</p>
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
            {interviews.map((i) => (
              <li key={i.id} className="small" style={{ padding: '6px 0', borderBottom: '1px solid #f0f3f7' }}>
                <b>{i.round_name || '面试'}</b> {i.format && `· ${i.format}`}
                <div className="muted">{i.scheduled_at ? fmtDateTime(i.scheduled_at) : '时间未定'} · {i.result || '待定'}</div>
                {i.feedback && <div className="muted">反馈：{i.feedback}</div>}
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="card mt16" style={{ padding: 12 }}>
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <b>备注 ({notes.length})</b>
          <button className="btn btn-ghost btn-small" onClick={() => setShowNote(true)}>＋ 记录</button>
        </div>
        {notes.length === 0 ? (
          <p className="muted small">记录沟通要点、联系方式等</p>
        ) : (
          notes.map((n) => (
            <div key={n.id} className="small" style={{ padding: '6px 0', borderBottom: '1px solid #f0f3f7', whiteSpace: 'pre-wrap' }}>
              {n.content_md}
              <div className="muted">{fmtDateTime(n.created_at)}</div>
            </div>
          ))
        )}
        <p className="small muted mt8">附件 {filesCount} 个 — 在「附件」标签页管理</p>
      </div>

      {showInterview && (
        <InterviewForm appId={app.id} onClose={() => setShowInterview(false)} onDone={() => { refetchAll(); setShowInterview(false) }} />
      )}
      {showAction && (
        <ActionForm app={app} onClose={() => setShowAction(false)} onDone={() => { qc.invalidateQueries(); setShowAction(false) }} />
      )}
      {showNote && (
        <NoteForm appId={app.id} onClose={() => setShowNote(false)} onDone={() => { qc.invalidateQueries(); setShowNote(false) }} />
      )}
    </div>
  )
}

function InterviewForm({ appId, onClose, onDone }: { appId: number; onClose: () => void; onDone: () => void }) {
  const [roundName, setRoundName] = useState('一面')
  const [format, setFormat] = useState('video')
  const [scheduled, setScheduled] = useState('')
  const [err, setErr] = useState('')
  const mut = useMutation({
    mutationFn: () =>
      api.post(`/api/v1/applications/${appId}/interviews`, {
        round_name: roundName,
        format,
        scheduled_at: scheduled ? new Date(scheduled).toISOString() : null,
      }),
    onSuccess: onDone,
    onError: (e) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })
  return (
    <Modal title="安排面试" onClose={onClose} footer={<button className="btn btn-primary" onClick={() => mut.mutate()} disabled={mut.isPending}>{mut.isPending ? <Spinner /> : '保存'}</button>}>
      {err && <p role="alert" className="err">{err}</p>}
      <div className="fgrid">
        <div className="fld">
          <label>轮次</label>
          <select className="select" value={roundName} onChange={(e) => setRoundName(e.target.value)}>
            {['一面', '二面', '三面', '终面', '技术面', 'HR 面', '其他'].map((r) => (
              <option key={r}>{r}</option>
            ))}
          </select>
        </div>
        <div className="fld">
          <label>形式</label>
          <select className="select" value={format} onChange={(e) => setFormat(e.target.value)}>
            {[['phone', '电话'], ['video', '视频'], ['onsite', '到面'], ['takehome', '作业']].map(([v, l]) => (
              <option key={v} value={v}>{l}</option>
            ))}
          </select>
        </div>
        <div className="fld full">
          <label>时间</label>
          <input className="input" type="datetime-local" value={scheduled} onChange={(e) => setScheduled(e.target.value)} />
        </div>
      </div>
    </Modal>
  )
}

function ActionForm({ app, onClose, onDone }: { app: AppRow; onClose: () => void; onDone: () => void }) {
  const [title, setTitle] = useState(app.next_action || NEXT_STEP_SUGGESTION[app.status] || '')
  const [due, setDue] = useState(app.next_action_due_at ? app.next_action_due_at.slice(0, 10) : '')
  const [err, setErr] = useState('')
  const mut = useMutation({
    mutationFn: async () => {
      // Save onto the application row (today dashboard reads next_action) and
      // create a standalone action for the checklist. Fetch the latest version
      // first so an earlier edit elsewhere does not cause a 409 conflict.
      const fresh = await api.get<AppRow>(`/api/v1/applications/${app.id}`)
      const patch: Record<string, unknown> = {
        version: fresh.version,
        next_action: title || null,
        next_action_due_at: due ? new Date(due + 'T00:00:00').toISOString() : null,
      }
      await api.patch(`/api/v1/applications/${app.id}`, patch)
      if (title) {
        await api.post(`/api/v1/applications/${app.id}/actions`, {
          title,
          due_date: due ? new Date(due + 'T00:00:00').toISOString() : null,
        })
      }
    },
    onSuccess: onDone,
    onError: (e) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })
  return (
    <Modal title="设置下一步行动" onClose={onClose} footer={<button className="btn btn-primary" onClick={() => mut.mutate()} disabled={mut.isPending}>{mut.isPending ? <Spinner /> : '保存'}</button>}>
      {err && <p role="alert" className="err">{err}</p>}
      <div className="fgrid">
        <div className="fld full">
          <label>行动内容</label>
          <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="例如：准备二面" />
        </div>
        <div className="fld full">
          <label>截止日期</label>
          <input className="input" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
        </div>
      </div>
    </Modal>
  )
}

function NoteForm({ appId, onClose, onDone }: { appId: number; onClose: () => void; onDone: () => void }) {
  const [content, setContent] = useState('')
  const mut = useMutation({
    mutationFn: () => api.post(`/api/v1/applications/${appId}/notes`, { content_md: content }),
    onSuccess: onDone,
  })
  return (
    <Modal title="记录备注" onClose={onClose} footer={<button className="btn btn-primary" disabled={!content.trim() || mut.isPending} onClick={() => mut.mutate()}>{mut.isPending ? <Spinner /> : '保存'}</button>}>
      <textarea className="input" rows={6} value={content} onChange={(e) => setContent(e.target.value)} placeholder="沟通要点、联系人、后续计划…" />
    </Modal>
  )
}

function FilesTab({ appId, files }: { appId: number; files: FileItem[] }) {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const [uploading, setUploading] = useState(false)
  const upMut = useMutation({
    mutationFn: async (file: File) => {
      const fd = new FormData()
      fd.append('file', file)
      fd.append('category', file.name.toLowerCase().endsWith('.pdf') || /\.(docx?|txt)$/i.test(file.name) ? 'resume' : 'other')
      fd.append('application_id', String(appId))
      return api.post('/api/v1/files', fd, true)
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e) => setErr(e instanceof ApiError ? e.message : '上传失败'),
  })
  return (
    <div className="mt16">
      {err && <p role="alert" className="err">{err}</p>}
      <label className="btn btn-ghost btn-small" style={{ cursor: 'pointer' }}>
        {uploading ? '上传中…' : '＋ 上传附件'}
        <input
          type="file"
          style={{ display: 'none' }}
          accept=".pdf,.docx,.txt,.png,.jpg,.jpeg"
          onChange={async (e) => {
            const f = e.target.files?.[0]
            if (!f) return
            setUploading(true)
            try {
              await upMut.mutateAsync(f)
            } finally {
              setUploading(false)
              e.target.value = ''
            }
          }}
        />
      </label>
      <p className="small muted mt8">允许 PDF / DOCX / TXT / PNG / JPEG，单文件 ≤ 20 MiB</p>
      {files.length === 0 ? (
        <p className="muted small">还没有附件。上传简历版本、JD、Offer 文件等。</p>
      ) : (
        <ul style={{ listStyle: 'none', padding: 0 }}>
          {files.map((f) => (
            <li key={f.id} className="card row" style={{ padding: 10, marginTop: 8, justifyContent: 'space-between' }}>
              <div className="grow">
                <div style={{ fontWeight: 600 }}>{f.name}</div>
                <div className="small muted">
                  {fmtBytes(f.size_bytes)} · {f.category === 'resume' ? '简历' : '其他'} · {fmtDate(f.created_at)}
                </div>
                {f.status !== 'ready' && <div className="small err">状态：{f.status}</div>}
              </div>
              <div className="row">
                {f.status === 'ready' && (
                  <a className="btn btn-ghost btn-small" href={`/api/v1/files/${f.id}/download`} download>
                    下载
                  </a>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function TimelineTab({
  appId,
  events,
  status,
}: {
  appId: number
  events: AppEvent[]
  status: string
}) {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const [pick, setPick] = useState<AppEvent | null>(null)
  const [newStatus, setNewStatus] = useState('')
  const correctMut = useMutation({
    mutationFn: async () => {
      if (!pick) return
      await api.post(`/api/v1/applications/${appId}/corrections`, {
        event_id: pick.id,
        new_status: newStatus,
        occurred_at: pick.occurred_at,
        reason: '纠错',
      })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['events', appId] })
      qc.invalidateQueries({ queryKey: ['app', appId] })
      setPick(null)
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : '纠正失败'),
  })
  return (
    <div className="mt16">
      {err && <p role="alert" className="err">{err}</p>}
      <p className="small muted">当前状态：{statusMeta(status).label}。每次真实变化都有审计记录。</p>
      <ol style={{ listStyle: 'none', padding: 0, position: 'relative' }}>
        {events.map((e) => (
          <li key={e.id} style={{ padding: '8px 0 8px 20px', borderLeft: '2px solid #e5eaf0', marginLeft: 8 }}>
            <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
              {e.event_type === 'created' && <b>创建记录</b>}
              {e.event_type === 'correction' && <b>✏️ 纠正</b>}
              {e.event_type === 'status_change' && (
                <span className="row" style={{ gap: 6 }}>
                  {e.from_status && <StatusChip status={e.from_status} />}
                  <span aria-hidden>→</span>
                  {e.to_status && <StatusChip status={e.to_status} />}
                </span>
              )}
              <span className="small muted">{fmtDateTime(e.occurred_at)}</span>
            </div>
            {e.note && <div className="small">{e.note.replace(/^\|idem:.*/, '')}</div>}
            {e.reason && <div className="small muted">原因：{e.reason}</div>}
            {e.corrects_event_id && (
              <div className="small muted">纠正了事件 #{e.corrects_event_id}</div>
            )}
            {e.event_type === 'status_change' && e.to_status && status !== e.to_status && (
              <button
                className="btn btn-ghost btn-small mt8"
                onClick={() => {
                  setPick(e)
                  setNewStatus(e.to_status ?? 'saved')
                }}
              >
                纠正此记录
              </button>
            )}
          </li>
        ))}
      </ol>
      {pick && (
        <Modal
          title="纠正历史事件"
          onClose={() => setPick(null)}
          footer={<button className="btn btn-primary" onClick={() => correctMut.mutate()} disabled={correctMut.isPending}>{correctMut.isPending ? <Spinner /> : '确认纠正'}</button>}
        >
          <p className="small">将事件 #{pick.id} 的目标状态纠正为：</p>
          <select className="select" value={newStatus} onChange={(e) => setNewStatus(e.target.value)}>
            {STATUSES.map((s) => (
              <option key={s.key} value={s.key}>{s.label}</option>
            ))}
          </select>
          <p className="small muted mt8">纠正会保留审计痕迹（corrects_event_id），并重新计算当前状态。</p>
        </Modal>
      )}
    </div>
  )
}

function TransitionModal({
  appId,
  currentStatus,
  version,
  onClose,
}: {
  appId: number
  currentStatus: string
  version: number
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [to, setTo] = useState('')
  const [occurredAt, setOccurredAt] = useState('')
  const [reason, setReason] = useState('')
  const [note, setNote] = useState('')
  const [submittedAt, setSubmittedAt] = useState('')
  const [err, setErr] = useState('')
  const allowed = allowedTargets(currentStatus)
  const mut = useMutation({
    mutationFn: () =>
      api.post(`/api/v1/applications/${appId}/transitions`, {
        to_status: to,
        version,
        occurred_at: occurredAt ? new Date(occurredAt).toISOString() : null,
        reason,
        note,
        submitted_at: submittedAt ? new Date(submittedAt).toISOString() : null,
        idempotency_key: `ui-${Date.now()}`,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app', appId] })
      qc.invalidateQueries({ queryKey: ['events', appId] })
      qc.invalidateQueries({ queryKey: ['apps'] })
      onClose()
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : '更新失败'),
  })
  const needsReason =
    ['rejected', 'withdrawn', 'closed'].includes(to) ||
    (['accepted', 'rejected', 'withdrawn', 'closed'].includes(currentStatus) && !['accepted', 'rejected', 'withdrawn', 'closed'].includes(to))
  const needsSubmitted = ['applied', 'screening', 'assessment', 'interviewing'].includes(to)
  return (
    <Modal
      title="更新进度"
      onClose={onClose}
      footer={
        <>
          <button className="btn btn-ghost" onClick={onClose}>取消</button>
          <button className="btn btn-primary" disabled={!to || mut.isPending} onClick={() => mut.mutate()}>
            {mut.isPending ? <Spinner /> : '确认更新'}
          </button>
        </>
      }
    >
      {err && <p role="alert" className="err">{err}</p>}
      <div className="fgrid">
        <div className="fld">
          <label>当前</label>
          <StatusChip status={currentStatus} />
        </div>
        <div className="fld">
          <label>目标状态 *</label>
          <select className="select" value={to} onChange={(e) => setTo(e.target.value)}>
            <option value="">选择…</option>
            {allowed.map((s) => (
              <option key={s.key} value={s.key}>{s.label}</option>
            ))}
          </select>
        </div>
        <div className="fld full">
          <label>发生时间（可选，默认现在）</label>
          <input className="input" type="datetime-local" value={occurredAt} onChange={(e) => setOccurredAt(e.target.value)} />
        </div>
        {needsSubmitted && (
          <div className="fld full">
            <label>实际投递时间</label>
            <input className="input" type="datetime-local" value={submittedAt} onChange={(e) => setSubmittedAt(e.target.value)} />
            <span className="small muted">进入招聘阶段需要补充投递时间（或标记未经正式投递）</span>
          </div>
        )}
        {(needsReason || to === 'rejected' || to === 'withdrawn') && (
          <div className="fld full">
            <label>原因 *</label>
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="必填" />
          </div>
        )}
        <div className="fld full">
          <label>说明（可选）</label>
          <input className="input" value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
      </div>
    </Modal>
  )
}

function allowedTargets(from: string) {
  const map: Record<string, string[]> = {
    saved: ['preparing', 'applied', 'withdrawn', 'closed'],
    preparing: ['applied', 'screening', 'withdrawn', 'closed', 'saved'],
    applied: ['screening', 'assessment', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
    screening: ['assessment', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
    assessment: ['screening', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
    interviewing: ['screening', 'offer', 'rejected', 'withdrawn', 'closed'],
    offer: ['accepted', 'rejected', 'withdrawn', 'closed'],
    accepted: ['offer', 'rejected', 'withdrawn'],
    rejected: ['saved', 'preparing', 'applied', 'screening', 'interviewing', 'offer'],
    withdrawn: ['saved', 'preparing', 'applied', 'screening', 'interviewing', 'offer'],
    closed: ['saved', 'preparing', 'applied', 'screening', 'interviewing', 'offer'],
  }
  const keys = map[from] ?? []
  return STATUSES.filter((s) => keys.includes(s.key))
}
