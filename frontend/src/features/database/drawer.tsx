import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, ApiError, fmtDate, fmtDay } from '../../lib/api'
import type { AppEvent, AppRow, AssessmentRound, FileItem, Interview, Milestone, Note } from '../../lib/types'
import { Badge, Button, Card, IconButton, Tabs } from '../../ds'
import { lockScroll } from '../../ds/scrollLock'
import { CompanyMark, Icon } from '../../components/Icon'
import { ErrorText, Num, PageSpinner, StatusChip } from '../../components/ui'
import { FilesTab, OverviewTab } from './tabs'
import { TimelineTab } from './timeline'
import { ApplicationEditForm } from './forms'

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
  // 时间线是记录进度的主面板（迁移 00006），所以抽屉默认落在这一页。
  const [tab, setTab] = useState<DetailTab>('timeline')
  // 基础信息（公司/岗位/城市/JD 链接/薪资/渠道/截止日期）的编辑入口：创建弹窗
  // 只要求公司和岗位，其余必须能在这里补齐。
  const [editing, setEditing] = useState(false)

  // 抽屉是 aria-modal 的对话框，就得像对话框一样能被 Esc 关掉（ds/Dialog 一直
  // 是这个行为）。嵌在 /apps/:id 整页里时不挂监听：那里没有可关的浮层，按 Esc
  // 反而会把人踢回数据库页。
  useEffect(() => {
    if (embedded) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      // 抽屉里还开着弹窗（编辑事件 / 删除确认 / 文件预览）时，这一下 Esc 属于
      // 那个弹窗。ds/Dialog 也监听 document，不让开就会一次关掉两层：确认框和
      // 它背后的整个抽屉。
      if (document.querySelector('.modal-backdrop')) return
      onClose()
    }
    document.addEventListener('keydown', onKey)
    // 抽屉也是浮层：背景列表不该跟着滚轮一起滚（与 ds/Dialog 同一把计数锁——
    // 抽屉里的弹窗关掉时不会把抽屉自己的那一层也解开）。
    const unlock = lockScroll()
    return () => {
      document.removeEventListener('keydown', onKey)
      unlock()
    }
  }, [embedded, onClose])

  const appQ = useQuery({ queryKey: ['app', appId], queryFn: () => api.get<AppRow>(`/api/v1/applications/${appId}`) })
  const eventsQ = useQuery({
    queryKey: ['events', appId],
    queryFn: () => api.get<{ items: AppEvent[] }>(`/api/v1/applications/${appId}/events`),
  })
  const milestonesQ = useQuery({
    queryKey: ['milestones', appId],
    queryFn: () => api.get<{ items: Milestone[] }>(`/api/v1/applications/${appId}/milestones`),
  })
  const filesQ = useQuery({
    queryKey: ['files', appId],
    queryFn: () => api.get<{ items: FileItem[] }>(`/api/v1/files?application_id=${appId}`),
  })
  const interviewsQ = useQuery({
    queryKey: ['interviews', appId],
    queryFn: () => api.get<{ items: Interview[] }>(`/api/v1/applications/${appId}/interviews`),
  })
  const assessmentsQ = useQuery({
    queryKey: ['assessments', appId],
    queryFn: () => api.get<{ items: AssessmentRound[] }>(`/api/v1/applications/${appId}/assessments`),
  })
  const notesQ = useQuery({
    queryKey: ['notes', appId],
    queryFn: () => api.get<{ items: Note[] }>(`/api/v1/applications/${appId}/notes`),
  })

  // refetch 失败时 react-query 仍保留上一次的 data，只是把 status 置成 error。
  // 只看 isError 的话，一次后台 refetch 失败（加完备注后 notesQ 重取、或 10s
  // staleTime 过后重开抽屉）就会把一个已经加载好的标签页换成「加载失败」。
  const failed = (q: { isError: boolean; data: unknown }) => q.isError && q.data === undefined

  const app = appQ.data
  const events = eventsQ.data?.items ?? []
  const milestones = milestonesQ.data?.items ?? []
  const files = filesQ.data?.items ?? []
  const interviews = interviewsQ.data?.items ?? []
  const assessments = assessmentsQ.data?.items ?? []
  const notes = notesQ.data?.items ?? []

  if (!app) {
    // 加载中与加载失败都得穿上抽屉的外壳：以前 loading 分支直接返回一个光秃秃
    // 的 PageSpinner，于是点开一个岗位时页面底部先冒出一个孤零零的转圈，既没有
    // backdrop 也没有关闭按钮——这段时间连「我点错了，退出」都做不到。
    const body = appQ.isError ? (
      <>
        <ErrorText>加载失败</ErrorText>
        <Button variant="secondary" size="sm" onClick={() => appQ.refetch()} style={{ marginTop: 10 }}>
          重试
        </Button>
      </>
    ) : (
      <PageSpinner />
    )
    if (embedded) return <div>{body}</div>
    return (
      <>
        <div className="drawer-backdrop" onClick={onClose} />
        <aside className="drawer" role="dialog" aria-modal="true" aria-label="岗位详情">
          <div className="drawer-head">
            <span className="grow" style={{ fontSize: 14, color: 'var(--text-muted)' }}>
              {appQ.isError ? '岗位详情' : '加载中…'}
            </span>
            <IconButton label="关闭" size="sm" variant="ghost" onClick={onClose}>
              <Icon name="close" size={16} />
            </IconButton>
          </div>
          <div className="drawer-body">{body}</div>
        </aside>
      </>
    )
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
        {/* 顶部显示具体进度（方案 §5），子状态为空时退回大阶段标签。 */}
        <StatusChip status={app.status} substatus={app.substatus} />
        {app.archived && <Badge tone="neutral">已归档</Badge>}
        <IconButton label="编辑基础信息" size="sm" variant="ghost" onClick={() => setEditing(true)}>
          <Icon name="edit" size={16} />
        </IconButton>
        {!embedded && (
          <Link to={`/apps/${app.id}`} aria-label="完整详情" title="完整详情" style={{ display: 'inline-flex' }}>
            <Icon name="external" size={16} color="var(--text-muted)" />
          </Link>
        )}
        {/* 归档 / 移到回收站 / 恢复在整页详情上同样要有。今日待办、面试日历和
            通知中心跳过来的全是 /apps/:id（embedded），以前这个菜单被
            `!embedded` 一起关掉了——从这三个入口进来的人只能改基础信息，想归档
            得自己绕回数据库页再从抽屉里打开。 */}
        <RowMenu
          appId={app.id}
          app={app}
          onEdit={() => setEditing(true)}
          // 只有「离开当前列表」的操作才关闭这一层：归档 / 移到回收站之后这条
          // 记录确实不在默认视图里了，继续开着抽屉指向一条看不见的行没有意义。
          // 取消归档 / 恢复相反——用户刚把它捞回来，这时候被弹回 /database
          // （整页详情上 onClose 就是导航）只会让人重新找一遍。
          onChanged={(left) => {
            qc.invalidateQueries()
            if (left) onClose()
          }}
        />
        {!embedded && (
          <IconButton label="关闭" size="sm" variant="ghost" onClick={onClose}>
            <Icon name="close" size={16} />
          </IconButton>
        )}
      </span>
    </>
  )

  const body = (
    <>
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

      {editing && (
        <ApplicationEditForm
          app={app}
          onClose={() => setEditing(false)}
          onDone={() => {
            setEditing(false)
            // 基础信息会同时影响详情、列表与看板行的展示，整组失效。
            qc.invalidateQueries({ queryKey: ['app', appId] })
            qc.invalidateQueries({ queryKey: ['apps'] })
            qc.invalidateQueries({ queryKey: ['views'] })
          }}
        />
      )}

      {app.reason && (
        <Card padding="10px 12px" variant="outline" style={{ fontSize: 13 }}>
          <b>原因：</b>
          {app.reason}
        </Card>
      )}

      {tab === 'overview' &&
        (failed(interviewsQ) || failed(assessmentsQ) || failed(notesQ) ? (
          <QueryError
            label="概览"
            onRetry={() => {
              void interviewsQ.refetch()
              void assessmentsQ.refetch()
              void notesQ.refetch()
            }}
          />
        ) : (
          <OverviewTab
            app={app}
            interviews={interviews}
            assessments={assessments}
            notes={notes}
            filesCount={files.length}
            refetchAll={() => {
              qc.invalidateQueries({ queryKey: ['app', appId] })
              qc.invalidateQueries({ queryKey: ['events', appId] })
              qc.invalidateQueries({ queryKey: ['milestones', appId] })
              qc.invalidateQueries({ queryKey: ['interviews', appId] })
              qc.invalidateQueries({ queryKey: ['assessments', appId] })
            }}
          />
        ))}
      {tab === 'files' &&
        (failed(filesQ) ? (
          <QueryError label="附件" onRetry={() => void filesQ.refetch()} />
        ) : (
          <FilesTab appId={app.id} files={files} interviews={interviews} />
        ))}
      {tab === 'timeline' &&
        (failed(eventsQ) || failed(milestonesQ) ? (
          <QueryError
            label="时间线"
            onRetry={() => {
              void eventsQ.refetch()
              void milestonesQ.refetch()
            }}
          />
        ) : (
          <TimelineTab
            appId={app.id}
            events={events}
            milestones={milestones}
            status={app.status}
            substatus={app.substatus}
          />
        ))}
    </>
  )

  const foot = (
    <>
      <span style={{ font: 'var(--type-caption)', color: 'var(--text-muted)' }}>
        进度记在「时间线」里：添加事件即可，状态会自动跟上。
      </span>
      <span style={{ marginLeft: 'auto' }}>
        <Num color="var(--text-muted)">版本 {app.version}</Num>
      </span>
    </>
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
      </aside>
    </>
  )
}

