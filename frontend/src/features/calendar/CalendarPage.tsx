import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, dayToInstant, fmtDateTime } from '../../lib/api'
import { queryListState } from '../../lib/queryState'
import type { CalendarEvent } from '../../lib/types'
import { effectiveZone } from '../../lib/tz'
import { Button, Card, PanelTitle, Tabs } from '../../ds'
import { Dot, EmptyHint, ErrorText, Modal, Num, PageSpinner } from '../../components/ui'
import {
  addDaysToKey,
  agenda,
  agendaWindowKeys,
  dayKeyInZone,
  monthGrid,
  monthKeyOf,
  splitMonthCell,
  stepLabels,
  todayKeyInZone,
  weekColumns,
  weekdayLabels,
  WEEKDAY_NAMES,
  weekdayOf,
  weekStartKeyOf,
  type ViewMode,
} from './grid'
import { useWeekStart } from '../../lib/weekStart'

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
/** 某一天的中文星期名（与每周起始日无关，它就是那天本身）。 */
function weekdayNameOf(key: string): string {
  return WEEKDAY_NAMES[weekdayOf(key)]
}

export function CalendarPage() {
  const nav = useNavigate()
  const zone = effectiveZone()
  // 每周起始日跟着账号偏好走（0 = 周日 … 6 = 周六）：首页的「本周工序」一直是
  // 这么排的，日历以前写死周一，改成周日之后两边的「本周」差一天。
  const weekStart = useWeekStart()
  const [view, setView] = useState<ViewMode>('week')
  // Anchor is a day KEY in the active zone. Week view navigates by whole weeks
  // anchored on the user's week-start day; month view by month keys; agenda by
  // the current week.
  const [weekKey, setWeekKey] = useState(() => weekStartKeyOf(todayKeyInZone(zone), weekStart))
  const [monthKey, setMonthKey] = useState(() => monthKeyOf(todayKeyInZone(zone)))
  // 月视图折叠的日程：点「+N 展开」把它单独摊开，不用切视图再重新定位到那天。
  const [expandedDay, setExpandedDay] = useState<{ key: string; events: CalendarEvent[] } | null>(null)

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
      const gridStart = weekStartKeyOf(monthKey, weekStart)
      return { from: instantOf(gridStart), to: instantOf(addDaysToKey(gridStart, 42)) }
    }
    // agenda: current Monday week −90d … +30d, computed as pure day keys in
    // the user zone and converted to user-local-midnight instants — the window
    // must never be derived from the browser zone (that drifted the `to` edge
    // and silently cut +29/+30-day events).
    const anchor = weekStartKeyOf(todayKeyInZone(zone), weekStart)
    const { fromKey, toKey } = agendaWindowKeys(anchor)
    return { from: instantOf(fromKey), to: instantOf(toKey) }
  }, [view, weekKey, monthKey, zone, weekStart])

  // 偏好是异步到的（首屏先用默认的周一），拿到之后把锚点挪到正确的起始日，否则
  // 首屏那一周会停在周一开头、和表头标签对不上。
  //
  // 重新锚定到「今天所在的那一周」，而不是拿旧锚点原地换算：周一起始的本周与
  // 周日起始的本周本来就是两个不同的区间（周日那天分属两边），拿旧锚点换算会把
  // 人送到上一周去——首屏尤其明显，一进日历就看不到今天。
  useEffect(() => {
    setWeekKey(weekStartKeyOf(todayKeyInZone(zone), weekStart))
  }, [weekStart, zone])

  const q = useQuery({
    queryKey: ['calendar', view, from.toISOString(), to.toISOString()],
    queryFn: () =>
      api.get<{ items: CalendarEvent[] }>(
        `/api/v1/calendar?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`,
      ),
    staleTime: 30_000,
  })

  const events = q.data?.items ?? []
  const calState = queryListState(q)

  const weeks = useMemo(
    () => (view === 'month' ? monthGrid(events, monthKey, zone, weekStart) : []),
    [view, events, monthKey, zone, weekStart],
  )
  const days = useMemo(() => (view === 'week' ? weekColumns(events, weekKey, zone) : []), [view, events, weekKey, zone])
  const agendaGroups = useMemo(() => (view === 'agenda' ? agenda(events, new Date(), zone) : []), [view, events, zone])

  const navTitle =
    view === 'week'
      ? `${fmtMonthKey(weekKey)} ${weekKey.slice(8, 10)} 日起`
      : view === 'month'
        ? fmtMonthKey(monthKey)
        : '议程 · 近 90 天 → 未来 30 天'
  const steps = stepLabels(view)

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
    setWeekKey(weekStartKeyOf(today, weekStart))
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
        {/* 议程视图的窗口固定锚在本周，没有上下页可翻——不画按钮，好过画三个
            点了没反应的。「今天」同理：议程本来就从今天往两边展开。 */}
        {steps && (
          <span style={{ display: 'flex', gap: 6 }}>
            <Button variant="ghost" size="sm" onClick={goPrev}>
              {steps.prev}
            </Button>
            <Button variant="ghost" size="sm" onClick={goToday}>
              今天
            </Button>
            <Button variant="ghost" size="sm" onClick={goNext}>
              {steps.next}
            </Button>
          </span>
        )}
        <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)' }}>
          已汇总面试 / 待办 / 截止 · 半开区间
        </span>
      </div>

      {calState === 'loading' ? (
        <PageSpinner />
      ) : calState === 'error' ? (
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
        <MonthView weeks={weeks} weekStart={weekStart} onOpen={(id) => nav(`/apps/${id}`)} onShowDay={setExpandedDay} />
      ) : (
        <AgendaView groups={agendaGroups} onOpen={(id) => nav(`/apps/${id}`)} />
      )}

      {expandedDay && (
        <DayEventsModal
          dayKey={expandedDay.key}
          events={expandedDay.events}
          zone={zone}
          onOpen={(id) => {
            setExpandedDay(null)
            nav(`/apps/${id}`)
          }}
          onClose={() => setExpandedDay(null)}
        />
      )}
    </section>
  )
}

