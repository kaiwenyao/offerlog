import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtBytes, fmtDate, fmtDateTime, fmtDay, toDayString } from '../../lib/api'
import { effectiveZone } from '../../lib/tz'
import type { ActionItem, AppRow, AssessmentRound, FileItem, Interview, Note } from '../../lib/types'
import { comboLabel, NEXT_STEP_SUGGESTION } from '../../lib/status'
import { Button, Card, Eyebrow, LinkButton, PanelTitle } from '../../ds'
import { Icon } from '../../components/Icon'
import { ErrorText, Num, Spinner } from '../../components/ui'
import { ActionForm, AssessmentForm, InterviewForm, NoteForm } from './forms'

const ACCEPTED_UPLOADS = '.pdf,.docx,.txt,.png,.jpg,.jpeg'

/**
 * 一轮活动的状态标签：进度（待安排 / 准备中 / 已完成 / 已取消）优先于结果，
 * 因为「面完了但结果未知」是常见且必须能表达的状态（方案 §3.3）；只有结果真的
 * 确定时才显示通过 / 未通过。
 */
function activityProgressLabel(progress: string, result: string, scheduled: boolean): string {
  if (result === 'passed') return '已通过'
  if (result === 'failed') return '未通过'
  if (progress === 'completed') return '已完成·等反馈'
  if (progress === 'cancelled') return '已取消'
  if (progress === 'preparing') return '准备中'
  return scheduled ? '待进行' : '待安排'
}

/** YYYY-MM-DD strictly before today's YYYY-MM-DD in the USER zone. */
function dayBeforeToday(dayStr: string): boolean {
  const today = toDayString(new Date().toISOString(), effectiveZone()) ?? ''
  return dayStr < today
}

const EXT_TINT: Record<string, string> = {
  PDF: 'var(--danger-soft)',
  DOC: 'var(--info-soft)',
  TXT: 'var(--surface-thin)',
  MD: 'var(--surface-thin)',
  PNG: 'var(--positive-soft)',
  JPG: 'var(--positive-soft)',
  ZIP: 'var(--warning-soft)',
}

export function fileExt(name: string): string {
  const dot = name.lastIndexOf('.')
  const raw = dot === -1 ? 'FILE' : name.slice(dot + 1).toUpperCase()
  return raw === 'JPEG' ? 'JPG' : raw === 'DOCX' ? 'DOC' : raw.slice(0, 4)
}

export function FileTile({ name, size = 24 }: { name: string; size?: number }) {
  const ext = fileExt(name)
  return (
    <span
      aria-hidden
      style={{
        width: size,
        height: size,
        flex: '0 0 auto',
        background: EXT_TINT[ext] ?? 'var(--neutral-100)',
        display: 'grid',
        placeItems: 'center',
        fontSize: 10,
        fontWeight: 500,
        color: 'var(--text)',
      }}
    >
      {ext}
    </span>
  )
}

/** 图片附件（截图）的缩略预览；非图片退回扩展名方块。 */
export function FilePreview({ f, size = 40 }: { f: FileItem; size?: number }) {
  const isImage = /^image\//.test(f.content_type || '')
  if (isImage && f.status === 'ready') {
    return (
      <img
        src={`/api/v1/files/${f.id}/download`}
        alt={f.name}
        loading="lazy"
        style={{
          width: size,
          height: size,
          objectFit: 'cover',
          border: '1px solid var(--border)',
          background: 'var(--surface-thin)',
          flex: '0 0 auto',
        }}
      />
    )
  }
  return <FileTile name={f.name} size={size} />
}

/* -------------------------------------------------------------------------- */

