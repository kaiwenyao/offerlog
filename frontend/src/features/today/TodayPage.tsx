import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, fmtDate, fmtDateTime } from '../../lib/api'
import type { ActionItem, AppRow, Interview } from '../../lib/types'
import { statusMeta } from '../../lib/status'
import { Button, Card, PanelTitle } from '../../ds'
import { Dot, EmptyHint, ErrorText, Num, PageSpinner, StatusChip } from '../../components/ui'
import { buildKpis, buildWeek, CHIP_TONES, dueTime, groupActions, startOfDay, type TodoItem } from './week'

const PANEL: React.CSSProperties = { padding: 0, overflow: 'hidden' }

export function TodayPage() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [toast, setToast] = useState('')

  const actionsQ = useQuery({
    queryKey: ['actions', 'open'],
    queryFn: () => api.get<{ items: Array<ActionItem & { company_name?: string }> }>('/api/v1/actions?open=1'),
  })
  const appsQ = useQuery({
    queryKey: ['apps', 'list', { page: 1, size: 200 }],
    queryFn: () => api.get<{ items: AppRow[] }>('/api/v1/applications?page=1&page_size=200'),
  })

  const doneMut = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/actions/${id}/done`, { done: true }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['actions'] })
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (e: unknown) => setToast(e instanceof ApiError ? e.message : '操作失败'),
  })

  const rows = useMemo(() => appsQ.data?.items ?? [], [appsQ.data])

  const groups = useMemo(() => {
    const soon = Date.now() + 8 * 86_400_000
    const fromApps: TodoItem[] = rows
      .filter((a) => a.next_action && !a.archived)
      .filter((a) => dueTime({ due_ts: null, due_date: a.next_action_due_at }) < soon)
      .map((a) => ({
        id: -a.id,
        application_id: a.id,
        title: a.next_action,
        due_date: a.next_action_due_at,
        due_ts: null,
        done_at: null,
        remind_me: false,
        created_at: a.created_at,
        company_name: a.company_name,
        position: a.position,
        status: a.status,
      }))
    return groupActions([...fromApps, ...(actionsQ.data?.items ?? [])])
  }, [rows, actionsQ.data])

  const week = useMemo(() => buildWeek(rows), [rows])
  const kpis = useMemo(() => buildKpis(rows), [rows])

  if (appsQ.isLoading || actionsQ.isLoading) return <PageSpinner />

  const weekTotals = {
    submitted: kpis.submittedThisWeek,
    interviews: rows.filter((r) => r.status === 'interviewing').length,
    replied: rows.filter((r) => r.first_response_at).length,
    overdue: kpis.overdue,
  }

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      {toast && <ErrorText>{toast}</ErrorText>}

      <WeekStrip week={week} totals={weekTotals} />

      <div className="metric-grid">
        <Kpi label="进行中" value={kpis.inProgress} delta="applied…interviewing" />
        <Kpi label="本周投递" value={kpis.submittedThisWeek} delta="按投递时间统计" />
        <Kpi label="待回复" value={kpis.awaitingReply} delta="已投递且尚无首次回复" deltaColor="var(--text-muted)" />
        <Kpi
          label="逾期待办"
          value={kpis.overdue}
          delta={kpis.overdue > 0 ? '需要今天处理' : '没有逾期'}
          deltaColor={kpis.overdue > 0 ? 'var(--danger)' : 'var(--positive)'}
        />
      </div>

      {groups.length === 0 && (
        <EmptyHint>
          <p style={{ margin: 0, font: 'var(--type-body-sm)' }}>今天没有待办 🎉</p>
          <p style={{ margin: 0, fontSize: 13 }}>在数据库中给岗位填写“下一步行动”与截止时间，就会出现在这里。</p>
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
                  busy={doneMut.isPending}
                  onOpen={() => t.application_id && nav(`/apps/${t.application_id}`)}
                  onDone={() => doneMut.mutate(t.id)}
                />
              ))}
            </Card>
          ))}
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <UpcomingInterviews rows={rows} onOpen={(id) => nav(`/apps/${id}`)} />
          <ActivityFeed rows={rows} onOpen={(id) => nav(`/apps/${id}`)} />
        </div>
      </div>
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
}: {
  item: TodoItem
  busy: boolean
  onOpen: () => void
  onDone: () => void
}) {
  const due = item.due_ts ?? item.due_date
  const overdue = due != null && new Date(due).getTime() < startOfDay()
  // Negative ids are synthesised from an application's next_action; they are
  // resolved by editing the application, not by ticking a standalone action.
  const isDerived = item.id < 0
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
      <Button variant="secondary" size="sm" disabled={busy} onClick={isDerived ? onOpen : onDone}>
        {isDerived ? '更新' : '完成'}
      </Button>
    </div>
  )
}

function UpcomingInterviews({ rows, onOpen }: { rows: AppRow[]; onOpen: (id: number) => void }) {
  const candidates = rows.filter((r) => r.status === 'interviewing' || r.status === 'screening').slice(0, 6)
  const ids = candidates.map((r) => r.id)

  const q = useQuery({
    queryKey: ['interviews', 'upcoming', ids],
    enabled: ids.length > 0,
    queryFn: async () => {
      const lists = await Promise.all(
        ids.map((id) =>
          api
            .get<{ items: Interview[] }>(`/api/v1/applications/${id}/interviews`)
            .then((r) => (r.items ?? []).map((i) => ({ ...i, application_id: id })))
            .catch(() => [] as Interview[]),
        ),
      )
      return lists.flat()
    },
  })

  const upcoming = (q.data ?? [])
    .filter((i) => i.scheduled_at && new Date(i.scheduled_at).getTime() >= startOfDay())
    .sort((a, b) => new Date(a.scheduled_at!).getTime() - new Date(b.scheduled_at!).getTime())
    .slice(0, 4)

  const nameOf = (id: number) => rows.find((r) => r.id === id)?.company_name ?? `#${id}`

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
          const at = new Date(i.scheduled_at!)
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
                  {nameOf(i.application_id)} · {i.round_name || '面试'}
                </span>
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                  {fmtDateTime(i.scheduled_at)} · {i.format || '待定'}
                </span>
              </span>
            </button>
          )
        })
      )}
    </Card>
  )
}

function ActivityFeed({ rows, onOpen }: { rows: AppRow[]; onOpen: (id: number) => void }) {
  const recent = [...rows]
    .filter((r) => !r.deleted)
    .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
    .slice(0, 6)
  return (
    <Card style={PANEL}>
      <div className="panel-head">
        <PanelTitle>最近动态</PanelTitle>
      </div>
      <div style={{ padding: '12px 16px 14px', display: 'flex', flexDirection: 'column', gap: 2 }}>
        {recent.length === 0 && <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>还没有记录</span>}
        {recent.map((r) => (
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
