import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import * as echarts from 'echarts'
import { api, fmtDate } from '../../lib/api'
import type { ChannelRow, Metrics, SankeyData } from '../../lib/types'
import { statusMeta } from '../../lib/status'
import { Spinner } from '../../components/ui'

function pct(x: number): string {
  if (x === 0) return '—'
  return (x * 100).toFixed(1) + '%'
}

export function AnalyticsPage() {
  const [mode, setMode] = useState<'current' | 'history'>('current')
  const summaryQ = useQuery({
    queryKey: ['analytics', 'summary'],
    // POST — the analytics routes accept POST (consistent with /sankey)
    queryFn: () => api.post<{ metrics: Metrics }>('/api/v1/analytics/summary', {}).then((r) => ({ metrics: r.metrics })),
  })
  const sankeyQ = useQuery({
    queryKey: ['analytics', 'sankey', mode],
    queryFn: () => api.post<SankeyData>('/api/v1/analytics/sankey', { mode }),
  })
  void summaryQ
  void sankeyQ

  return (
    <div>
      <div className="page-head">
        <div>
          <div className="eyebrow">Insights</div>
          <h1 className="display">统计分析</h1>
        </div>
      </div>
      <Summary metrics={summaryQ.data?.metrics} />
      <SankeyPanel mode={mode} setMode={setMode} data={sankeyQ.data} loading={sankeyQ.isLoading} />
      <Channels />
    </div>
  )
}

