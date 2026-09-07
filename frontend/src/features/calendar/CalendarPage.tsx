import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, dayToInstant, fmtDateTime } from '../../lib/api'
import type { CalendarEvent } from '../../lib/types'
import { effectiveZone } from '../../lib/tz'
import { Button, Card, PanelTitle, Tabs } from '../../ds'
import { Dot, EmptyHint, ErrorText, Num, PageSpinner } from '../../components/ui'
import {
  addDaysToKey,
  agenda,
  agendaWindowKeys,
  dayKeyInZone,
  monthGrid,
  monthKeyOf,
  mondayKeyOf,
  todayKeyInZone,
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

/** Display helpers for YYYY-MM-DD keys. */
function keyDayNum(key: string): number {
  return Number(key.slice(8, 10))
}
function fmtMonthKey(key: string): string {
  const y = key.slice(0, 4)
  const m = Number(key.slice(5, 7))
  return `${y} 年 ${m} 月`
}
/** Weekday label index 0=Mon..6=Sun for a day key. */
function weekdayIdxOf(key: string): number {
  const t = new Date(`${key}T00:00:00Z`)
  return (t.getUTCDay() + 6) % 7
}

export function CalendarPage() {
  const nav = useNavigate()
  const zone = effectiveZone()
  const [view, setView] = useState<ViewMode>('week')
  // Anchor is a day KEY in the active zone. Week view navigates by Monday
  // weeks; month view by month keys; agenda by the current week.
  const [weekKey, setWeekKey] = useState(() => mondayKeyOf(todayKeyInZone(zone)))
  const [monthKey, setMonthKey] = useState(() => monthKeyOf(todayKeyInZone(zone)))

  // Fetch window (instants) computed from the anchor keys. The boundary keys
  // are user-zone calendar days; their instants are the user's local midnights
  // (dayToInstant), so the server's AT TIME ZONE comparisons line up.
  const { from, to } = useMemo(() => {
    const instantOf = (key: string): Date => {
      const ms = dayToInstant(key, zone)
      return new Date(ms ?? Date.parse(`${key}T00:00:00Z`))
    }
    if (view === 'week') {
      return { from: instantOf(weekKey), to: instantOf(addDaysToKey(weekKey, 7)) }
    }
    if (view === 'month') {
      const gridStart = mondayKeyOf(monthKey)
      return { from: instantOf(gridStart), to: instantOf(addDaysToKey(gridStart, 42)) }
    }
    // agenda: current Monday week −90d … +30d, computed as pure day keys in
    // the user zone and converted to user-local-midnight instants — the window
    // must never be derived from the browser zone (that drifted the `to` edge
    // and silently cut +29/+30-day events).
    const monday = mondayKeyOf(todayKeyInZone(zone))
    const { fromKey, toKey } = agendaWindowKeys(monday)
    return { from: instantOf(fromKey), to: instantOf(toKey) }
  }, [view, weekKey, monthKey, zone])

  const q = useQuery({
    queryKey: ['calendar', view, from.toISOString(), to.toISOString()],
    queryFn: () =>
      api.get<{ items: CalendarEvent[] }>(
        `/api/v1/calendar?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`,
      ),
    staleTime: 30_000,
  })

  const events = q.data?.items ?? []

  const weeks = useMemo(() => (view === 'month' ? monthGrid(events, monthKey, zone) : []), [view, events, monthKey, zone])
  const days = useMemo(() => (view === 'week' ? weekColumns(events, weekKey, zone) : []), [view, events, weekKey, zone])
  const agendaGroups = useMemo(() => (view === 'agenda' ? agenda(events, new Date(), zone) : []), [view, events, zone])

  const navTitle =
    view === 'week'
      ? `${fmtMonthKey(weekKey)} ${weekKey.slice(8, 10)} 日起`
      : view === 'month'
        ? fmtMonthKey(monthKey)
        : '议程'

  const shiftMonth = (k: string, n: number): string => {
    const y = Number(k.slice(0, 4))
    const m = Number(k.slice(5, 7)) + n
    const ny = y + Math.floor((m - 1) / 12)
    const nm = ((m - 1) % 12 + 12) % 12 + 1
    return `${ny}-${String(nm).padStart(2, '0')}-01`
  }
  const goPrev = () => {
    if (view === 'week') setWeekKey((k) => addDaysToKey(k, -7))
    else if (view === 'month') setMonthKey((k) => shiftMonth(k, -1))
  }
  const goNext = () => {
    if (view === 'week') setWeekKey((k) => addDaysToKey(k, 7))
    else if (view === 'month') setMonthKey((k) => shiftMonth(k, 1))
  }
  const goToday = () => {
    const today = todayKeyInZone(zone)
    setWeekKey(mondayKeyOf(today))
    setMonthKey(monthKeyOf(today))
  }

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
          <Button variant="ghost" size="sm" onClick={goPrev}>
            上一周
          </Button>
          <Button variant="ghost" size="sm" onClick={goToday}>
            今天
          </Button>
          <Button variant="ghost" size="sm" onClick={goNext}>
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
  return (
    <div style={{ overflowX: 'auto', paddingBottom: 2 }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7,minmax(130px,1fr))', gap: 8, minWidth: 920 }}>
        {days.map((d) => (
          <div
            key={d.key}
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
                {WEEKDAYS[weekdayIdxOf(d.key)]}
              </span>
              <Num color={d.isToday ? 'var(--accent)' : 'var(--text)'}>{keyDayNum(d.key)}</Num>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 8 }}>
              {d.events.length === 0 && (
                <span style={{ fontSize: 12, color: 'var(--text-muted)', opacity: 0.7 }}>—</span>
              )}
              {d.events.map((e) => (
                <button
                  key={`${e.kind}-${e.id}`}
                  className="board-card"
                  style={{ opacity: e.cancelled || e.done ? 0.55 : 1, textAlign: 'left' }}
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
              ))}
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
                {keyDayNum(cell.key)}
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
          {g.items.map((e) => (
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
                  {kindLabel(e)} · {evTime(e)}
                  {e.location ? ` · ${e.location}` : ''}
                </span>
              </span>
            </button>
          ))}
        </Card>
      ))}
    </div>
  )
}
