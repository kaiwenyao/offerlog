// 时间线面板：记录求职进度的主界面（迁移 00005 / 00006）。
//
// 用户往这里添加事件（投递 / 初筛 / OA / 面试 / Offer / 任意自定义）并选择发生
// 时间；面板按业务时间自动排序，岗位状态由后端推导成「时间线上最后一个事件」。
// 顶上的参考流程图（FlowGuide）只是建议路线，点节点即可快速记录。
//
// 这里同时只读地显示旧的状态事件（AppEvent）——「更新进度」时代留下的追加式
// 审计。两种来源合并排序，但只有用户事件可编辑、可移除。
import { useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtDateTime, relativeDayLabel } from '../../lib/api'
import type { AppEvent, Milestone } from '../../lib/types'
import { comboLabel, statusMeta } from '../../lib/status'
import { milestoneDefaultLabel, milestoneDotColor, milestoneIcon } from '../../lib/milestones'
import { Badge, Button } from '../../ds'
import { Icon } from '../../components/Icon'
import { FlowGuide } from '../../components/FlowGuide'
import { Dot, ErrorText, Num } from '../../components/ui'
import { MilestoneForm } from './forms'

/**
 * 旧的状态事件上带的变更类型标签（advance / rollback / reopen / correct）。
 * 只用于把「更新进度」时代留下的历史读顺——新的用户事件没有这个概念。
 */
function changeTypeLabel(t: string): string {
  switch (t) {
    case 'rollback':
      return '回退'
    case 'reopen':
      return '重开'
    case 'correct':
      return '更正'
    default:
      return '推进'
  }
}

/** 时间线统一条目：状态事件（审计）与用户节点（自由记录）合并排序。 */
type TimelineEntry =
  | { type: 'event'; e: AppEvent; pinned: boolean }
  | { type: 'milestone'; m: Milestone }

/**
 * 合并排序（migration 00005）：建档行始终钉在最前（与后端 ListEvents 同规则），
 * 其余按业务时间升序；时间未定的节点排最后（不伪造时间，也不乱插入中间）；
 * 同一时刻状态事件在前、节点在后，保证审计链读起来连贯。
 */
export function mergeTimeline(entries: TimelineEntry[]): TimelineEntry[] {
  const pinned = entries.filter((x) => x.type === 'event' && x.pinned)
  const rest = entries.filter((x) => !(x.type === 'event' && x.pinned))
  const key = (x: TimelineEntry) => {
    if (x.type === 'event') return x.e.occurred_at
    return x.m.occurred_at ?? ''
  }
  const rank = (x: TimelineEntry) => (x.type === 'event' ? 0 : 1)
  const idOf = (x: TimelineEntry) => (x.type === 'event' ? x.e.id : x.m.id)
  const cmp = (a: TimelineEntry, b: TimelineEntry): number => {
    const ka = key(a) === '' ? null : Date.parse(key(a))
    const kb = key(b) === '' ? null : Date.parse(key(b))
    if (ka == null && kb == null) {
      if (rank(a) !== rank(b)) return rank(a) - rank(b)
      return idOf(a) - idOf(b)
    }
    if (ka == null) return 1
    if (kb == null) return -1
    if (ka !== kb) return ka - kb
    if (rank(a) !== rank(b)) return rank(a) - rank(b)
    return idOf(a) - idOf(b)
  }
  return [...pinned, ...rest.sort(cmp)]
}

