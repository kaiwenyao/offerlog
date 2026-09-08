import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import * as echarts from 'echarts'
import { api, fmtDate } from '../../lib/api'
import type { ChannelRow, Metrics, SankeyData } from '../../lib/types'
import { Button, Card, PanelTitle, Tabs } from '../../ds'
import { ErrorText, Num, PageSpinner, Spinner } from '../../components/ui'

type SankeyMode = 'current' | 'history'

/** Sankey palette: blue keeps flowing, grey drops out, green is an Offer.
    Values are design tokens; canvas rendering resolves them via resolve(). */
const FLOW_BLUE = 'var(--info)'
const DROPPED = 'var(--neutral-400)'
const GOOD = 'var(--positive)'

/** echarts paints to canvas, where var() does not resolve — read the token. */
function resolve(color: string): string {
  if (!color.startsWith('var(')) return color
  const v = getComputedStyle(document.documentElement).getPropertyValue(color.slice(4, -1)).trim()
  return v || color
}

const NODE_COLORS: Record<string, string> = {
  all: 'var(--accent)',
  submitted: FLOW_BLUE,
  not_submitted: DROPPED,
  saved: 'var(--neutral)',
  preparing: 'var(--text-muted)',
  applied: FLOW_BLUE,
  screening: FLOW_BLUE,
  assessment: 'var(--warning)',
  interviewing: 'var(--accent)',
  offer: GOOD,
  accepted: GOOD,
  rejected: DROPPED,
  withdrawn: DROPPED,
  closed: DROPPED,
}

function nodeColor(name: string): string {
  const suffix = name.includes('_') ? name.slice(name.indexOf('_') + 1) : name
  return NODE_COLORS[suffix] ?? DROPPED
}

function pct(x: number | null | undefined): string {
  if (x === null || x === undefined) return '—'
  return `${(x * 100).toFixed(1)}%`
}

/** 分母>0 且分子=0 时显示 0%（而非 —）；无样本由调用方按 denominator=0 显式处理。 */
function rateOrDash(rate: number | null | undefined, denominator: number): string {
  if (denominator === 0) return '—' // 暂无样本
  return pct(rate ?? 0)
}

export function AnalyticsPage() {
  const [mode, setMode] = useState<SankeyMode>('current')

  const summaryQ = useQuery({
    queryKey: ['analytics', 'summary'],
    // POST — the analytics routes accept POST (consistent with /sankey)
    queryFn: () => api.post<{ metrics: Metrics }>('/api/v1/analytics/summary', {}).then((r) => r.metrics),
  })
  const sankeyQ = useQuery({
    queryKey: ['analytics', 'sankey', mode],
    queryFn: () => api.post<SankeyData>('/api/v1/analytics/sankey', { mode }),
  })

  const metrics = summaryQ.data

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      {summaryQ.isError ? (
        <Card padding="18px">
          <ErrorText>指标加载失败，请稍后重试</ErrorText>
          <Button variant="secondary" size="sm" onClick={() => summaryQ.refetch()} style={{ marginTop: 10 }}>
            重试
          </Button>
        </Card>
      ) : summaryQ.isLoading ? (
        <PageSpinner />
      ) : metrics ? (
        <MetricGrid metrics={metrics} />
      ) : null}

      <SankeyPanel mode={mode} setMode={setMode} data={sankeyQ.data} loading={sankeyQ.isLoading} />

      <div className="split-grid">
        {metrics && <ChannelPanel rows={metrics.by_channel ?? []} />}
        {metrics && <OutcomePanel metrics={metrics} />}
      </div>
    </section>
  )
}

