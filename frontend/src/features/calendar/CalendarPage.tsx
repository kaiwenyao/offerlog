import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, fmtDateTime } from '../../lib/api'
import type { CalendarEvent } from '../../lib/types'
import { Button, Card, PanelTitle, Tabs } from '../../ds'
import { Dot, EmptyHint, ErrorText, Num, PageSpinner } from '../../components/ui'
import {
  addDays,
  agenda,
  monthGrid,
  mondayOf,
  monthStart,
  sameLocalDay,
  toISO,
  weekColumns,
  WEEKDAYS,
  type ViewMode,
} from './grid'

function toneOf(e: CalendarEvent): string {
  if (e.cancelled) return 'var(--neutral)'
  if (e.done) return 'var(--text-muted)'
  switch (e.kind) {
    case 'interview':
      return 'var(--accent)'
    case 'action':
      return 'var(--warning)'
    case 'offer_decision':
      return 'var(--positive)'
    default:
      return 'var(--info)'
  }
}

function kindLabel(e: CalendarEvent): string {
  if (e.kind === 'interview') return e.round_name || '面试'
  if (e.kind === 'action') return '待办'
  if (e.kind === 'offer_decision') return 'Offer 答复'
  return '截止'
}

function evTime(e: CalendarEvent): string {
  if (e.all_day) return '全天'
  if (!e.start) return ''
  return fmtDateTime(e.start)
}