export function OverviewTab({
  app,
  interviews,
  assessments = [],
  notes,
  filesCount,
  refetchAll,
}: {
  app: AppRow
  interviews: Interview[]
  /** 已记录的 OA / 作业轮次（方案 §3.2）——一轮一笔，不覆盖上一轮。 */
  assessments?: AssessmentRound[]
  notes: Note[]
  filesCount: number
  refetchAll: () => void
}) {
  const qc = useQueryClient()
  const [showInterview, setShowInterview] = useState(false)
  const [showAction, setShowAction] = useState(false)
  const [showNote, setShowNote] = useState(false)
  const [showAssessment, setShowAssessment] = useState(false)
  const [actionErr, setActionErr] = useState('')
  const [noteErr, setNoteErr] = useState('')

  // 活动进度快捷操作（方案 §3.3/§5）：只改轮次自身的完成事实，绝不自动改大阶段，
  // 也不把「完成」当成「通过」。
  const progressMut = useMutation({
    mutationFn: ({ kind, id, progress }: { kind: 'interview' | 'assessment'; id: number; progress: string }) =>
      api.post(
        kind === 'interview'
          ? `/api/v1/applications/${app.id}/interviews/${id}/${progress === 'completed' ? 'complete' : 'reopen'}`
          : `/api/v1/applications/${app.id}/assessments/${id}/${progress === 'completed' ? 'complete' : 'reopen'}`,
        progress === 'completed' ? { completed_unknown: false } : {},
      ),
    onSuccess: () => {
      setActionErr('')
      qc.invalidateQueries({ queryKey: ['interviews', app.id] })
      qc.invalidateQueries({ queryKey: ['assessments', app.id] })
      qc.invalidateQueries({ queryKey: ['app', app.id] })
      qc.invalidateQueries({ queryKey: ['events', app.id] })
      qc.invalidateQueries({ queryKey: ['calendar'] })
      refetchAll()
    },
    onError: (e: unknown) => setActionErr(e instanceof ApiError ? e.message : '更新轮次失败，请重试'),
  })

  // Standalone actions for this application — the unified todo source of
  // truth (§5.3). The card below lists open + recently completed so the user
  // can undo a completion.
  const actionsQ = useQuery({
    queryKey: ['actions', 'app', app.id],
    queryFn: () => api.get<{ items: ActionItem[] }>(`/api/v1/applications/${app.id}/actions`),
  })
  const actions = actionsQ.data?.items ?? []
  const openActions = actions.filter((a) => !a.done_at)
  const recentDone = actions.filter((a) => a.done_at).slice(0, 3)

  const invalidateAfterAction = () => {
    qc.invalidateQueries({ queryKey: ['actions'] })
    // completing/postponing moves the item between calendar buckets
    qc.invalidateQueries({ queryKey: ['calendar'] })
    refetchAll()
  }
  const doneMut = useMutation({
    mutationFn: ({ id, done }: { id: number; done: boolean }) => api.post(`/api/v1/actions/${id}/done`, { done }),
    onSuccess: () => {
      setActionErr('')
      invalidateAfterAction()
    },
    onError: (e: unknown) => setActionErr(e instanceof ApiError ? e.message : '操作失败，请重试'),
  })
  const postponeMut = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/actions/${id}/postpone`, { days: 1 }),
    onSuccess: () => {
      setActionErr('')
      invalidateAfterAction()
    },
    onError: (e: unknown) => setActionErr(e instanceof ApiError ? e.message : '延期失败，请重试'),
  })
  // 删除备注：行内的「删除」按钮走 DELETE /notes/:note_id。
  const delNoteMut = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/applications/${app.id}/notes/${id}`),
    onSuccess: () => {
      setNoteErr('')
      qc.invalidateQueries({ queryKey: ['notes', app.id] })
      refetchAll()
    },
    onError: (e: unknown) => setNoteErr(e instanceof ApiError ? e.message : '删除备注失败，请重试'),
  })

  const facts: Array<[string, string]> = [
    // §5：详情也直接显示具体进度，而不是笼统的大阶段名。
    ['状态', comboLabel(app.status, app.substatus)],
    ['渠道', app.channel || '—'],
    ['投递时间', fmtDate(app.submitted_at)],
    ['首次回复', fmtDate(app.first_response_at)],
    ['工作方式', app.remote_policy || '—'],
    ['优先级', app.priority === 'high' ? '高' : app.priority === 'low' ? '低' : '中'],
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
      <div className="fact-grid">
        {facts.map(([label, value]) => (
          <div key={label} className="fact">
            <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{label}</div>
            <div style={{ fontSize: 14, fontWeight: 500, marginTop: 3 }}>{value}</div>
          </div>
        ))}
      </div>

      {actionErr && (
        <div style={{ marginTop: -4 }}>
          <ErrorText>{actionErr}</ErrorText>
        </div>
      )}

      <Card padding={0}>
        <div className="panel-head">
          <PanelTitle>待办 ({openActions.length})</PanelTitle>
          <span style={{ marginLeft: 'auto' }}>
            <Button variant="secondary" size="sm" onClick={() => setShowAction(true)}>
              ＋ 添加
            </Button>
          </span>
        </div>
        {openActions.length === 0 && !app.next_action ? (
          <div style={{ padding: '12px 16px', fontSize: 13, color: 'var(--text-muted)' }}>
            {NEXT_STEP_SUGGESTION[app.status] ?? '添加一个待办，会出现在首页与统一清单中'}
          </div>
        ) : (
          <>
            {openActions.map((a) => {
              const dueTs = a.due_ts ? new Date(a.due_ts).getTime() : null
              const dueDay = a.due_date ? toDayString(a.due_date) : null
              const todayKey = toDayString(new Date().toISOString(), effectiveZone()) ?? ''
              // instant due: compare in user-zone local day; date-only due: day-key compare
              const overdue =
                dueTs != null
                  ? (toDayString(a.due_ts, effectiveZone()) ?? '') < todayKey
                  : dueDay != null && dueDay < todayKey
              const shown = dueTs != null ? fmtDateTime(a.due_ts) : dueDay ? fmtDay(a.due_date) : null
              return (
                <div key={a.id} className="panel-row">
                  <span className="grow">
                    <span style={{ display: 'block', fontSize: 13, fontWeight: 500 }}>{a.title}</span>
                    <span style={{ display: 'block', fontSize: 12, color: overdue ? 'var(--danger)' : 'var(--text-muted)' }}>
                      {shown ? (overdue ? `逾期 ${shown}` : `截止 ${shown}`) : '无截止日期'}
                    </span>
                  </span>
                  {overdue && (
                    <Button variant="ghost" size="sm" disabled={postponeMut.isPending} onClick={() => postponeMut.mutate(a.id)}>
                      延期
                    </Button>
                  )}
                  <Button variant="secondary" size="sm" disabled={doneMut.isPending} onClick={() => doneMut.mutate({ id: a.id, done: true })}>
                    完成
                  </Button>
                </div>
              )
            })}
            {/* legacy next_action without a standalone action still surfaces */}
            {openActions.length === 0 && app.next_action && (
              <div className="panel-row">
                <span className="grow">
                  <span style={{ display: 'block', fontSize: 13, fontWeight: 500 }}>{app.next_action}</span>
                  <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>
                    {app.next_action_due_at ? `截止 ${fmtDay(app.next_action_due_at)}` : ''} · 旧记录（迁移后并入统一待办）
                  </span>
                </span>
              </div>
            )}
            {recentDone.length > 0 && (
              <div style={{ borderTop: '1px solid var(--border-alt)', padding: '6px 16px' }}>
                {recentDone.map((a) => (
                  <div key={a.id} className="panel-row" style={{ padding: '4px 0' }}>
                    <span className="grow" style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                      ✓ {a.title}
                    </span>
                    <Button variant="ghost" size="sm" disabled={doneMut.isPending} onClick={() => doneMut.mutate({ id: a.id, done: false })}>
                      撤销
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </Card>

      <Card padding={0}>
        <div className="panel-head">
          <PanelTitle>面试 ({interviews.length})</PanelTitle>
          <span style={{ marginLeft: 'auto' }}>
            <Button variant="secondary" size="sm" onClick={() => setShowInterview(true)}>
              ＋ 安排
            </Button>
          </span>
        </div>
        {interviews.length === 0 ? (
          <div style={{ padding: '12px 16px', fontSize: 13, color: 'var(--text-muted)' }}>还没有面试安排</div>
        ) : (
          interviews.map((i) => (
            <div key={i.id} className="panel-row">
              <span className="grow">
                <span style={{ display: 'block', fontSize: 14, fontWeight: 500 }}>
                  {i.round_name || '面试'}
                  {i.format ? ` · ${i.format}` : ''}
                </span>
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                  {i.scheduled_at ? fmtDateTime(i.scheduled_at) : '时间未定'}
                </span>
                {i.feedback && (
                  <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>反馈：{i.feedback}</span>
                )}
              </span>
              {/* 进度与结果分开显示：完成 ≠ 通过（方案 §3.3）。 */}
              <span
                style={{
                  fontSize: 11,
                  border: '1px solid var(--border)',
                  padding: '1px 6px',
                  color:
                    i.result === 'passed'
                      ? 'var(--positive)'
                      : i.result === 'failed'
                        ? 'var(--danger)'
                        : i.progress === 'completed'
                          ? 'var(--info)'
                          : i.scheduled_at
                            ? 'var(--info)'
                            : 'var(--neutral-600)',
                }}
              >
                {activityProgressLabel(i.progress, i.result, !!i.scheduled_at)}
              </span>
              <Button
                variant="ghost"
                size="sm"
                disabled={progressMut.isPending}
                onClick={() =>
                  progressMut.mutate({
                    kind: 'interview',
                    id: i.id,
                    progress: i.progress === 'completed' ? 'preparing' : 'completed',
                  })
                }
              >
                {i.progress === 'completed' ? '撤销完成' : '标记完成'}
              </Button>
            </div>
          ))
        )}
      </Card>

      <Card padding={0}>
        <div className="panel-head">
          <PanelTitle>OA / 作业 ({assessments.length})</PanelTitle>
          <span style={{ marginLeft: 'auto' }}>
            <Button variant="secondary" size="sm" onClick={() => setShowAssessment(true)}>
              ＋ 新增一轮
            </Button>
          </span>
        </div>
        {assessments.length === 0 ? (
          <div style={{ padding: '12px 16px', fontSize: 13, color: 'var(--text-muted)' }}>
            没有测评记录。收到 OA 后点右上角「＋ 新增一轮」记下这一轮。
          </div>
        ) : (
          assessments.map((a) => (
            <div key={a.id} className="panel-row">
              <span className="grow">
                <span style={{ display: 'block', fontSize: 14, fontWeight: 500 }}>
                  {a.name || 'OA'}
                  {a.kind === 'take_home' ? ' · Take-home 作业' : a.kind === 'other' ? ' · 其他测评' : ' · 在线测试'}
                </span>
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                  {a.completed_at
                    ? `完成于 ${fmtDateTime(a.completed_at)}`
                    : a.planned_at
                      ? `计划 ${fmtDateTime(a.planned_at)}`
                      : a.due_at
                        ? `截止 ${fmtDateTime(a.due_at)}`
                        : '时间未定'}
                  {a.due_at && a.progress !== 'completed' ? ` · 截止 ${fmtDateTime(a.due_at)}` : ''}
                </span>
                {a.link && (
                  <span style={{ display: 'block', fontSize: 12 }}>
                    <a href={a.link} target="_blank" rel="noreferrer">
                      测试链接
                    </a>
                  </span>
                )}
              </span>
              <span
                style={{
                  fontSize: 11,
                  border: '1px solid var(--border)',
                  padding: '1px 6px',
                  color:
                    a.result === 'passed'
                      ? 'var(--positive)'
                      : a.result === 'failed'
                        ? 'var(--danger)'
                        : 'var(--warning)',
                }}
              >
                {a.result === 'passed' ? '已通过' : a.result === 'failed' ? '未通过' : a.progress === 'completed' ? '已完成·等结果' : '准备中'}
              </span>
              <Button
                variant="ghost"
                size="sm"
                disabled={progressMut.isPending}
                onClick={() =>
                  progressMut.mutate({
                    kind: 'assessment',
                    id: a.id,
                    progress: a.progress === 'completed' ? 'preparing' : 'completed',
                  })
                }
              >
                {a.progress === 'completed' ? '撤回完成' : '标记已完成'}
              </Button>
            </div>
          ))
        )}
        <div style={{ padding: '10px 16px', fontSize: 12, color: 'var(--text-muted)' }}>
          完成时间不详时只标「已完成」，不伪造精确时间；收到、计划、截止、完成四种时间各自保存。
        </div>
      </Card>

      <Card padding={0}>
        <div className="panel-head">
          <PanelTitle>备注 ({notes.length})</PanelTitle>
          <span style={{ marginLeft: 'auto' }}>
            <Button variant="secondary" size="sm" onClick={() => setShowNote(true)}>
              ＋ 记录
            </Button>
          </span>
        </div>
        {noteErr && (
          <div style={{ padding: '6px 16px' }}>
            <ErrorText>{noteErr}</ErrorText>
          </div>
        )}
        {notes.length === 0 ? (
          <div style={{ padding: '12px 16px', fontSize: 13, color: 'var(--text-muted)' }}>
            记录沟通要点、联系方式等
          </div>
        ) : (
          notes.map((n) => (
            <div key={n.id} className="panel-row" style={{ alignItems: 'flex-start' }}>
              <span className="grow" style={{ fontSize: 13, lineHeight: 1.55, whiteSpace: 'pre-wrap' }}>
                {n.content_md}
                <span style={{ display: 'block', color: 'var(--text-muted)', marginTop: 4 }}>
                  {fmtDateTime(n.created_at)}
                </span>
              </span>
              <Button
                variant="ghost"
                size="sm"
                disabled={delNoteMut.isPending}
                onClick={() => delNoteMut.mutate(n.id)}
                title="删除这条备注"
              >
                删除
              </Button>
            </div>
          ))
        )}
        <div style={{ padding: '10px 16px', fontSize: 12, color: 'var(--text-muted)' }}>
          附件 {filesCount} 个 — 在「附件」标签页管理
        </div>
      </Card>

      {app.notes && (
        <div>
          <Eyebrow style={{ marginBottom: 8 }}>备注</Eyebrow>
          <div
            style={{
              padding: '12px 14px',
              borderRadius: 'var(--radius-control)',
              background: 'var(--surface-thin)',
              border: '1px solid var(--border)',
              fontSize: 13,
              lineHeight: 1.55,
              whiteSpace: 'pre-wrap',
            }}
          >
            {app.notes}
          </div>
        </div>
      )}

      {showInterview && (
        <InterviewForm
          appId={app.id}
          onClose={() => setShowInterview(false)}
          onDone={() => {
            refetchAll()
            setShowInterview(false)
          }}
        />
      )}
      {showAction && (
        <ActionForm
          app={app}
          onClose={() => setShowAction(false)}
          onDone={() => {
            qc.invalidateQueries()
            setShowAction(false)
          }}
        />
      )}
      {showAssessment && (
        <AssessmentForm
          appId={app.id}
          suggestedName={assessments.length === 0 ? 'OA' : `OA ${assessments.length + 1}`}
          onClose={() => setShowAssessment(false)}
          onDone={() => {
            qc.invalidateQueries({ queryKey: ['assessments', app.id] })
            qc.invalidateQueries({ queryKey: ['app', app.id] })
            setShowAssessment(false)
          }}
        />
      )}
      {showNote && (
        <NoteForm
          appId={app.id}
          onClose={() => setShowNote(false)}
          onDone={() => {
            qc.invalidateQueries()
            setShowNote(false)
          }}
        />
      )}
    </div>
  )
}

/* -------------------------------------------------------------------------- */

export function FilesTab({
  appId,
  files,
  interviews = [],
}: {
  appId: number
  files: FileItem[]
  /** 全部轮次（用于把附件按轮次分组展示；无轮次附件归入「未归档」） */
  interviews?: Interview[]
}) {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const [uploading, setUploading] = useState(false)
  const [attachTo, setAttachTo] = useState<number>(0)

  // 画布：⌘V 直接粘贴截图，自动挂到当前轮次。监听全局 paste：只有当焦点不在
  // 输入框/文本域里（避免用户在表单里粘贴文字时误传）才处理剪贴板图片。
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT')) return
      const files = Array.from(e.clipboardData?.files ?? [])
      const img = files.find((f) => f.type.startsWith('image/'))
      if (img) {
        e.preventDefault()
        uploadOne(img)
      }
    }
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attachTo])

  const uploadOne = async (file: File) => {
    setUploading(true)
    setErr('')
    try {
      await upMut.mutateAsync(file)
    } catch {
      /* surfaced through the mutation's onError */
    } finally {
      setUploading(false)
    }
  }

  const upMut = useMutation({
    mutationFn: (file: File) => {
      const fd = new FormData()
      fd.append('file', file)
      fd.append('category', /[.]png|jpe?g$/i.test(file.name) ? 'other' : 'resume')
      fd.append('application_id', String(appId))
      // 截图/附件挂到选中的轮次（默认挂到当前正在进行的轮次）
      if (attachTo > 0) fd.append('interview_id', String(attachTo))
      return api.post('/api/v1/files', fd, true)
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '上传失败'),
  })

  const roundByID = (id: number | null) => interviews.find((i) => i.id === id) ?? null
  const roundFiles = (rid: number | null) => files.filter((f) => f.interview_id === rid)
  const currentRound =
    interviews.find((i) => i.result === 'pending' && i.scheduled_at) ??
    interviews.find((i) => i.result === 'pending') ??
    interviews[interviews.length - 1] ??
    null
  // 轮次到位后默认挂到当前轮（画布：「自动挂到当前轮次」），用户仍可改选。
  useEffect(() => {
    if (interviews.length > 0 && attachTo === 0 && currentRound) setAttachTo(currentRound.id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [interviews])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      {err && <ErrorText>{err}</ErrorText>}

      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
        <label
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 'var(--space-2)',
            height: 'var(--control-h-sm)',
            padding: '0 var(--space-3)',
            borderRadius: 'var(--radius-control)',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            boxShadow: 'var(--highlight-inner)',
            font: 'var(--type-ui)',
            fontSize: 'var(--text-13)',
            cursor: 'pointer',
          }}
        >
          {uploading ? <Spinner size={14} /> : <Icon name="upload" size={14} />}
          {uploading ? '上传中…' : '＋ 上传附件'}
          <input
            type="file"
            style={{ display: 'none' }}
            accept={ACCEPTED_UPLOADS}
            onChange={async (e) => {
              const f = e.target.files?.[0]
              if (!f) return
              await uploadOne(f)
              e.target.value = ''
            }}
          />
        </label>
        {interviews.length > 0 && (
          <select
            aria-label="附件归属轮次"
            value={attachTo}
            onChange={(e) => setAttachTo(Number(e.target.value))}
            style={{
              height: 'var(--control-h-sm)',
              borderRadius: 'var(--radius-control)',
              border: '1px solid var(--border)',
              background: 'var(--surface)',
              fontSize: 'var(--text-13)',
              padding: '0 8px',
              color: 'var(--text)',
            }}
          >
            <option value={0}>不挂轮次</option>
            {interviews.map((i) => (
              <option key={i.id} value={i.id}>
                挂到 {i.round_name || `面试 #${i.id}`}
              </option>
            ))}
          </select>
        )}
      </div>

      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
        允许 PDF / DOCX / TXT / PNG / JPEG，单文件 ≤ 20 MiB。截图可 ⌘V 直接粘贴，自动挂到当前轮次。
      </p>

      {files.length === 0 ? (
        <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>
          还没有附件。上传简历版本、JD、Offer 文件等。
        </p>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          {/* 按轮次分组的附件区块：无轮次附件先列，再每个有附件的轮次 */}
          {roundFiles(null).length > 0 && (
            <FileGroup
              key="unassigned"
              title={interviews.length ? '未归档附件' : '附件'}
              items={roundFiles(null)}
            />
          )}
          {interviews.map((i) => {
            const its = roundFiles(i.id)
            if (its.length === 0) return null
            return (
              <FileGroup
                key={i.id}
                title={`${i.round_name || '面试'} · ${i.result === 'passed' ? '已通过' : i.result === 'failed' ? '未通过' : i.scheduled_at ? '待进行' : '待安排'}`}
                items={its}
              />
            )
          })}
          {roundFiles(null).length === files.length && files.length > 0 && (
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              共 {files.length} 个文件
            </span>
          )}
        </div>
      )}
    </div>
  )
}

function FileGroup({ title, items }: { title: string; items: FileItem[] }) {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const del = useMutation({
    mutationFn: (id: string) => api.del(`/api/v1/files/${id}`),
    onSuccess: () => {
      setErr('')
      qc.invalidateQueries({ queryKey: ['files'] })
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '删除失败'),
  })
  return (
    <Card padding={0}>
      <div className="panel-head">
        <PanelTitle>{title}</PanelTitle>
        <Num color="var(--text-muted)">{items.length}</Num>
      </div>
      {err && (
        <div style={{ padding: '6px 16px' }}>
          <ErrorText>{err}</ErrorText>
        </div>
      )}
      {items.map((f) => (
        <div key={f.id} className="panel-row">
          <FilePreview f={f} />
          <span className="grow" style={{ minWidth: 0 }}>
            <span className="ellipsis" style={{ display: 'block', fontSize: 14 }}>
              {f.name}
            </span>
            <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>
              {fmtBytes(f.size_bytes)} · {f.category === 'resume' ? '简历' : '其他'} · {fmtDate(f.created_at)}
            </span>
            {f.status !== 'ready' && <span style={{ fontSize: 12, color: 'var(--danger)' }}>状态：{f.status}</span>}
          </span>
          {f.status === 'ready' && (
            <LinkButton variant="ghost" size="sm" href={`/api/v1/files/${f.id}/download`} download>
              下载
            </LinkButton>
          )}
          <Button variant="ghost" size="sm" disabled={del.isPending} onClick={() => del.mutate(f.id)} title="删除文件并移除关联">
            删除
          </Button>
        </div>
      ))}
    </Card>
  )
}