function MetricGrid({ metrics }: { metrics: Metrics }) {
  const cards: Array<{ label: string; value: string | number; def: string }> = [
    { label: '待投递', value: metrics.to_apply, def: 'saved + preparing' },
    { label: '进行中', value: metrics.in_progress, def: 'applied…interviewing' },
    { label: '有结果', value: metrics.with_result, def: '含主动撤回等管理结果' },
    { label: '已投递', value: metrics.submitted_count, def: '去重申请数' },
    { label: '有效回复率', value: rateOrDash(metrics.response_rate, metrics.denominator), def: `已回复 ${metrics.responded} / 样本 ${metrics.denominator}` },
    {
      label: '面试到达率',
      value: rateOrDash(metrics.interview_rate, metrics.denominator),
      def: metrics.denominator === 0 ? '暂无样本' : `到达面试 ${metrics.reached_interview} / 样本 ${metrics.denominator}`,
    },
    { label: 'Offer 率', value: rateOrDash(metrics.offer_rate, metrics.denominator), def: metrics.denominator === 0 ? '暂无样本' : `曾收 Offer ${metrics.received_offer} / 样本 ${metrics.denominator}` },
    {
      label: '回复中位耗时',
      value: metrics.replied_sample ? `${(metrics.response_median_hours ?? 0).toFixed(0)}h` : '—',
      def: `已回复 ${metrics.replied_sample}，未回复 ${metrics.pending_response}`,
    },
  ]
  return (
    <div className="metric-grid">
      {cards.map((c) => (
        <div key={c.label} style={{ padding: '15px 16px' }}>
          <div className="micro">{c.label}</div>
          <div
            style={{
              fontFamily: 'var(--font-display)',
              fontWeight: 600,
              fontSize: 36,
              lineHeight: 1.1,
              marginTop: 6,
            }}
          >
            {c.value}
          </div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4, lineHeight: 1.4 }}>{c.def}</div>
        </div>
      ))}
    </div>
  )
}

function SankeyPanel({
  mode,
  setMode,
  data,
  loading,
}: {
  mode: SankeyMode
  setMode: (m: SankeyMode) => void
  data?: SankeyData
  loading: boolean
}) {
  const box = useRef<HTMLDivElement>(null)
  const chart = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    const el = box.current
    if (!el || !data) return
    // (Re)bind: a previous instance may reference a node replaced while loading.
    if (chart.current && chart.current.getDom() !== el) {
      chart.current.dispose()
      chart.current = null
    }
    if (!chart.current) chart.current = echarts.init(el)
    chart.current.setOption(buildOption(data), true)
    chart.current.resize()
  }, [data, loading])

  useEffect(() => {
    const onResize = () => chart.current?.resize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useEffect(() => () => chart.current?.dispose(), [])

  return (
    <Card padding="18px">
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, flexWrap: 'wrap' }}>
        <span
          style={{
            fontFamily: 'var(--font-display)',
            fontWeight: 500,
            fontSize: 17,
            letterSpacing: 'var(--tracking-display)',
          }}
        >
          桑基图 · 投递去了哪里
        </span>
        <Tabs
          items={[
            { value: 'current', label: '当前进度' },
            { value: 'history', label: '历史路径' },
          ]}
          value={mode}
          onChange={(v) => setMode(v as SankeyMode)}
          size="sm"
          ariaLabel="桑基图口径"
        />
        <span
          style={{
            marginLeft: 'auto',
            display: 'flex',
            alignItems: 'center',
            gap: 14,
            fontSize: 12,
            color: 'var(--text-muted)',
          }}
        >
          <LegendSwatch color={FLOW_BLUE} label="继续推进" />
          <LegendSwatch color={DROPPED} label="流失" />
          <LegendSwatch color={GOOD} label="Offer" />
        </span>
      </div>

      <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.5 }}>
        {mode === 'current'
          ? '全部机会 → 是否已投递 → 当前状态。当前快照，不声称展示历史顺序或转化率。下钻数量与列表一致。'
          : '从每条申请的有效事件重建真实路径；节点 =（步骤, 状态）；终点 = 截至当前状态；超过 12 步折叠。导入记录显示“导入起点 → 已知当前状态”。'}
      </p>

      <div style={{ position: 'relative', marginTop: 14 }}>
        <div
          ref={box}
          style={{ width: '100%', height: 360 }}
          role="img"
          aria-label="桑基图，另有下方明细表可核对"
        />
        {loading && (
          <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' }}>
            <Spinner size={22} />
          </div>
        )}
      </div>

      {data && (
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          样本量 <Num color="var(--text)">{data.cohort_count}</Num> · 口径版本 {data.definition_version} · as_of{' '}
          {fmtDate(data.as_of)}
        </div>
      )}

      <SankeyTable data={data} />
      {data?.drilldown_token && <Drilldown token={data.drilldown_token} />}
    </Card>
  )
}

