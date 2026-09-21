import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtBytes, fmtDate, fmtDateTime, fmtDay, toDayString } from '../../lib/api'
import { effectiveZone } from '../../lib/tz'
import { FILE_CATEGORIES, guessCategory } from '../../lib/files'
import type { ActionItem, AppRow, AssessmentRound, FileItem, Interview, Note } from '../../lib/types'
import { comboLabel, NEXT_STEP_SUGGESTION } from '../../lib/status'
import { Button, Card, Eyebrow, LinkButton, PanelTitle } from '../../ds'
import { Icon } from '../../components/Icon'
import { ErrorText, ConfirmDialog, Num, Spinner } from '../../components/ui'
import { CategorySelect, FileViewerModal, useDropUpload, useUpdateCategory } from '../files/shared'
import { ActionEditForm, ActionForm, AssessmentEditForm, AssessmentForm, InterviewEditForm, InterviewForm, NoteEditForm, NoteForm, formatLabel } from './forms'

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

/**
 * 一轮 OA / 测评那行的时间说明。
 *
 * 四种时间各自保存，但一行里最多说两件事：**这轮走到哪一步了**（完成于 / 计划）
 * 和**什么时候截止**。截止只说一次——之前「只填了截止、还没开做」（最常见的
 * 情况）会先在主时间位渲染一遍「截止 X」，后面再追加一遍「· 截止 X」，同一个
 * 时间在同一行里出现两次。
 *
 * 纯函数（只依赖 fmtDateTime），便于单测钉住「截止不重复」。
 */
