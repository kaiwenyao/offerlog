import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, fmtDate, fmtDateTime } from '../../lib/api'
import type { ActionItem, HomeSummary } from '../../lib/types'
import { statusMeta } from '../../lib/status'
import { Button, Card, PanelTitle } from '../../ds'
import { Dot, EmptyHint, ErrorText, Num, PageSpinner, StatusChip } from '../../components/ui'
import { buildWeek, CHIP_TONES, dueTime, groupActions, startOfDay, type TodoItem } from './week'

const PANEL: React.CSSProperties = { padding: 0, overflow: 'hidden' }

/**
 * Today dashboard backed by /api/v1/home/summary (server-side full-data
 * aggregates). Counts are never extrapolated from a 200-row page; upcoming
 * interviews are cross-application and sorted by actual scheduled time; the
 * todo count and checklist share one data source (standalone actions +
 * legacy derived next_actions). Error states show a retry entry rather than a
 * misleading "nothing here".
 */
export function TodayPage() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [toast, setToast] = useState('')

  const summaryQ = useQuery({
    queryKey: ['home', 'summary'],
    queryFn: () => api.get<HomeSummary>('/api/v1/home/summary?limit=5'),
  })

  const actionsQ = useQuery({
    queryKey: ['actions', 'open'],
    queryFn: () =>
      api.get<{ items: Array<ActionItem & { company_name?: string; position?: string; status?: string }> }>(
        '/api/v1/actions?open=1',
      ),
  })

  const doneMut = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/actions/${id}/done`, { done: true }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['actions'] })
      qc.invalidateQueries({ queryKey: ['home'] })
    },
    onError: (e: unknown) => setToast(e instanceof ApiError ? e.message : '操作失败'),
  })

  const postponeMut = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/actions/${id}/postpone`, { days: 1 }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['actions'] })
      qc.invalidateQueries({ queryKey: ['home'] })
    },
    onError: (e: unknown) => setToast(e instanceof ApiError ? e.message : '延期失败'),
  })

  const summary = summaryQ.data

  const groups = useMemo(() => {
    const items: TodoItem[] = (actionsQ.data?.items ?? []).map((a) => ({
      ...a,
      company_name: a.company_name ?? '',
      position: a.position ?? '',
      status: a.status ?? '',
    }))
    return groupActions(items)
  }, [actionsQ.data])

  const week = useMemo(() => (summary ? buildWeek(summary) : []), [summary])

  if (summaryQ.isLoading) return <PageSpinner />
  if (summaryQ.isError) {
    return (
      <section style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <Card padding="18px">
          <ErrorText>首页数据加载失败，请检查网络后重试。</ErrorText>
          <Button variant="primary" size="sm" onClick={() => summaryQ.refetch()} style={{ marginTop: 12 }}>
            重新加载
          </Button>
        </Card>
      </section>
    )
  }
  if (!summary) return <PageSpinner />

  const weekTotals = {
    submitted: summary.submitted_week,
    interviews: summary.interviews_week,
    replied: summary.replied_week,
    overdue: summary.todos.overdue,
  }

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      {toast && <ErrorText>{toast}</ErrorText>}

      <WeekStrip week={week} totals={weekTotals} />

      <div className="metric-grid">
        <Kpi label="进行中" value={summary.in_progress} delta="applied…interviewing（不含归档）" />
        <Kpi label="本周投递" value={summary.submitted_week} delta="本周内发生的投递" />
        <Kpi label="待回复" value={summary.awaiting_reply} delta="已投递且尚无首次回复（不含归档）" />
        <Kpi
          label="逾期待办"
          value={summary.todos.overdue}
          delta={summary.todos.overdue > 0 ? '需要今天处理' : '没有逾期'}
          deltaColor={summary.todos.overdue > 0 ? 'var(--danger)' : 'var(--positive)'}
        />
      </div>

      {actionsQ.isError && (
        <ErrorText>待办清单加载失败，请稍后重试</ErrorText>
      )}

      {groups.length === 0 && !actionsQ.isError && (
        <EmptyHint>
          <p style={{ margin: 0, font: 'var(--type-body-sm)' }}>今天没有待办 🎉</p>
          <p style={{ margin: 0, fontSize: 13 }}>在岗位详情添加“下一步行动”或独立待办，就会出现在这里。</p>
          <Button variant="primary" size="sm" onClick={() => nav('/database')}>
            去添加岗位
          </Button>
        </EmptyHint>
      )}

      <div className="split-grid">
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {groups.map((g) => (
            <Card key={g.title} style={PANEL}>
              <div className="panel-head">
                <Dot color={g.dot} />
                <PanelTitle>{g.title}</PanelTitle>
                <Num color="var(--text-muted)">{g.items.length}</Num>
              </div>
              {g.items.map((t) => (
                <TodoRow
                  key={t.id}
                  item={t}
                  busy={doneMut.isPending || postponeMut.isPending}
                  onOpen={() => t.application_id && nav(`/apps/${t.application_id}`)}
                  onDone={() => doneMut.mutate(t.id)}
                  onPostpone={() => postponeMut.mutate(t.id)}
                />
              ))}
            </Card>
          ))}
          {actionsQ.isLoading && <PageSpinner />}
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <UpcomingInterviews
            upcoming={summary.upcoming}
            loading={summaryQ.isLoading}
            onOpen={(id) => nav(`/apps/${id}`)}
          />
          <ActivityFeed rows={summary.recent} onOpen={(id) => nav(`/apps/${id}`)} />
        </div>
      </div>

      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 }}>{summary.scope_note}</p>
    </section>
  )
}