function LegendSwatch({ color, label }: { color: string; label: string }) {
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
      <span aria-hidden style={{ width: 9, height: 9, background: color }} />
      {label}
    </span>
  )
}

function buildOption(d: SankeyData): echarts.EChartsOption {
  return {
    tooltip: { trigger: 'item', triggerOn: 'mousemove' },
    textStyle: { fontFamily: 'Inter, "Noto Sans SC", system-ui, sans-serif' },
    series: [
      {
        type: 'sankey',
        data: d.nodes.map((n) => ({
          name: n.name,
          itemStyle: { color: resolve(nodeColor(n.name)), borderWidth: 0, borderRadius: 0 },
        })),
        links: d.links.map((l) => ({ source: l.source, target: l.target, value: l.value })),
        emphasis: { focus: 'adjacency' },
        lineStyle: { color: 'gradient', curveness: 0.5, opacity: 0.3 },
        label: {
          formatter: (p: { name: string }) => d.nodes.find((n) => n.name === p.name)?.label ?? p.name,
          fontSize: 12,
          color: resolve('var(--text)'),
          fontWeight: 500,
        },
        nodeAlign: 'justify',
        nodeWidth: 13,
        nodeGap: 12,
      },
    ],
  }
}

function SankeyTable({ data }: { data?: SankeyData }) {
  if (!data) return null
  const label = (name: string) => data.nodes.find((n) => n.name === name)?.label ?? name
  const rows = data.links.filter((l) => l.value > 0)
  return (
    <details style={{ marginTop: 14 }}>
      <summary style={{ cursor: 'pointer', fontSize: 13, color: 'var(--text-muted)' }}>
        查看明细表（供数量核对）
      </summary>
      <div style={{ overflowX: 'auto', marginTop: 8 }}>
        <table className="tbl">
          <thead>
            <tr>
              <th>从</th>
              <th>到</th>
              <th style={{ width: 90 }}>数量</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <tr key={i}>
                <td>{label(r.source)}</td>
                <td>{label(r.target)}</td>
                <td>
                  <Num>{r.value}</Num>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  )
}

function ChannelPanel({ rows }: { rows: ChannelRow[] }) {
  if (rows.length === 0) return null
  const max = Math.max(...rows.map((r) => r.response_rate ?? 0), 0.0001)
  return (
    <Card padding="16px">
      <PanelTitle style={{ marginBottom: 12 }}>各渠道表现</PanelTitle>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 56px 1fr', gap: '10px 12px', alignItems: 'center' }}>
        <span style={{ fontSize: 11, letterSpacing: '.08em', color: 'var(--text-muted)' }}>渠道</span>
        <span style={{ fontSize: 11, letterSpacing: '.08em', color: 'var(--text-muted)', textAlign: 'right' }}>
          投递
        </span>
        <span style={{ fontSize: 11, letterSpacing: '.08em', color: 'var(--text-muted)' }}>回复率</span>
        {rows.map((c) => (
          <Row key={c.channel} channel={c} max={max} />
        ))}
      </div>
      <p style={{ marginTop: 12, marginBottom: 0, fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 }}>
        “有结果”包含主动撤回等管理结果，不等同于招聘方回复。分母为零（该渠道尚无投递）显示 — 表示暂无样本。
      </p>
    </Card>
  )
}