export function assessmentTiming(
  a: Pick<AssessmentRound, 'completed_at' | 'planned_at' | 'due_at' | 'progress'>,
): string {
  const parts: string[] = []
  if (a.completed_at) parts.push(`完成于 ${fmtDateTime(a.completed_at)}`)
  else if (a.planned_at) parts.push(`计划 ${fmtDateTime(a.planned_at)}`)
  // 做完之后截止时间不再是待办信息，所以只在「还没完成」时追加；但当它是这行
  // 唯一已知的时间时仍然要显示，否则会退化成一句没用的「时间未定」。
  if (a.due_at && (a.progress !== 'completed' || parts.length === 0)) {
    parts.push(`截止 ${fmtDateTime(a.due_at)}`)
  }
  return parts.length > 0 ? parts.join(' · ') : '时间未定'
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
  // 编辑 / 删除的目标行：以前这些实体只能新增，填错了无从修正。
  const [editInterview, setEditInterview] = useState<Interview | null>(null)
  const [editAssessment, setEditAssessment] = useState<AssessmentRound | null>(null)
  const [editAction, setEditAction] = useState<ActionItem | null>(null)
  const [editNote, setEditNote] = useState<Note | null>(null)
  const [delInterview, setDelInterview] = useState<Interview | null>(null)
  const [delAssessment, setDelAssessment] = useState<AssessmentRound | null>(null)
  const [delAction, setDelAction] = useState<ActionItem | null>(null)
  const [delNote, setDelNote] = useState<Note | null>(null)
  const [actionErr, setActionErr] = useState('')
  // 删除失败的文案必须显示在确认弹窗内部：失败时弹窗还开着（pending 回到 false），
  // 而 actionErr 渲染在抽屉顶部、被 backdrop 盖住——用户只看到「点了确认没反应」。
  // 所以删除错误单走一个状态，并由 ConfirmDialog 的 error 插槽渲染。
  const [delErr, setDelErr] = useState('')

  // 一轮活动的增删改都要连岗位快照、时间线与日历一起刷新：改了面试时间而首页
  // 「即将到来的面试」和日历还按旧时间显示，正是这次要修的东西。
  // ['home'] / ['notifications'] 也必须失效：前者是今日待办里那份「即将到来的
  // 面试 / OA」（staleTime 内会一直显示旧时间），后者是铃铛（取消面试后端已经
  // 清了提醒，但通知中心不清就最多 60s 后才对得上）。
  const refreshActivity = () => {
    qc.invalidateQueries({ queryKey: ['interviews', app.id] })
    qc.invalidateQueries({ queryKey: ['assessments', app.id] })
    qc.invalidateQueries({ queryKey: ['actions'] })
    qc.invalidateQueries({ queryKey: ['app', app.id] })
    qc.invalidateQueries({ queryKey: ['events', app.id] })
    qc.invalidateQueries({ queryKey: ['calendar'] })
    qc.invalidateQueries({ queryKey: ['apps'] })
    qc.invalidateQueries({ queryKey: ['home'] })
    qc.invalidateQueries({ queryKey: ['notifications'] })
    refetchAll()
  }

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
  // 岗位行上的 next_action 只是历史镜像（独立待办才是唯一真相，§5.3）。只有
  // 「一条待办都没有」时才把它当迁移遗留展示；只要有待办记录（包括已完成的），
  // 它就不再是一件事——否则完成最后一个待办后，详情会出现「待办 (0)」下面还
  // 挂着一条不可操作的旧记录。后端在最后一个未完成待办完成时也会清掉这个镜像。
  const legacyNextAction = actions.length === 0 ? app.next_action : ''

  const invalidateAfterAction = () => {
    qc.invalidateQueries({ queryKey: ['actions'] })
    // completing/postponing moves the item between calendar buckets
    qc.invalidateQueries({ queryKey: ['calendar'] })
    // 今天完成 / 延期一条待办会改首页的统一待办计数与清单（还有过期提醒）。
    qc.invalidateQueries({ queryKey: ['home'] })
    qc.invalidateQueries({ queryKey: ['notifications'] })
    // 岗位行的 next_action / next_action_due_at 镜像会跟着变，数据库表格
    // 「下一步 / 截止」读的是 ['apps']，漏掉就会一直显示已完成或旧日期。
    qc.invalidateQueries({ queryKey: ['apps'] })
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
  const delActionMut = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/actions/${id}`),
    onSuccess: () => {
      setDelErr('')
      setDelAction(null)
      refreshActivity()
    },
    onError: (e: unknown) => setDelErr(e instanceof ApiError ? e.message : '删除待办失败，请重试'),
  })
  // 删除备注：行内的「删除」按钮走 DELETE /notes/:note_id。
  const delNoteMut = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/applications/${app.id}/notes/${id}`),
    onSuccess: () => {
      setDelErr('')
      setDelNote(null)
      qc.invalidateQueries({ queryKey: ['notes', app.id] })
      refetchAll()
    },
    onError: (e: unknown) => setDelErr(e instanceof ApiError ? e.message : '删除备注失败，请重试'),
  })
  // 删除面试 / OA 轮次：DELETE 会让日历、首页「即将到来的面试 / OA」和提醒一起消失。
  const delInterviewMut = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/applications/${app.id}/interviews/${id}`),
    onSuccess: () => {
      setDelErr('')
      setDelInterview(null)
      refreshActivity()
    },
    onError: (e: unknown) => setDelErr(e instanceof ApiError ? e.message : '删除面试失败，请重试'),
  })
  const delAssessmentMut = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/applications/${app.id}/assessments/${id}`),
    onSuccess: () => {
      setDelErr('')
      setDelAssessment(null)
      refreshActivity()
    },
    onError: (e: unknown) => setDelErr(e instanceof ApiError ? e.message : '删除测评失败，请重试'),
  })
  // 取消 / 恢复面试：改期或对方取消后，错的日程不该继续占着日历和每天的提醒。
  const cancelInterviewMut = useMutation({
    mutationFn: ({ id, cancel }: { id: number; cancel: boolean }) =>
      api.post(`/api/v1/applications/${app.id}/interviews/${id}/${cancel ? 'cancel' : 'uncancel'}`),
    onSuccess: () => {
      setActionErr('')
      refreshActivity()
    },
    onError: (e: unknown) => setActionErr(e instanceof ApiError ? e.message : '取消面试失败，请重试'),
  })

  // 打开删除确认前先清掉上一次的失败文案，否则会带着旧错误重新弹出来。
  const askDeleteAction = (a: ActionItem) => {
    setDelErr('')
    setDelAction(a)
  }
  const askDeleteNote = (n: Note) => {
    setDelErr('')
    setDelNote(n)
  }
  const askDeleteInterview = (i: Interview) => {
    setDelErr('')
    setDelInterview(i)
  }
  const askDeleteAssessment = (a: AssessmentRound) => {
    setDelErr('')
    setDelAssessment(a)
  }

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
        {openActions.length === 0 && !legacyNextAction ? (
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
                  <Button variant="ghost" size="sm" onClick={() => setEditAction(a)}>
                    编辑
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => askDeleteAction(a)}>
                    删除
                  </Button>
                  <Button variant="secondary" size="sm" disabled={doneMut.isPending} onClick={() => doneMut.mutate({ id: a.id, done: true })}>
                    完成
                  </Button>
                </div>
              )
            })}
            {/* legacy next_action without a standalone action still surfaces */}
            {openActions.length === 0 && legacyNextAction && (
              <div className="panel-row">
                <span className="grow">
                  <span style={{ display: 'block', fontSize: 13, fontWeight: 500 }}>{legacyNextAction}</span>
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
                    <Button variant="ghost" size="sm" onClick={() => setEditAction(a)}>
                      编辑
                    </Button>
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
                  {i.format ? ` · ${formatLabel(i.format)}` : ''}
                  {i.schedule?.cancelled && (
                    <span style={{ marginLeft: 6, fontSize: 11, color: 'var(--text-muted)', border: '1px solid var(--border)', padding: '1px 6px' }}>
                      已取消
                    </span>
                  )}
                </span>
                <span
                  style={{
                    display: 'block',
                    fontSize: 12,
                    color: 'var(--text-muted)',
                    marginTop: 2,
                    textDecoration: i.schedule?.cancelled ? 'line-through' : undefined,
                  }}
                >
                  {i.scheduled_at ? fmtDateTime(i.scheduled_at) : '时间未定'}
                  {i.schedule?.cancelled_reason ? ` · ${i.schedule.cancelled_reason}` : ''}
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
              {/* 改期 / 取消 / 删除：填错的时间必须能改，否则它会一直留在日历、
                  首页「即将到来的面试」和每天的提醒里。 */}
              <Button variant="ghost" size="sm" onClick={() => setEditInterview(i)}>
                编辑
              </Button>
              <Button
                variant="ghost"
                size="sm"
                disabled={cancelInterviewMut.isPending}
                onClick={() => cancelInterviewMut.mutate({ id: i.id, cancel: !i.schedule?.cancelled })}
              >
                {i.schedule?.cancelled ? '恢复面试' : '取消面试'}
              </Button>
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
              <Button variant="ghost" size="sm" onClick={() => askDeleteInterview(i)}>
                删除
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
                  {assessmentTiming(a)}
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
              <Button variant="ghost" size="sm" onClick={() => setEditAssessment(a)}>
                编辑
              </Button>
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
              <Button variant="ghost" size="sm" onClick={() => askDeleteAssessment(a)}>
                删除
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
              <Button variant="ghost" size="sm" onClick={() => setEditNote(n)} title="编辑这条备注">
                编辑
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => askDeleteNote(n)}
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
            // 新增必须和编辑/删除/取消走同一套刷新：否则日历 staleTime 30s、
            // 首页 10s 内还显示旧数据，刚排的面试像没存上。
            refreshActivity()
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
            refreshActivity()
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

      {editInterview && (
        <InterviewEditForm
          appId={app.id}
          interview={editInterview}
          onClose={() => setEditInterview(null)}
          onDone={() => {
            setEditInterview(null)
            refreshActivity()
          }}
        />
      )}
      {editAssessment && (
        <AssessmentEditForm
          appId={app.id}
          assessment={editAssessment}
          onClose={() => setEditAssessment(null)}
          onDone={() => {
            setEditAssessment(null)
            refreshActivity()
          }}
        />
      )}
      {editAction && (
        <ActionEditForm
          appId={app.id}
          action={editAction}
          onClose={() => setEditAction(null)}
          onDone={() => {
            setEditAction(null)
            refreshActivity()
          }}
        />
      )}
      {editNote && (
        <NoteEditForm
          appId={app.id}
          note={editNote}
          onClose={() => setEditNote(null)}
          onDone={() => {
            setEditNote(null)
            qc.invalidateQueries({ queryKey: ['notes', app.id] })
            refetchAll()
          }}
        />
      )}

      {/* 删除是硬删、不可撤销：一律先过确认弹窗。 */}
      {delInterview && (
        <ConfirmDialog
          title="删除这轮面试？"
          pending={delInterviewMut.isPending}
          error={delErr}
          onClose={() => setDelInterview(null)}
          onConfirm={() => delInterviewMut.mutate(delInterview.id)}
        >
          将永久删除「{delInterview.round_name || '面试'}」
          {delInterview.scheduled_at ? `（${fmtDateTime(delInterview.scheduled_at)}）` : ''}
          。它会从面试日历、首页「即将到来的面试」和提醒里一并消失，且无法恢复。
          <br />
          对方只是改期 / 取消了，请改用「编辑」改时间或「取消面试」——不用删掉这一轮。
        </ConfirmDialog>
      )}
      {delAssessment && (
        <ConfirmDialog
          title="删除这轮 OA / 测评？"
          pending={delAssessmentMut.isPending}
          error={delErr}
          onClose={() => setDelAssessment(null)}
          onConfirm={() => delAssessmentMut.mutate(delAssessment.id)}
        >
          将永久删除「{delAssessment.name || 'OA'}」
          {delAssessment.due_at ? `（截止 ${fmtDateTime(delAssessment.due_at)}）` : ''}，无法恢复。
          截止时间写错了请用「编辑」改。
        </ConfirmDialog>
      )}
      {delAction && (
        <ConfirmDialog
          title="删除这条待办？"
          pending={delActionMut.isPending}
          error={delErr}
          onClose={() => setDelAction(null)}
          onConfirm={() => delActionMut.mutate(delAction.id)}
        >
          将永久删除「{delAction.title}」，无法恢复。写错了请用「编辑」改——直接删掉不会影响已完成统计。
        </ConfirmDialog>
      )}
      {delNote && (
        <ConfirmDialog
          title="删除这条备注？"
          pending={delNoteMut.isPending}
          error={delErr}
          onClose={() => setDelNote(null)}
          onConfirm={() => delNoteMut.mutate(delNote.id)}
        >
          备注删除后无法恢复。只想改内容的话，点「编辑」即可。
        </ConfirmDialog>
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
  const [category, setCategory] = useState('resume')
  const [catDirty, setCatDirty] = useState(false)

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
        uploadOne(img, 'other')
      }
    }
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attachTo])

  // 上传即落库，类别无法二次询问：用户动过下拉就以他选的为准，否则按文件名猜
  // （cover_letter_stripe.pdf → 求职信；图片归「其它」）。猜错了可在行内直接改。
  const resolveCategory = (f: File): string => {
    if (catDirty) return category
    const g = f.type.startsWith('image/') ? 'other' : guessCategory(f.name)
    setCategory(g)
    return g
  }

  const uploadOne = async (file: File, cat: string) => {
    setUploading(true)
    setErr('')
    try {
      await upMut.mutateAsync({ file, category: cat })
    } catch {
      /* surfaced through the mutation's onError */
    } finally {
      setUploading(false)
    }
  }

  const upMut = useMutation({
    mutationFn: ({ file, category }: { file: File; category: string }) => {
      const fd = new FormData()
      fd.append('file', file)
      fd.append('category', category)
      fd.append('application_id', String(appId))
      // 截图/附件挂到选中的轮次（默认挂到当前正在进行的轮次）
      if (attachTo > 0) fd.append('interview_id', String(attachTo))
      return api.post('/api/v1/files', fd, true)
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '上传失败'),
  })

  // 文件拖进浏览器任意位置松开即上传（与点选同一套类别解析；多文件逐个传）。
  const drop = useDropUpload(async (files) => {
    for (const f of files) await uploadOne(f, resolveCategory(f))
  }, '松开鼠标，上传到当前岗位')

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
      {drop.banner}
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
              await uploadOne(f, resolveCategory(f))
              e.target.value = ''
            }}
          />
        </label>
        <select
          aria-label="附件类别"
          title="上传时使用的类别；粘贴的截图固定归「其它」"
          value={category}
          onChange={(e) => {
            setCategory(e.target.value)
            setCatDirty(true)
          }}
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
          {FILE_CATEGORIES.map((c) => (
            <option key={c.key} value={c.key}>
              {c.label}
            </option>
          ))}
        </select>
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
        允许 PDF / DOCX / TXT / PNG / JPEG，单文件 ≤ 20 MiB。文件可拖进页面任意位置松开上传（截图可 ⌘V
        直接粘贴，自动挂到当前轮次）。PDF / 图片 / TXT 点击文件名可在线预览（DOCX 请下载查看）；类别猜错了在行内下拉直接改。
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
  const [viewing, setViewing] = useState<FileItem | null>(null)
  // 删除的是简历原件，而「删除」就挨着「预览」「下载」——先过确认弹窗。
  const [pendingDel, setPendingDel] = useState<FileItem | null>(null)
  // 删除失败要显示在弹窗里（而不是被 backdrop 盖住的列表上方）。
  const [delErr, setDelErr] = useState('')
  const del = useMutation({
    mutationFn: (id: string) => api.del(`/api/v1/files/${id}`),
    onSuccess: () => {
      setErr('')
      setDelErr('')
      setPendingDel(null)
      qc.invalidateQueries({ queryKey: ['files'] })
    },
    onError: (e: unknown) => setDelErr(e instanceof ApiError ? e.message : '删除失败'),
  })
  const recat = useUpdateCategory(setErr)
  return (
    <>
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
            <button
              type="button"
              onClick={() => setViewing(f)}
              title="点击预览"
              style={{ padding: 0, border: 'none', background: 'none', cursor: 'pointer', lineHeight: 0, flex: '0 0 auto' }}
            >
              <FilePreview f={f} />
            </button>
            <span className="grow" style={{ minWidth: 0 }}>
              <button
                type="button"
                className="ellipsis"
                onClick={() => setViewing(f)}
                title="点击预览"
                style={{
                  display: 'block',
                  maxWidth: '100%',
                  fontSize: 14,
                  color: 'var(--text)',
                  background: 'none',
                  border: 'none',
                  padding: 0,
                  cursor: 'pointer',
                  textAlign: 'left',
                }}
              >
                {f.name}
              </button>
              <span style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-muted)' }}>
                {fmtBytes(f.size_bytes)} ·
                <CategorySelect value={f.category} disabled={recat.isPending} onChange={(c) => recat.mutate({ id: f.id, category: c })} />
                · {fmtDate(f.created_at)}
              </span>
              {f.status !== 'ready' && <span style={{ fontSize: 12, color: 'var(--danger)' }}>状态：{f.status}</span>}
            </span>
            {f.status === 'ready' && (
              <>
                <Button variant="ghost" size="sm" onClick={() => setViewing(f)}>
                  预览
                </Button>
                <LinkButton variant="ghost" size="sm" href={`/api/v1/files/${f.id}/download`} download>
                  下载
                </LinkButton>
              </>
            )}
            <Button variant="ghost" size="sm" onClick={() => { setDelErr(''); setPendingDel(f) }} title="删除文件并移除关联">
              删除
            </Button>
          </div>
        ))}
      </Card>
      {viewing && <FileViewerModal file={viewing} onClose={() => setViewing(null)} />}
      {pendingDel && (
        <ConfirmDialog
          title="删除这个附件？"
          pending={del.isPending}
          error={delErr}
          onClose={() => setPendingDel(null)}
          onConfirm={() => del.mutate(pendingDel.id)}
        >
          将永久删除「{pendingDel.name}」（{fmtBytes(pendingDel.size_bytes)}），文件原件同时从存储中移除，无法恢复。
          若只是想换一份新的，请直接上传新文件。
        </ConfirmDialog>
      )}
    </>
  )
}