function Summary({ metrics }: { metrics?: Metrics }) {
  if (!metrics) return <div className="mt16"><Spinner /></div>
  const cards = [
    { label: '待投递', value: metrics.to_apply, note: 'saved + preparing' },
    { label: '进行中', value: metrics.in_progress, note: 'applied…interviewing' },
    { label: '有结果', value: metrics.with_result, note: '含主动撤回等管理结果' },
    { label: '有效回复率', value: pct(metrics.response_rate), note: `样本 ${metrics.denominator}` },
    { label: '面试到达率', value: pct(metrics.interview_rate), note: metrics.small_sample ? '样本偏少(<10)' : '投递 cohort' },
    { label: 'Offer 率', value: pct(metrics.offer_rate), note: '曾收到 Offer / 投递数' },
    { label: '首次回复中位耗时', value: metrics.replied_sample ? `${(metrics.response_median_hours ?? 0).toFixed(0)}h` : '—', note: `已回复 ${metrics.replied_sample}，未回复 ${metrics.pending_response}` },
    { label: '已投递', value: metrics.submitted_count, note: '去重申请数' },
  ]
  return (
    <div className="stat-cards mt16">
      {cards.map((c) => (
        <div key={c.label} className="card stat-card">
          <div className="sc-label">{c.label}</div>
          <div className="sc-value num">{c.value}</div>
          <div className="sc-note">{c.note}</div>
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
  mode: 'current' | 'history'
  setMode: (m: 'current' | 'history') => void
  data?: SankeyData
  loading: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)
  const [active, setActive] = useState<SankeyData | null>(null)
  const display = active ?? data

  useEffect(() => {
    if (!ref.current) return
    const el = ref.current
    // (Re)bind to the current DOM node; a previous instance may reference a
    // node that was replaced while loading.
    if (chartRef.current && chartRef.current.getDom() !== el) {
      chartRef.current.dispose()
      chartRef.current = null
    }
    if (!chartRef.current) {
      chartRef.current = echarts.init(el)
    }
    return () => {
      // keep alive across loading swaps; real unmount disposes below
    }
  }, [loading])

  useEffect(() => {
    if (!ref.current || !display) return
    if (!chartRef.current) {
      chartRef.current = echarts.init(ref.current)
    }
    const opt = buildOption(display)
    chartRef.current.setOption(opt, true)
    chartRef.current.resize()
  }, [display, loading])
  useEffect(() => () => chartRef.current?.dispose(), [])
  void setActive

  return (
    <div className="card mt16" style={{ padding: 16 }}>
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <h2 className="panel-title">桑基图</h2>
        <div className="row">
          <button className={'btn btn-ghost btn-small ' + (mode === 'current' ? 'active-layout' : '')} onClick={() => setMode('current')}>
            当前进度
          </button>
          <button className={'btn btn-ghost btn-small ' + (mode === 'history' ? 'active-layout' : '')} onClick={() => setMode('history')}>
            历史路径
          </button>
        </div>
      </div>
      {mode === 'current' ? (
        <p className="small muted mt8">
          全部机会 → 是否已投递 → 当前状态。当前快照，不声称展示历史顺序或转化率。下钻数量与列表一致。
        </p>
      ) : (
        <p className="small muted mt8">
          从每条申请的有效事件重建真实路径；节点 = (步骤, 状态)；终点 = 截至当前状态；超过 12 步折叠。导入记录显示“导入起点 → 已知当前状态”。
        </p>
      )}
      <div style={{ position: 'relative' }}>
        <div ref={ref} className="chart-box sankey" role="img" aria-label="桑基图，另有下方数据表可核对" />
        {loading && (
          <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center', background: 'rgb(255 255 255 / 0.6)' }}>
            <Spinner />
          </div>
        )}
      </div>
      {data && (
        <div className="small muted">
          样本量 {data.cohort_count} · 口径版本 {data.definition_version} · as_of {fmtDate(data.as_of)}
        </div>
      )}
      <SankeyTable data={display} />
      {data?.drilldown_token && <Drilldown token={data.drilldown_token} />}
    </div>
  )
}

// Node colors follow the app palette: ink for the cohort root, pine for the
// submitted branch, and the status chip colors for terminal states.
const SANKEY_COLORS: Record<string, string> = {
  all: '#1c2a33',
  submitted: '#0d6b5a',
  not_submitted: '#9aa8ac',
  saved: '#52626a',
  preparing: '#31519e',
  applied: '#0d6b5a',
  screening: '#0d6b5a',
  assessment: '#0d6b5a',
  interviewing: '#31519e',
  offer: '#8a6414',
  accepted: '#0d6b5a',
  rejected: '#c2452d',
  withdrawn: '#6d4f93',
  closed: '#64747a',
}

function sankeyColor(name: string): string {
  const suffix = name.includes('_') ? name.slice(name.indexOf('_') + 1) : name
  return SANKEY_COLORS[suffix] ?? '#9aa8ac'
}

function buildOption(d: SankeyData) {
  return {
    tooltip: { trigger: 'item', triggerOn: 'mousemove' },
    series: [
      {
        type: 'sankey',
        data: d.nodes.map((n) => ({
          name: n.name,
          label: { formatter: () => n.label ?? n.name },
          itemStyle: { color: sankeyColor(n.name), borderRadius: 2 },
        })),
        links: d.links.map((l) => ({ source: l.source, target: l.target, value: l.value })),
        emphasis: { focus: 'adjacency' },
        lineStyle: { color: 'gradient', curveness: 0.5, opacity: 0.35 },
        label: {
          formatter: (p: { name: string }) => {
            const nd = d.nodes.find((n) => n.name === p.name)
            return nd?.label ?? p.name
          },
          fontSize: 11,
          color: '#46565e',
        },
        nodeAlign: 'justify',
        nodeWidth: 12,
        nodeGap: 10,
      },
    ],
    textStyle: { fontFamily: 'system-ui' },
  }
}

function SankeyTable({ data }: { data?: SankeyData }) {
  if (!data) return null
  const rows = data.links
    .filter((l) => l.value > 0)
    .map((l) => ({
      ...l,
      from: data.nodes.find((n) => n.name === l.source)?.label ?? l.source,
      to: data.nodes.find((n) => n.name === l.target)?.label ?? l.target,
    }))
  return (
    <details className="mt16">
      <summary className="small muted" style={{ cursor: 'pointer' }}>查看明细表（供数量核对）</summary>
      <table className="tbl mt8">
        <thead>
          <tr><th>从</th><th>到</th><th>数量</th></tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              <td>{r.from}</td>
              <td>{r.to}</td>
              <td className="num">{r.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  )
}

function Channels() {
  const q = useQuery({
    queryKey: ['analytics', 'channels'],
    queryFn: () => api.post<{ metrics: Metrics }>('/api/v1/analytics/summary', {}).then((r) => r.metrics),
  })
  const rows: ChannelRow[] = q.data?.by_channel ?? []
  if (rows.length === 0) return null
  return (
    <div className="card mt16" style={{ padding: 16 }}>
      <h2 className="panel-title mb8">渠道效果</h2>
      <div className="tbl-wrap">
        <table className="tbl">
          <thead>
            <tr>
              <th>渠道</th>
              <th>投递量</th>
              <th>有效回复率</th>
              <th>面试到达率</th>
              <th>Offer 率</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.channel}>
                <td><b>{r.channel}</b></td>
                <td className="num">{r.submitted}</td>
                <td className="num">{pct(r.response_rate)}</td>
                <td className="num">{pct(r.interview_rate)}</td>
                <td className="num">{pct(r.offer_rate)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="small muted mt8">“有结果”包含主动撤回等管理结果，不等同于招聘方回复。分母为零显示 —。</p>
    </div>
  )
}

// keep TS happy with unused helpers used by future drilldown
export const _internal = { statusMeta }

export default AnalyticsPage

// Drilldown lists the snapshot members bound to the chart token so figures can
// be audited against the real record ids (§5.4 reproducibility).
function Drilldown({ token }: { token: string }) {
  const q = useQuery({
    queryKey: ['analytics', 'drilldown', token],
    queryFn: () => api.get<{ member_ids: number[]; expires_at: string }>(`/api/v1/analytics/drilldowns/${encodeURIComponent(token)}`),
    staleTime: 1000 * 60 * 8,
  })
  if (q.isLoading) return <div className="small muted mt8"><Spinner /></div>
  if (q.isError) return <p className="small err mt8">下钻令牌已过期，请刷新图表后重试</p>
  const ids = q.data?.member_ids ?? []
  return (
    <details className="mt8">
      <summary className="small muted" style={{ cursor: 'pointer' }}>
        下钻成员（快照内 {ids.length} 条记录 ID，与图表同时刻）
      </summary>
      <p className="small muted num" style={{ wordBreak: 'break-all' }}>
        {ids.join(', ')}
      </p>
    </details>
  )
}