function Row({ channel, max }: { channel: ChannelRow; max: number }) {
  const rate = channel.response_rate
  const color = rate != null && rate >= max * 0.9 ? GOOD : rate != null && rate > 0 ? FLOW_BLUE : DROPPED
  return (
    <>
      <span style={{ fontSize: 13 }}>{channel.channel || '—'}</span>
      <span style={{ textAlign: 'right' }}>
        <Num>{channel.submitted}</Num>
      </span>
      <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span className="meter">
          <span style={{ width: `${Math.min(100, (rate ?? 0) * 100)}%`, background: color }} />
        </span>
        <span style={{ width: 44, textAlign: 'right' }}>
          <Num color="var(--text-muted)">
            {channel.submitted === 0 ? '—' : rateOrDash(rate, channel.submitted)}
          </Num>
        </span>
      </span>
    </>
  )
}

function OutcomePanel({ metrics }: { metrics: Metrics }) {
  const by = metrics.by_status ?? {}
  const accepted = by.accepted ?? 0
  // 分别展示被拒 / 主动撤回 / 岗位关闭，不把招聘方拒绝归入「婉拒」。
  const rejected = by.rejected ?? 0
  const withdrawn = by.withdrawn ?? 0
  const closed = by.closed ?? 0
  const pending = by.offer ?? 0
  const total = accepted + rejected + withdrawn + closed + pending

  const split = [
    { label: '已接受', value: accepted, color: GOOD },
    { label: '被拒绝', value: rejected, color: 'var(--danger)' },
    { label: '主动撤回', value: withdrawn, color: 'var(--neutral)' },
    { label: '岗位关闭', value: closed, color: DROPPED },
    { label: '待答复', value: pending, color: 'var(--warning)' },
  ].filter((s) => s.value > 0)

  return (
    <Card padding="16px">
      <PanelTitle style={{ marginBottom: 4 }}>结果去向</PanelTitle>
      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 14 }}>
        {total} 条已有结果或待答复的记录
      </div>
      <div
        style={{
          display: 'flex',
          height: 22,
          overflow: 'hidden',
          background: 'var(--neutral-200)',
        }}
      >
        {total > 0 &&
          split.map((s) => (
            <span key={s.label} style={{ width: `${(s.value / total) * 100}%`, background: s.color }} />
          ))}
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 14 }}>
        {split.map((s) => (
          <div key={s.label} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
            <span aria-hidden style={{ width: 8, height: 8, background: s.color }} />
            <span style={{ flex: 1 }}>{s.label}</span>
            <Num>{s.value}</Num>
          </div>
        ))}
      </div>
      {metrics.small_sample && (
        <div
          style={{
            marginTop: 14,
            paddingTop: 12,
            borderTop: '1px solid var(--border-alt)',
            fontSize: 12,
            color: 'var(--text-muted)',
            lineHeight: 1.5,
          }}
        >
          样本低于 20 的口径提示：比例仅供参考。
        </div>
      )}
    </Card>
  )
}

/**
 * Lists the snapshot members bound to the chart token so figures can be
 * audited against real record ids (§5.4 reproducibility).
 */
function Drilldown({ token }: { token: string }) {
  const [open, setOpen] = useState(false)
  const q = useQuery({
    queryKey: ['analytics', 'drilldown', token],
    enabled: open,
    queryFn: () =>
      api.get<{ member_ids: number[]; expires_at: string }>(
        `/api/v1/analytics/drilldowns/${encodeURIComponent(token)}`,
      ),
    staleTime: 1000 * 60 * 8,
  })

  if (!open) {
    return (
      <div style={{ marginTop: 8 }}>
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
          下钻成员（与图表同时刻的记录 ID）
        </Button>
      </div>
    )
  }
  if (q.isLoading) return <div style={{ marginTop: 8 }}><Spinner size={14} /></div>
  if (q.isError) {
    return (
      <p role="alert" style={{ marginTop: 8, fontSize: 13, color: 'var(--danger)' }}>
        下钻令牌已过期，请刷新图表后重试
      </p>
    )
  }
  const ids = q.data?.member_ids ?? []
  return (
    <p style={{ marginTop: 8, fontSize: 12, color: 'var(--text-muted)', wordBreak: 'break-all' }}>
      快照内 {ids.length} 条记录 ID：<Num color="var(--text-muted)">{ids.join(', ')}</Num>
    </p>
  )
}

export default AnalyticsPage
