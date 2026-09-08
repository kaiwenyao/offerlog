import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtBytes, fmtDate, fmtDateTime, fmtDay, toDayString } from '../../lib/api'
import { effectiveZone } from '../../lib/tz'
import type { ActionItem, AppEvent, AppRow, FileItem, Interview, Note } from '../../lib/types'
import { NEXT_STEP_SUGGESTION, STATUSES, statusMeta } from '../../lib/status'
import { Button, Card, Eyebrow, LinkButton, PanelTitle, Select } from '../../ds'
import { Icon } from '../../components/Icon'
import { Dot, ErrorText, Modal, Num, Spinner } from '../../components/ui'
import { ActionForm, InterviewForm, NoteForm } from './forms'

const ACCEPTED_UPLOADS = '.pdf,.docx,.txt,.png,.jpg,.jpeg'

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
  const [actionErr, setActionErr] = useState('')

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

  const facts: Array<[string, string]> = [
    ['状态', statusMeta(app.status).label],
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
              {/* 三态标签：已通过 / 待进行 / 待安排（result + scheduled_at 推导） */}
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
                        : i.scheduled_at
                          ? 'var(--info)'
                          : 'var(--neutral-600)',
                }}
              >
                {i.result === 'passed' ? '已通过' : i.result === 'failed' ? '未通过' : i.scheduled_at ? '待进行' : '待安排'}
              </span>
            </div>
          ))
        )}
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

/* -------------------------------------------------------------------------- */

export function TimelineTab({ appId, events, status }: { appId: number; events: AppEvent[]; status: string }) {
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
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '纠正失败'),
  })

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      {err && <ErrorText>{err}</ErrorText>}
      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
        当前状态：{statusMeta(status).label}。每次真实变化都有审计记录。
      </p>

      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {events.map((e, i) => (
          <div key={e.id} style={{ display: 'flex', gap: 12 }}>
            <span className="timeline-rail">
              <span style={{ marginTop: 5 }}>
                <Dot color={e.to_status ? statusMeta(e.to_status).dot : 'var(--neutral)'} size={9} />
              </span>
              {i < events.length - 1 && <span className="line" />}
            </span>
            <span className="grow" style={{ paddingBottom: 14, minWidth: 0 }}>
              <span style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
                <span style={{ fontSize: 14, fontWeight: 500 }}>
                  {e.event_type === 'created' && '创建记录'}
                  {e.event_type === 'correction' && '纠正'}
                  {e.event_type === 'status_change' &&
                    `${e.from_status ? statusMeta(e.from_status).label : '—'} → ${
                      e.to_status ? statusMeta(e.to_status).label : '—'
                    }`}
                  {!['created', 'correction', 'status_change'].includes(e.event_type) && e.event_type}
                </span>
                <span style={{ marginLeft: 'auto' }}>
                  <Num color="var(--text-muted)">{fmtDateTime(e.occurred_at)}</Num>
                </span>
              </span>
              {e.note && (
                <span style={{ display: 'block', fontSize: 13, color: 'var(--text-muted)', marginTop: 3, lineHeight: 1.45 }}>
                  {e.note.replace(/^\|idem:.*/, '')}
                </span>
              )}
              {e.reason && (
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>原因：{e.reason}</span>
              )}
              {e.corrects_event_id && (
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>
                  纠正了事件 #{e.corrects_event_id}
                </span>
              )}
              {e.event_type === 'status_change' && e.to_status && status !== e.to_status && (
                <span style={{ display: 'block', marginTop: 6 }}>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setPick(e)
                      setNewStatus(e.to_status ?? 'saved')
                    }}
                  >
                    <Icon name="edit" size={13} /> 纠正此记录
                  </Button>
                </span>
              )}
            </span>
          </div>
        ))}
      </div>

      {pick && (
        <Modal
          title="纠正历史事件"
          onClose={() => setPick(null)}
          footer={
            <Button variant="primary" size="sm" onClick={() => correctMut.mutate()} disabled={correctMut.isPending}>
              {correctMut.isPending ? <Spinner size={14} /> : '确认纠正'}
            </Button>
          }
        >
          <p style={{ fontSize: 13, marginTop: 0 }}>将事件 #{pick.id} 的目标状态纠正为：</p>
          <Select
            options={STATUSES.map((s) => ({ value: s.key, label: s.label }))}
            value={newStatus}
            onChange={(e) => setNewStatus(e.target.value)}
            aria-label="纠正后的状态"
          />
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 0 }}>
            纠正会保留审计痕迹（corrects_event_id），并重新计算当前状态。
          </p>
        </Modal>
      )}
    </div>
  )
}
