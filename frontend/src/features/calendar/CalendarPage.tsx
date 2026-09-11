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
    case 'assessment':
      return 'var(--warning)'
    case 'assessment_due':
      return 'var(--danger)'
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
  if (e.kind === 'assessment') return e.round_name || 'OA'
  if (e.kind === 'assessment_due') return e.round_name ? `${e.round_name} 截止` : 'OA 截止'
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

/**
 * Event chip in the Industry idiom: a flat neutral body with a coloured
 * leading rule — the same tape treatment as the home week strip.
 */
function EventChip({
  event,
  onOpen,
  compact = false,
}: {
  event: CalendarEvent
  onOpen: (id: number) => void
  compact?: boolean
}) {
  const dimmed = event.cancelled || event.done
  return (
    <button
      type="button"
      onClick={() => onOpen(event.application_id)}
      style={{
        all: 'unset',
        boxSizing: 'border-box',
        cursor: 'pointer',
        display: 'block',
        width: '100%',
        padding: compact ? '3px 5px' : '4px 6px',
        background: 'var(--neutral-100)',
        borderLeft: '2px solid ' + toneOf(event),
        opacity: dimmed ? 0.55 : 1,
        overflow: 'hidden',
      }}
    >
      <span
        style={{
          display: 'block',
          fontSize: 10,
          letterSpacing: '.08em',
          color: 'var(--neutral-700)',
          fontVariantNumeric: 'tabular-nums',
          lineHeight: 1.3,
        }}
      >
        {kindLabel(event)} {evTime(event)}
      </span>
      <span
        className="ellipsis"
        style={{ display: 'block', fontSize: compact ? 11 : 12, fontWeight: 500, lineHeight: 1.3 }}
      >
        {event.company_name || event.title}
      </span>
    </button>
  )
}

function WeekView({ days, onOpen }: { days: ReturnType<typeof weekColumns>; onOpen: (id: number) => void }) {
  return (
    <div style={{ overflowX: 'auto' }}>
      <div className="mesh" style={{ gridTemplateColumns: 'repeat(7,minmax(130px,1fr))', minWidth: 920 }}>
        {days.map((d) => (
          <div
            key={d.key}
            style={{
              minHeight: 340,
              padding: '9px 9px 11px',
              background: d.isToday ? 'var(--accent-100)' : 'var(--bg)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
              <span style={{ fontSize: 11, letterSpacing: '.1em', color: 'var(--neutral-600)' }}>
                {WEEKDAYS[weekdayIdxOf(d.key)]}
              </span>
              <span
                style={{
                  fontFamily: 'var(--font-display)',
                  fontWeight: 600,
                  fontSize: 17,
                  color: d.isToday ? 'var(--accent-700)' : 'var(--text)',
                }}
              >
                {keyDayNum(d.key)}
              </span>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 8 }}>
              {d.events.length === 0 && (
                <span style={{ fontSize: 12, color: 'var(--text-muted)', opacity: 0.7 }}>—</span>
              )}
              {d.events.map((e) => (
                <EventChip key={`${e.kind}-${e.id}`} event={e} onOpen={onOpen} />
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
    <div className="mesh" style={{ gridTemplateColumns: 'repeat(7,minmax(0,1fr))' }}>
      {WEEKDAYS.map((d) => (
        <div
          key={d}
          className="micro"
          style={{ background: 'var(--neutral-100)', padding: '6px 9px', letterSpacing: '.14em' }}
        >
          {d}
        </div>
      ))}
      {weeks.flatMap((row, wi) =>
        row.map((cell, ci) => (
          <div
            key={`${wi}-${ci}`}
            style={{
              minHeight: 104,
              padding: '7px 8px',
              display: 'flex',
              flexDirection: 'column',
              gap: 4,
              background: cell.inMonth ? 'var(--bg)' : 'var(--neutral-100)',
              opacity: cell.inMonth ? 1 : 0.6,
            }}
          >
            <span
              style={{
                fontFamily: 'var(--font-display)',
                fontWeight: 600,
                fontSize: 15,
                color: cell.isToday ? 'var(--accent-700)' : 'var(--text)',
              }}
            >
              {keyDayNum(cell.key)}
            </span>
            {cell.events.slice(0, 3).map((e) => (
              <EventChip key={`${e.kind}-${e.id}`} event={e} onOpen={onOpen} compact />
            ))}
            {cell.events.length > 3 && (
              <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>+{cell.events.length - 3}</span>
            )}
          </div>
        )),
      )}
    </div>
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
        <Card key={g.title} padding={0}>
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