/**
 * 月视图「+N」的展开面板：列出这一天被折叠掉的全部日程。
 * 每行与网格里的日程条同构（公司/岗位 · 类型 · 时间），点开就进岗位详情。
 */
function DayEventsModal({
  dayKey,
  events,
  zone,
  onOpen,
  onClose,
}: {
  dayKey: string
  events: CalendarEvent[]
  zone?: string
  onOpen: (id: number) => void
  onClose: () => void
}) {
  const { hidden } = splitMonthCell(events)
  const weekday = weekdayNameOf(dayKey)
  return (
    <Modal
      title={`${dayKey.slice(0, 4)}-${dayKey.slice(5, 7)}-${dayKey.slice(8, 10)} ${weekday} · ${events.length} 项日程`}
      onClose={onClose}
      width={520}
    >
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {events.map((e) => (
          <button
            key={`${e.kind}-${e.id}`}
            className="panel-row"
            onClick={() => onOpen(e.application_id)}
            title="打开岗位详情"
          >
            <Dot color={toneOf(e)} />
            <span className="grow" style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
              <span style={{ fontSize: 14, fontWeight: 500 }}>
                {e.company_name || e.title}
                {e.position ? (
                  <span style={{ color: 'var(--text-muted)', fontSize: 12, fontWeight: 400 }}> · {e.position}</span>
                ) : null}
              </span>
              <span className="ellipsis" style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                {kindLabel(e)} · {evTime(e)}
                {e.location ? ` · ${e.location}` : ''}
              </span>
            </span>
          </button>
        ))}
      </div>
      {hidden.length === 0 && (
        <p style={{ margin: '10px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
          这一天没有折叠的日程（网格里已全部显示）。
        </p>
      )}
    </Modal>
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
                {weekdayNameOf(d.key)}
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

function MonthView({
  weeks,
  weekStart,
  onOpen,
  onShowDay,
}: {
  weeks: ReturnType<typeof monthGrid>
  /** 表头的 7 个星期名要跟网格同一个起始日，否则整排标签错位。 */
  weekStart: number
  onOpen: (id: number) => void
  /** 展平折叠的日程：月视图同日超过 3 项时「+N」可点，弹出当日全部日程。 */
  onShowDay: (cell: { key: string; events: CalendarEvent[] }) => void
}) {
  return (
    <div className="mesh" style={{ gridTemplateColumns: 'repeat(7,minmax(0,1fr))' }}>
      {weekdayLabels(weekStart).map((d) => (
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
            {splitMonthCell(cell.events).shown.map((e) => (
              <EventChip key={`${e.kind}-${e.id}`} event={e} onOpen={onOpen} compact />
            ))}
            {splitMonthCell(cell.events).hidden.length > 0 && (
              <button
                type="button"
                onClick={() => onShowDay({ key: cell.key, events: cell.events })}
                title={`展开这一天的全部 ${cell.events.length} 项日程`}
                style={{
                  all: 'unset',
                  cursor: 'pointer',
                  fontSize: 10,
                  color: 'var(--accent-700)',
                  padding: '1px 2px',
                  alignSelf: 'flex-start',
                }}
              >
                +{splitMonthCell(cell.events).hidden.length} 展开
              </button>
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