export function Drawer({ appId, onClose }: { appId: number; onClose: () => void }) {
  return <AppDetailContent appId={appId} onClose={onClose} />
}

function QueryError({ label, onRetry }: { label: string; onRetry: () => void }) {
  return (
    <Card padding="14px">
      <ErrorText>{label}加载失败</ErrorText>
      <Button variant="secondary" size="sm" onClick={onRetry} style={{ marginTop: 10 }}>
        重试
      </Button>
    </Card>
  )
}

/**
 * Archive / unarchive / soft-delete / restore. Trash and archive are
 * visibility flags rather than statuses (plan §2.2).
 */
function RowMenu({
  appId,
  app,
  onEdit,
  onChanged,
}: {
  appId: number
  app: AppRow
  onEdit: () => void
  /** `left` = 这条记录离开了默认视图（归档 / 移到回收站），调用方可以关掉这一层。 */
  onChanged: (left: boolean) => void
}) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [err, setErr] = useState('')

  const run = useMutation({
    mutationFn: ({ fn }: { fn: () => Promise<unknown>; left: boolean }) => fn(),
    onSuccess: (_data, vars) => {
      setOpen(false)
      qc.invalidateQueries({ queryKey: ['app', appId] })
      onChanged(vars.left)
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
            <button
              className="menu-item"
              onClick={() => {
                // 必须先关菜单：否则 position:fixed 的关闭遮罩还留在 DOM 里，
                // 编辑弹窗关掉后下一次点击会被它吃掉（只用来关菜单）。
                setOpen(false)
                onEdit()
              }}
            >
              <Icon name="edit" size={15} /> 编辑基础信息
            </button>
            {!app.archived ? (
              <button
                className="menu-item"
                onClick={() => run.mutate({ fn: () => api.post(`/api/v1/applications/${appId}/archive`), left: true })}
              >
                <Icon name="archive" size={15} /> 归档
              </button>
            ) : (
              <button
                className="menu-item"
                onClick={() => run.mutate({ fn: () => api.post(`/api/v1/applications/${appId}/unarchive`), left: false })}
              >
                <Icon name="archive" size={15} /> 取消归档
              </button>
            )}
            {!app.deleted ? (
              <button
                className="menu-item danger"
                onClick={() => run.mutate({ fn: () => api.del(`/api/v1/applications/${appId}`), left: true })}
              >
                <Icon name="trash" size={15} /> 移到回收站
              </button>
            ) : (
              <button
                className="menu-item"
                onClick={() => run.mutate({ fn: () => api.post(`/api/v1/applications/${appId}/restore`), left: false })}
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