export function TimelineTab({
  appId,
  events,
  milestones = [],
  status,
  substatus = '',
}: {
  appId: number
  events: AppEvent[]
  /** 用户添加的事件（迁移 00005 / 00006）：岗位阶段就由它们推导。 */
  milestones?: Milestone[]
  status: string
  /** 当前子状态：页头标签要显示 OA / 面试轮次派生出的细化进度。 */
  substatus?: string
}) {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  // 系统信息（录入时间等数据库时间）默认隐藏，点击逐条展开。
  const [sysInfo, setSysInfo] = useState<Set<number>>(new Set())
  const toggleSys = (id: number) =>
    setSysInfo((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  // Events a later correction supersedes. Their own row keeps the wrong time
  // (the audit trail is append-only), so it has to READ as superseded —
  // otherwise a user who just fixed a date still sees the old one sitting there.
  const supersededBy = new Map<number, number>()
  for (const e of events) {
    if (e.event_type === 'correction' && e.corrects_event_id) supersededBy.set(e.corrects_event_id, e.id)
  }

  // 记录进度的唯一方式（迁移 00005 / 00006）：不是每个岗位都有 OA / 初筛 / 面试，
  // 发生了什么由用户自己添加；时间线按业务时间合并排序，阶段由后端据此推导。
  const [showMilestone, setShowMilestone] = useState<Milestone | 'new' | null>(null)
  // 从参考流程图点进来时预填的事件类型（'' = 从「＋ 添加事件」进来，不预填）。
  const [addKind, setAddKind] = useState('')
  const openAdd = (kind: string) => {
    setAddKind(kind)
    setShowMilestone('new')
  }
  // 阶段由时间线推导，所以每次增删改都要连岗位快照一起刷新——否则顶部的状态芯片
  // 会和刚刚改完的时间线对不上。
  const afterMilestoneChanged = () => {
    qc.invalidateQueries({ queryKey: ['milestones', appId] })
    qc.invalidateQueries({ queryKey: ['app', appId] })
    qc.invalidateQueries({ queryKey: ['apps'] })
  }
  const milestoneMut = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/applications/${appId}/milestones/${id}`),
    onSuccess: () => {
      setErr('')
      afterMilestoneChanged()
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '删除失败'),
  })
  const afterMilestoneSaved = () => {
    afterMilestoneChanged()
    setShowMilestone(null)
  }

  // 参考流程图上哪些节点已经记过了：两个来源都算——旧岗位的阶段来自状态事件，
  // 新记录的来自用户添加的节点。
  const reached = useMemo(() => {
    const set = new Set<string>()
    for (const e of events) if (e.to_status) set.add(e.to_status)
    for (const m of milestones) if (m.status_effect) set.add(m.status_effect)
    return set
  }, [events, milestones])

  // 合并排序：建档钉最前，其余按业务时间升序，时间未定的节点排最后。
  const merged = useMemo(
    () =>
      mergeTimeline([
        ...events.map((e) => ({ type: 'event' as const, e, pinned: e.event_type === 'created' })),
        ...milestones.map((m) => ({ type: 'milestone' as const, m })),
      ]),
    [events, milestones],
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      {err && <ErrorText>{err}</ErrorText>}

      <FlowGuide reached={reached} onPick={openAdd} />

      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)', flexGrow: 1 }}>
          当前状态：{comboLabel(status, substatus)}，取自时间线上最后一个事件。
          按你填写的发生时间排列；系统录入时间默认隐藏，展开「系统信息」可见。
        </p>
        <Button variant="primary" size="sm" onClick={() => openAdd('')}>
          ＋ 添加事件
        </Button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {merged.map((entry, i) => {
          const last = i === merged.length - 1
          if (entry.type === 'milestone') {
            const m = entry.m
            return (
              <div key={`m${m.id}`} style={{ display: 'flex', gap: 12 }}>
                <span className="timeline-rail">
                  <span style={{ marginTop: 5 }}>
                    <Dot color={milestoneDotColor(m.kind)} size={9} />
                  </span>
                  {!last && <span className="line" />}
                </span>
                <span className="grow" style={{ paddingBottom: 14, minWidth: 0 }}>
                  <span style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
                    <span style={{ fontSize: 14, fontWeight: 500 }}>
                      <span style={{ marginRight: 6 }}>{milestoneIcon(m.kind)}</span>
                      {m.label || milestoneDefaultLabel(m.kind)}
                    </span>
                    {m.status_effect ? (
                      <Badge tone="neutral">→ {statusMeta(m.status_effect).label}</Badge>
                    ) : (
                      <Badge tone="neutral">不改变状态</Badge>
                    )}
                    <span
                      style={{ marginLeft: 'auto', display: 'flex', alignItems: 'baseline', gap: 6 }}
                      title="实际发生时间（你选择的业务时间）"
                    >
                      {m.occurred_at ? (
                        <>
                          <Num color="var(--text-muted)">{fmtDateTime(m.occurred_at)}</Num>
                          {relativeDayLabel(m.occurred_at) && (
                            <span style={{ font: 'var(--type-caption)', fontWeight: 400, color: 'var(--text-muted)' }}>
                              · {relativeDayLabel(m.occurred_at)}
                            </span>
                          )}
                        </>
                      ) : (
                        <Num color="var(--text-muted)">时间未定</Num>
                      )}
                    </span>
                  </span>
                  {m.note && (
                    <span style={{ display: 'block', fontSize: 13, color: 'var(--text-muted)', marginTop: 3, lineHeight: 1.45 }}>
                      {m.note}
                    </span>
                  )}
                  <span style={{ display: 'flex', gap: 6, marginTop: 6 }}>
                    <Button variant="ghost" size="sm" onClick={() => setShowMilestone(m)}>
                      <Icon name="edit" size={13} /> 编辑
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={milestoneMut.isPending}
                      onClick={() => milestoneMut.mutate(m.id)}
                    >
                      <Icon name="trash" size={13} /> 移除
                    </Button>
                  </span>
                </span>
              </div>
            )
          }
          const e = entry.e
          return (
            <div key={e.id} style={{ display: 'flex', gap: 12 }}>
              <span className="timeline-rail">
                <span style={{ marginTop: 5 }}>
                  <Dot color={e.to_status ? statusMeta(e.to_status).dot : 'var(--neutral)'} size={9} />
                </span>
                {!last && <span className="line" />}
              </span>
            <span className="grow" style={{ paddingBottom: 14, minWidth: 0 }}>
              <span style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
                <span style={{ fontSize: 14, fontWeight: 500 }}>
                  {e.event_type === 'created' && '建档（开始追踪）'}
                  {e.event_type === 'correction' && '纠正'}
                  {e.event_type === 'status_change' &&
                    `${e.from_status ? comboLabel(e.from_status, e.from_substatus) : '—'} → ${
                      e.to_status ? comboLabel(e.to_status, e.to_substatus) : '—'
                    }`}
                  {!['created', 'correction', 'status_change'].includes(e.event_type) && e.event_type}
                </span>
                {/* 方案 §4.3：变更类型只做展示，帮助区分真实回退、重开与更正。 */}
                {e.event_type !== 'created' && e.change_type && e.change_type !== 'advance' && (
                  <Badge tone={e.change_type === 'correct' ? 'warning' : 'neutral'}>{changeTypeLabel(e.change_type)}</Badge>
                )}
                <span
                  style={{ marginLeft: 'auto', display: 'flex', alignItems: 'baseline', gap: 6 }}
                  title={supersededBy.has(e.id) ? '这条记录已被纠正，以纠正行为准' : '实际发生时间（你填写的业务时间）'}
                >
                  <span style={supersededBy.has(e.id) ? { textDecoration: 'line-through', opacity: 0.6 } : undefined}>
                    <Num color="var(--text-muted)">{fmtDateTime(e.occurred_at)}</Num>
                  </span>
                  {!supersededBy.has(e.id) && relativeDayLabel(e.occurred_at) && (
                    <span style={{ font: 'var(--type-caption)', fontWeight: 400, color: 'var(--text-muted)' }}>
                      · {relativeDayLabel(e.occurred_at)}
                    </span>
                  )}
                </span>
              </span>
              {e.note && (
                <span style={{ display: 'block', fontSize: 13, color: 'var(--text-muted)', marginTop: 3, lineHeight: 1.45 }}>
                  {e.note}
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
              {supersededBy.has(e.id) && (
                <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>
                  已被事件 #{supersededBy.get(e.id)} 纠正
                </span>
              )}
              {sysInfo.has(e.id) && (
                <span
                  style={{
                    display: 'block',
                    marginTop: 6,
                    padding: '6px 10px',
                    border: '1px solid var(--border-alt)',
                    background: 'var(--surface-thin)',
                    fontSize: 12,
                    color: 'var(--text-muted)',
                    lineHeight: 1.6,
                  }}
                >
                  <span style={{ display: 'block' }}>录入时间：{fmtDateTime(e.recorded_at)}（数据库写入，非业务时间）</span>
                  <span style={{ display: 'block' }}>
                    事件 #{e.id} · 序号 {e.sequence}
                  </span>
                </span>
              )}
              <span style={{ display: 'flex', gap: 6, marginTop: 6 }}>
                {/* 状态事件是追加式审计，不可编辑也不可删除——它们是这个岗位在
                    「更新进度」时代留下的历史。新的记录都是可编辑的用户事件，
                    两者在时间线上按同一个业务时间排序。 */}
                <Button variant="ghost" size="sm" onClick={() => toggleSys(e.id)} aria-expanded={sysInfo.has(e.id)}>
                  <Icon name="clock" size={13} /> 系统信息{sysInfo.has(e.id) ? '▴' : '▾'}
                </Button>
              </span>
            </span>
          </div>
          )
        })}
      </div>

      {showMilestone && (
        <MilestoneForm
          appId={appId}
          milestone={showMilestone === 'new' ? undefined : showMilestone}
          initialKind={showMilestone === 'new' && addKind ? addKind : undefined}
          onClose={() => setShowMilestone(null)}
          onDone={afterMilestoneSaved}
        />
      )}
    </div>
  )
}
