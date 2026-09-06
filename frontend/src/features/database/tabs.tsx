import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtBytes, fmtDate, fmtDateTime } from '../../lib/api'
import type { AppEvent, AppRow, FileItem, Interview, Note } from '../../lib/types'
import { NEXT_STEP_SUGGESTION, STATUSES, statusMeta } from '../../lib/status'
import { Button, Card, Eyebrow, LinkButton, PanelTitle, Select } from '../../ds'
import { Icon } from '../../components/Icon'
import { Dot, ErrorText, Modal, Num, Spinner } from '../../components/ui'
import { ActionForm, InterviewForm, NoteForm } from './forms'

const ACCEPTED_UPLOADS = '.pdf,.docx,.txt,.png,.jpg,.jpeg'

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
        borderRadius: 7,
        background: EXT_TINT[ext] ?? 'var(--surface-thin)',
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

      <Card padding={0} style={{ overflow: 'hidden' }}>
        <div className="panel-head">
          <PanelTitle>下一步行动</PanelTitle>
          <span style={{ marginLeft: 'auto' }}>
            <Button variant="secondary" size="sm" onClick={() => setShowAction(true)}>
              ＋ 添加
            </Button>
          </span>
        </div>
        <div style={{ padding: '12px 16px', fontSize: 13 }}>
          {app.next_action ? (
            <>
              {app.next_action}
              {app.next_action_due_at && (
                <span style={{ color: 'var(--text-muted)' }}> · 截止 {fmtDate(app.next_action_due_at)}</span>
              )}
            </>
          ) : (
            <span style={{ color: 'var(--text-muted)' }}>
              {NEXT_STEP_SUGGESTION[app.status] ?? '填写下一步行动以在今日待办中提醒自己'}
            </span>
          )}
        </div>
      </Card>

      <Card padding={0} style={{ overflow: 'hidden' }}>
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
                  {i.scheduled_at ? fmtDateTime(i.scheduled_at) : '时间未定'} · {i.result || '待定'}
                </span>
                {i.feedback && (
                  <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>反馈：{i.feedback}</span>
                )}
              </span>
            </div>
          ))
        )}
      </Card>

      <Card padding={0} style={{ overflow: 'hidden' }}>
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

export function FilesTab({ appId, files }: { appId: number; files: FileItem[] }) {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const [uploading, setUploading] = useState(false)

  const upMut = useMutation({
    mutationFn: (file: File) => {
      const fd = new FormData()
      fd.append('file', file)
      fd.append('category', /\.(pdf|docx?|txt)$/i.test(file.name) ? 'resume' : 'other')
      fd.append('application_id', String(appId))
      return api.post('/api/v1/files', fd, true)
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '上传失败'),
  })

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      {err && <ErrorText>{err}</ErrorText>}

      <label
        style={{
          display: 'inline-flex',
          alignSelf: 'flex-start',
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
            setUploading(true)
            try {
              await upMut.mutateAsync(f)
            } catch {
              /* surfaced through the mutation's onError */
            } finally {
              setUploading(false)
              e.target.value = ''
            }
          }}
        />
      </label>

      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
        允许 PDF / DOCX / TXT / PNG / JPEG，单文件 ≤ 20 MiB
      </p>

      {files.length === 0 ? (
        <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>
          还没有附件。上传简历版本、JD、Offer 文件等。
        </p>
      ) : (
        <Card padding={0} style={{ overflow: 'hidden' }}>
          {files.map((f) => (
            <div key={f.id} className="panel-row">
              <FileTile name={f.name} />
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
            </div>
          ))}
        </Card>
      )}
    </div>
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