/** Month label for the range header. */
function fmtMonth(d: Date): string {
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月`
}

export function CalendarPage() {
  const nav = useNavigate()
  const [view, setView] = useState<ViewMode>('week')
  const [anchor, setAnchor] = useState(() => mondayOf())

  const from = useMemo(() => {
    if (view === 'week') return anchor
    if (view === 'month') return monthStart(anchor)
    return mondayOf(anchor)
  }, [view, anchor])

  const to = useMemo(() => {
    if (view === 'week') return addDays(anchor, 7)
    if (view === 'month') return addDays(monthStart(anchor), 42)
    return addDays(from, 14)
  }, [view, anchor, from])

  const q = useQuery({
    queryKey: ['calendar', from.toISOString(), to.toISOString()],
    queryFn: () =>
      api.get<{ items: CalendarEvent[] }>(
        `/api/v1/calendar?from=${encodeURIComponent(toISO(from))}&to=${encodeURIComponent(toISO(to))}`,
      ),
    staleTime: 30_000,
  })

  const events = q.data?.items ?? []

  const weeks = useMemo(() => (view === 'month' ? monthGrid(events, monthStart(anchor)) : []), [view, events, anchor])
  const days = useMemo(() => (view === 'week' ? weekColumns(events, anchor) : []), [view, events, anchor])
  const agendaGroups = useMemo(() => (view === 'agenda' ? agenda(events) : []), [view, events])

  const navTitle =
    view === 'week'
      ? `${anchor.getFullYear()} 年 ${anchor.getMonth() + 1} 月 ${anchor.getDate()} 日起`
      : view === 'month'
        ? fmtMonth(anchor)
        : '未来议程'

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <Tabs
          items={[
            { value: 'week', label: '周' },
            { value: 'month', label: '月' },
            { value: 'agenda', label: '议程' },
          ]}
          value={view}
          onChange={(v) => setView(v as ViewMode)}
          ariaLabel="日历视图"
        />
        <b style={{ fontSize: 15, minWidth: 150 }}>{navTitle}</b>
        <span style={{ display: 'flex', gap: 6 }}>
          <Button variant="ghost" size="sm" onClick={() => setAnchor((a) => addDays(a, -7))}>
            上一周
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setAnchor(mondayOf())}>
            今天
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setAnchor((a) => addDays(a, 7))}>
            下一周
          </Button>
        </span>
        <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)' }}>
          已汇总面试 / 待办 / 截止 · 半开区间
        </span>
      </div>

      {q.isLoading ? (
        <PageSpinner />
      ) : q.isError ? (
        <Card padding="18px">
          <ErrorText>日程加载失败</ErrorText>
          <Button variant="secondary" size="sm" onClick={() => q.refetch()} style={{ marginTop: 10 }}>
            重试
          </Button>
        </Card>
      ) : events.length === 0 && view !== 'agenda' ? (
        <EmptyHint>
          <p style={{ margin: 0 }}>这个时间范围没有日程</p>
          <p style={{ margin: 0, fontSize: 13 }}>面试、待办截止和岗位截止日期会出现在日历上。</p>
        </EmptyHint>
      ) : view === 'week' ? (
        <WeekView days={days} onOpen={(id) => nav(`/apps/${id}`)} />
      ) : view === 'month' ? (
        <MonthView weeks={weeks} onOpen={(id) => nav(`/apps/${id}`)} />
      ) : (
        <AgendaView groups={agendaGroups} onOpen={(id) => nav(`/apps/${id}`)} />
      )}
    </section>
  )
}

function WeekView({ days, onOpen }: { days: ReturnType<typeof weekColumns>; onOpen: (id: number) => void }) {
  const today = new Date()
  return (
    <div style={{ overflowX: 'auto', paddingBottom: 2 }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7,minmax(130px,1fr))', gap: 8, minWidth: 920 }}>
        {days.map((d) => (
          <div
            key={d.date.toISOString()}
            style={{
              minHeight: 340,
              borderRadius: 12,
              padding: 8,
              background: d.isToday ? 'var(--accent-soft)' : 'var(--surface-thin)',
              border: '1px solid ' + (d.isToday ? 'var(--accent-border)' : 'var(--border-alt)'),
            }}
          >
            <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                {WEEKDAYS[(d.date.getDay() + 6) % 7]}
              </span>
              <Num color={d.isToday ? 'var(--accent)' : 'var(--text)'}>{d.date.getDate()}</Num>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 8 }}>
              {d.events.length === 0 && (
                <span style={{ fontSize: 12, color: 'var(--text-muted)', opacity: 0.7 }}>—</span>
              )}
              {d.events.map((e) => {
                const past = e.start && new Date(e.start).getTime() < today.setHours(0, 0, 0, 0)
                return (
                  <button
                    key={`${e.kind}-${e.id}`}
                    className="board-card"
                    style={{ opacity: e.cancelled || past || e.done ? 0.55 : 1, textAlign: 'left' }}
                    onClick={() => onOpen(e.application_id)}
                  >
                    <span style={{ display: 'flex', alignItems: 'center', gap: 5, fontSize: 11 }}>
                      <Dot color={toneOf(e)} size={6} />
                      <b style={{ fontSize: 11, fontWeight: 600 }}>{kindLabel(e)}</b>
                      <span style={{ color: 'var(--text-muted)' }}>{evTime(e)}</span>
                    </span>
                    <span className="ellipsis" style={{ display: 'block', fontSize: 12, marginTop: 3 }}>
                      {e.company_name || e.title}
                    </span>
                    {e.location && (
                      <span className="ellipsis" style={{ display: 'block', fontSize: 11, color: 'var(--text-muted)' }}>
                        📍 {e.location}
                      </span>
                    )}
                  </button>
                )
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function MonthView({ weeks, onOpen }: { weeks: ReturnType<typeof monthGrid>; onOpen: (id: number) => void }) {
  return (
    <Card padding={0} style={{ overflow: 'hidden' }}>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(7,1fr)',
          background: 'var(--surface-thin)',
          borderBottom: '1px solid var(--border-alt)',
        }}
      >
        {WEEKDAYS.map((d) => (
          <div key={d} style={{ padding: '8px 10px', fontSize: 12, color: 'var(--text-muted)' }}>
            {d}
          </div>
        ))}
      </div>
      {weeks.map((row, wi) => (
        <div key={wi} style={{ display: 'grid', gridTemplateColumns: 'repeat(7,1fr)' }}>
          {row.map((cell, ci) => (
            <div
              key={`${wi}-${ci}`}
              style={{
                minHeight: 88,
                borderRight: ci < 6 ? '1px solid var(--border-alt)' : 0,
                borderBottom: wi < weeks.length - 1 ? '1px solid var(--border-alt)' : 0,
                padding: 5,
                background: cell.inMonth ? 'transparent' : 'var(--surface-thin)',
                opacity: cell.inMonth ? 1 : 0.5,
              }}
            >
              <div style={{ fontSize: 11, color: cell.isToday ? 'var(--accent)' : 'var(--text-muted)', fontWeight: cell.isToday ? 600 : 400 }}>
                {cell.date.getDate()}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 2, marginTop: 3 }}>
                {cell.events.slice(0, 3).map((e) => (
                  <button
                    key={`${e.kind}-${e.id}`}
                    className="board-card"
                    onClick={() => onOpen(e.application_id)}
                    style={{ padding: '2px 4px', fontSize: 10, textAlign: 'left', opacity: e.cancelled ? 0.5 : 1 }}
                  >
                    <Dot color={toneOf(e)} size={5} /> {e.company_name || e.title}
                  </button>
                ))}
                {cell.events.length > 3 && (
                  <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>+{cell.events.length - 3}</span>
                )}
              </div>
            </div>
          ))}
        </div>
      ))}
    </Card>
  )
}

function AgendaView({ groups, onOpen }: { groups: Array<{ title: string; items: CalendarEvent[] }>; onOpen: (id: number) => void }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {groups.length === 0 && (
        <EmptyHint>
          <p style={{ margin: 0 }}>近期没有日程</p>
        </EmptyHint>
      )}
      {groups.map((g) => (
        <Card key={g.title} padding={0} style={{ overflow: 'hidden' }}>
          <div className="panel-head">
            <PanelTitle>{g.title}</PanelTitle>
            <Num color="var(--text-muted)">{g.items.length}</Num>
          </div>
          {g.items.map((e) => {
            const start = e.start ?? e.dueDate
            return (
              <button key={`${e.kind}-${e.id}`} className="panel-row" onClick={() => onOpen(e.application_id)}>
                <Dot color={toneOf(e)} />
                <span className="grow" style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
                  <span style={{ fontSize: 14, fontWeight: 500 }}>
                    {e.company_name || e.title}
                    <span style={{ color: 'var(--text-muted)', fontSize: 12, fontWeight: 400 }}>
                      {' '}
                      · {e.position}
                    </span>
                  </span>
                  <span className="ellipsis" style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    {kindLabel(e)} · {start ? fmtDateTime(start) : '无时间'}
                    {e.location ? ` · ${e.location}` : ''}
                  </span>
                </span>
                {sameLocalDay(start ? new Date(start) : new Date(0), new Date()) && (
                  <span style={{ fontSize: 11, color: 'var(--accent)' }}>今天</span>
                )}
              </button>
            )
          })}
        </Card>
      ))}
    </div>
  )
}