function Kpi({
  label,
  value,
  delta,
  deltaColor = 'var(--text-muted)',
}: {
  label: string
  value: number | string
  delta: string
  deltaColor?: string
}) {
  return (
    <Card padding="14px 16px">
      <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>{label}</div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 4 }}>
        <span style={{ font: 'var(--type-h3)', letterSpacing: 'var(--tracking-display)' }}>{value}</span>
      </div>
      <div style={{ fontSize: 12, color: deltaColor, marginTop: 4 }}>{delta}</div>
    </Card>
  )
}

function WeekStrip({
  week,
  totals,
}: {
  week: ReturnType<typeof buildWeek>
  totals: { submitted: number; interviews: number; replied: number; overdue: number }
}) {
  return (
    <Card padding="16px">
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, flexWrap: 'wrap', marginBottom: 14 }}>
        <span style={{ font: 'var(--type-ui)', fontSize: 17, fontFamily: 'var(--font-display)', letterSpacing: 'var(--tracking-display)' }}>
          本周进展
        </span>
        <span style={{ marginLeft: 'auto', fontSize: 13, color: 'var(--text-muted)' }}>
          投递 <Num color="var(--text)">{totals.submitted}</Num> · 面试 <Num color="var(--text)">{totals.interviews}</Num> ·
          回复 <Num color="var(--text)">{totals.replied}</Num> · 逾期{' '}
          <Num color={totals.overdue > 0 ? 'var(--danger)' : 'var(--text)'}>{totals.overdue}</Num>
        </span>
      </div>
      <div style={{ overflowX: 'auto', overflowY: 'hidden', paddingBottom: 2 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7,minmax(100px,1fr))', gap: 8, minWidth: 756 }}>
          {week.map((d) => (
            <div
              key={d.weekday}
              style={{
                minHeight: 112,
                padding: 8,
                borderRadius: 12,
                background: d.isToday ? 'var(--accent-soft)' : 'var(--surface-thin)',
                border: '1px solid ' + (d.isToday ? 'var(--accent-border)' : 'var(--border-alt)'),
                display: 'flex',
                flexDirection: 'column',
                gap: 6,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{d.weekday}</span>
                <Num color={d.isToday ? 'var(--accent)' : 'var(--text)'}>{d.dayNum}</Num>
              </div>
              {d.items.slice(0, 3).map((e, i) => (
                <div
                  key={i}
                  style={{
                    padding: '4px 6px',
                    borderRadius: 8,
                    background: CHIP_TONES[e.tone].bg,
                    color: CHIP_TONES[e.tone].fg,
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                  }}
                >
                  <span style={{ display: 'block', fontSize: 11, opacity: 0.85, lineHeight: 1.25 }}>{e.kind}</span>
                  <span
                    className="ellipsis"
                    style={{ display: 'block', fontSize: 12, fontWeight: 500, lineHeight: 1.25 }}
                  >
                    {e.who}
                  </span>
                </div>
              ))}
              {d.items.length > 3 && (
                <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>+{d.items.length - 3}</span>
              )}
            </div>
          ))}
        </div>
      </div>
    </Card>
  )
}

function TodoRow({
  item,
  busy,
  onOpen,
  onDone,
  onPostpone,
}: {
  item: TodoItem
  busy: boolean
  onOpen: () => void
  onDone: () => void
  onPostpone: () => void
}) {
  const due = item.due_ts ?? item.due_date
  const overdue = due != null && new Date(due).getTime() < startOfDay()
  return (
    <div className="panel-row">
      <span className="grow" onClick={onOpen} style={{ cursor: 'pointer' }}>
        <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span className="ellipsis" style={{ fontSize: 14, fontWeight: 500 }}>
            {item.company_name ?? '未关联岗位'}
          </span>
          {item.position && (
            <span className="ellipsis" style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              {item.position}
            </span>
          )}
          {item.status && <StatusChip status={item.status} />}
        </span>
        <span style={{ display: 'block', fontSize: 13, marginTop: 2 }}>{item.title}</span>
      </span>
      <Num color={overdue ? 'var(--danger)' : 'var(--text-muted)'}>
        {overdue ? `逾期 ${fmtDate(due)}` : fmtDate(due)}
      </Num>
      {overdue && (
        <Button variant="ghost" size="sm" disabled={busy} onClick={onPostpone} title="延期一天">
          延期
        </Button>
      )}
      <Button variant="secondary" size="sm" disabled={busy} onClick={onDone}>
        完成
      </Button>
    </div>
  )
}

function UpcomingInterviews({
  upcoming,
  loading,
  onOpen,
}: {
  upcoming: HomeSummary['upcoming']
  loading: boolean
  onOpen: (id: number) => void
}) {
  if (loading) return null
  return (
    <Card style={PANEL}>
      <div className="panel-head">
        <PanelTitle>即将到来的面试</PanelTitle>
      </div>
      {upcoming.length === 0 ? (
        <div style={{ padding: '14px 16px', fontSize: 13, color: 'var(--text-muted)' }}>
          还没有排期的面试。在岗位详情里「＋ 安排」一轮面试后会显示在这里。
        </div>
      ) : (
        upcoming.map((i) => {
          const at = new Date(i.scheduled_at)
          return (
            <button key={i.id} className="panel-row" onClick={() => onOpen(i.application_id)}>
              <span
                style={{
                  width: 44,
                  textAlign: 'center',
                  padding: '4px 0',
                  borderRadius: 10,
                  background: 'var(--surface)',
                  border: '1px solid var(--border-alt)',
                  flex: '0 0 auto',
                }}
              >
                <span style={{ display: 'block', fontFamily: 'var(--font-mono)', fontSize: 14, fontWeight: 500 }}>
                  {String(at.getDate()).padStart(2, '0')}
                </span>
                <span style={{ display: 'block', fontSize: 11, color: 'var(--text-muted)' }}>{at.getMonth() + 1} 月</span>
              </span>
              <span className="grow">
                <span className="ellipsis" style={{ display: 'block', fontSize: 14, fontWeight: 500 }}>
                  {i.company_name} · {i.round_name || '面试'}
                </span>
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                  {fmtDateTime(i.scheduled_at)} · {i.format || '待定'}
                  {i.location ? ` · ${i.location}` : ''}
                </span>
              </span>
            </button>
          )
        })
      )}
    </Card>
  )
}

function ActivityFeed({ rows, onOpen }: { rows: HomeSummary['recent']; onOpen: (id: number) => void }) {
  return (
    <Card style={PANEL}>
      <div className="panel-head">
        <PanelTitle>最近动态</PanelTitle>
      </div>
      <div style={{ padding: '12px 16px 14px', display: 'flex', flexDirection: 'column', gap: 2 }}>
        {rows.length === 0 && <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>还没有记录</span>}
        {rows.map((r) => (
          <button
            key={r.id}
            className="menu-item"
            style={{ alignItems: 'flex-start', padding: '5px 0' }}
            onClick={() => onOpen(r.id)}
          >
            <span style={{ marginTop: 5 }}>
              <Dot color={statusMeta(r.status).dot} />
            </span>
            <span className="grow" style={{ fontSize: 13, lineHeight: 1.45, textAlign: 'left' }}>
              {r.company_name} · {statusMeta(r.status).label}
              {r.next_action ? ` — ${r.next_action}` : ''}
            </span>
            <Num color="var(--text-muted)">{fmtDate(r.updated_at)}</Num>
          </button>
        ))}
      </div>
    </Card>
  )
}
