import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, ApiError, fmtDate, fmtDay } from '../../lib/api'
import type { AppEvent, AppRow, FileItem, Interview, Note } from '../../lib/types'
import { ENDED } from '../../lib/status'
import { Button, Card, Eyebrow, IconButton, Tabs } from '../../ds'
import { CompanyMark, Icon } from '../../components/Icon'
import { StageTrail } from '../../components/StageTrail'
import { ErrorText, Num, PageSpinner, StatusChip } from '../../components/ui'
import { FilesTab, OverviewTab, TimelineTab } from './tabs'
import { TransitionModal } from './transition'

type DetailTab = 'overview' | 'files' | 'timeline'

/**
 * Full detail panel. Rendered inside the sliding drawer on the database page
 * and inline on the shareable `/apps/:id` route.
 */
export function AppDetailContent({
  appId,
  onClose,
  embedded,
}: {
  appId: number
  onClose: () => void
  embedded?: boolean
}) {
  const qc = useQueryClient()
  const [tab, setTab] = useState<DetailTab>('overview')
  const [showTransition, setShowTransition] = useState(false)

  const appQ = useQuery({ queryKey: ['app', appId], queryFn: () => api.get<AppRow>(`/api/v1/applications/${appId}`) })
  const eventsQ = useQuery({
    queryKey: ['events', appId],
    queryFn: () => api.get<{ items: AppEvent[] }>(`/api/v1/applications/${appId}/events`),
  })
  const filesQ = useQuery({
    queryKey: ['files', appId],
    queryFn: () => api.get<{ items: FileItem[] }>(`/api/v1/files?application_id=${appId}`),
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

  const path = useMemo(
    () =>
      events
        .filter((e) => (e.event_type === 'created' || e.event_type === 'status_change') && e.to_status)
        .map((e) => e.to_status as string),
    [events],
  )

  if (!app) {
    if (appQ.isError) {
      return embedded ? (
        <ErrorText>加载失败</ErrorText>
      ) : (
        <>
          <div className="drawer-backdrop" onClick={onClose} />
          <aside className="drawer">
            <div className="drawer-body">
              <ErrorText>加载失败</ErrorText>
            </div>
          </aside>
        </>
      )
    }
    return <PageSpinner />
  }

  const tabItems = [
    { value: 'overview', label: '概览' },
    { value: 'files', label: `附件 (${files.length})` },
    { value: 'timeline', label: '时间线' },
  ]

  const head = (
    <>
      <CompanyMark name={app.company_name} size={34} seed={app.id} />
      <span className="grow" style={{ minWidth: 0 }}>
        <span
          className="ellipsis"
          style={{
            display: 'block',
            font: 'var(--type-h4)',
            fontFamily: 'var(--font-display)',
            letterSpacing: 'var(--tracking-display)',
          }}
        >
          {app.company_name}
        </span>
        <span
          className="ellipsis"
          style={{ display: 'block', fontSize: 13, color: 'var(--text-muted)', marginTop: 2 }}
        >
          {app.position}
          {app.location ? ` · ${app.location}` : ''}
          {app.salary_max != null ? ` · ${app.salary_min ?? '—'}–${app.salary_max} ${app.salary_currency}` : ''}
        </span>
      </span>
      <span style={{ display: 'flex', alignItems: 'center', gap: 6, flex: '0 0 auto' }}>
        <StatusChip status={app.status} />
        {!embedded && (
          <>
            <Link to={`/apps/${app.id}`} aria-label="完整详情" title="完整详情" style={{ display: 'inline-flex' }}>
              <Icon name="external" size={16} color="var(--text-muted)" />
            </Link>
            <RowMenu
              appId={app.id}
              app={app}
              onChanged={() => {
                qc.invalidateQueries()
                onClose()
              }}
            />
            <IconButton label="关闭" size="sm" variant="ghost" onClick={onClose}>
              <Icon name="close" size={16} />
            </IconButton>
          </>
        )}
      </span>
    </>
  )

  const body = (
    <>
      <div>
        <Eyebrow style={{ marginBottom: 10 }}>阶段轨迹</Eyebrow>
        <StageTrail current={app.status} path={path} />
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <Tabs items={tabItems} value={tab} onChange={(v) => setTab(v as DetailTab)} size="sm" ariaLabel="详情视图" />
        {app.job_url && (
          <a href={app.job_url} target="_blank" rel="noreferrer" style={{ fontSize: 13 }}>
            岗位链接 ↗
          </a>
        )}
        {app.deadline && (
          <span style={{ marginLeft: 'auto' }}>
            <Num color="var(--text-muted)">截止 {fmtDay(app.deadline)}</Num>
          </span>
        )}
      </div>

      {app.reason && (
        <Card padding="10px 12px" variant="outline" style={{ fontSize: 13 }}>
          <b>原因：</b>
          {app.reason}
        </Card>
      )}

      {tab === 'overview' && (
        <OverviewTab
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
      {tab === 'files' && <FilesTab appId={app.id} files={files} />}
      {tab === 'timeline' && <TimelineTab appId={app.id} events={events} status={app.status} />}
    </>
  )

  const foot = (
    <>
      <Button variant="primary" size="sm" disabled={ENDED.has(app.status)} onClick={() => setShowTransition(true)}>
        更新进度
      </Button>
      <span style={{ marginLeft: 'auto' }}>
        <Num color="var(--text-muted)">版本 {app.version}</Num>
      </span>
    </>
  )

  const transition = showTransition && (
    <TransitionModal
      appId={app.id}
      currentStatus={app.status}
      version={app.version}
      onClose={() => setShowTransition(false)}
    />
  )

  if (embedded) {
    return (
      <div className="drawer" style={{ position: 'static', width: '100%', border: 0, boxShadow: 'none', background: 'transparent' }}>
        <div className="drawer-head" style={{ paddingLeft: 0, paddingRight: 0 }}>
          {head}
        </div>
        <div className="drawer-body" style={{ paddingLeft: 0, paddingRight: 0, overflow: 'visible' }}>
          {body}
        </div>
        <div className="drawer-foot" style={{ paddingLeft: 0, paddingRight: 0 }}>
          {foot}
        </div>
        {transition}
      </div>
    )
  }

  return (
    <>
      <div className="drawer-backdrop" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-modal="true" aria-label="岗位详情">
        <div className="drawer-head">{head}</div>
        <div className="drawer-body">{body}</div>
        <div className="drawer-foot">{foot}</div>
        {transition}
      </aside>
    </>
  )
}

export function Drawer({ appId, onClose }: { appId: number; onClose: () => void }) {
  return <AppDetailContent appId={appId} onClose={onClose} />
}

/**
 * Archive / unarchive / soft-delete / restore. Trash and archive are
 * visibility flags rather than statuses (plan §2.2).
 */
function RowMenu({ appId, app, onChanged }: { appId: number; app: AppRow; onChanged: () => void }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [err, setErr] = useState('')

  const run = useMutation({
    mutationFn: (fn: () => Promise<unknown>) => fn(),
    onSuccess: () => {
      setOpen(false)
      qc.invalidateQueries({ queryKey: ['app', appId] })
      onChanged()
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '操作失败'),
  })

  return (
    <span style={{ position: 'relative', display: 'inline-flex' }}>
      <IconButton label="更多操作" size="sm" variant="ghost" onClick={() => setOpen((v) => !v)}>
        <Icon name="dots" size={16} />
      </IconButton>
      {open && (
        <>
          <span style={{ position: 'fixed', inset: 0, zIndex: 55 }} onClick={() => setOpen(false)} />
          <Card
            variant="strong"
            padding={6}
            style={{ position: 'absolute', right: 0, top: '100%', zIndex: 56, minWidth: 170 }}
          >
            {err && <ErrorText>{err}</ErrorText>}
            {!app.archived ? (
              <button
                className="menu-item"
                onClick={() => run.mutate(() => api.post(`/api/v1/applications/${appId}/archive`))}
              >
                <Icon name="archive" size={15} /> 归档
              </button>
            ) : (
              <button
                className="menu-item"
                onClick={() => run.mutate(() => api.post(`/api/v1/applications/${appId}/unarchive`))}
              >
                <Icon name="archive" size={15} /> 取消归档
              </button>
            )}
            {!app.deleted ? (
              <button
                className="menu-item danger"
                onClick={() => run.mutate(() => api.del(`/api/v1/applications/${appId}`))}
              >
                <Icon name="trash" size={15} /> 移到回收站
              </button>
            ) : (
              <button
                className="menu-item"
                onClick={() => run.mutate(() => api.post(`/api/v1/applications/${appId}/restore`))}
              >
                <Icon name="restore" size={15} /> 恢复
              </button>
            )}
          </Card>
        </>
      )}
    </span>
  )
}
