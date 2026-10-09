// 投递热力图（docs/投递热力图方案.md §4.1）——类似 GitHub 贡献图：横向按周、
// 纵向一周 7 天，每格一天，颜色深浅表示当天投递了几个岗位。
//
// 放在分析页的指标卡和桑基图之间：先看总量，再看到「哪几天真的动手了」。
// 图表用项目里已有的 echarts（calendar 坐标系 + heatmap 系列），不新增依赖。
import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import * as echarts from 'echarts'
import { api } from '../../lib/api'
import { resolve } from '../../lib/tokens'
import { useWeekStart } from '../../lib/weekStart'
import type { HeatmapData } from '../../lib/types'
import { Button, Card, Select } from '../../ds'
import { ErrorText, Num, Spinner } from '../../components/ui'
import { HEATMAP_PIECES, fillDays, formatDayZh, heatmapStats } from './heatmap'

/** 「最近一年」= 后端缺省窗口（今天往前 364 天，正好 53 列）。 */
const RECENT = 'recent'

const CELL_H = 14
/** 一屏最多 53 个周列（365/7），窄屏就靠这个宽度出横向滚动。 */
const MIN_CANVAS_W = 53 * CELL_H + 84
const CANVAS_H = 210

/** 5 档固定色阶，全部走 token：将来加暗色主题时图表自动跟随。 */
const BUCKET_COLORS = [
  'var(--surface-hover)', // 0 · 空格
  'var(--accent-200)',
  'var(--accent-400)',
  'var(--accent-600)',
  'var(--accent-800)', // 7+
]

export function HeatmapPanel() {
  // 与设置页「每周起始日」一致：dayLabel.firstDay 用同一份偏好，热力图的
  // 列才不会和日历页/首页的「本周」互相错位。
  const weekStart = useWeekStart()
  const [range, setRange] = useState<string>(RECENT)

  const q = useQuery({
    queryKey: ['analytics', 'heatmap', range],
    queryFn: () =>
      api.get<HeatmapData>(
        range === RECENT
          ? '/api/v1/analytics/heatmap'
          : `/api/v1/analytics/heatmap?from=${range}-01-01&to=${range}-12-31`,
      ),
  })
  const data = q.data

  const filled = useMemo(() => (data ? fillDays(data.days, data.from, data.to) : []), [data])
  const stats = useMemo(() => (data ? heatmapStats(data.days, data.today) : null), [data])

  const box = useRef<HTMLDivElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const chart = useRef<echarts.ECharts | null>(null)
  // 记住「刚才是不是贴在右边缘」：视口变窄之后 scrollLeft 已经是旧值，
  // 事后再算就再也算不出当时的位置了。
  const wasAtRight = useRef(true)

  useEffect(() => {
    const el = box.current
    if (!el || !data) return
    // 整段时间一条投递都没有时不画一整块空网格（§4.1）：空画布看着像加载失败。
    if (data.total === 0) {
      chart.current?.dispose()
      chart.current = null
      return
    }
    if (chart.current && chart.current.getDom() !== el) {
      chart.current.dispose()
      chart.current = null
    }
    if (!chart.current) chart.current = echarts.init(el)
    chart.current.setOption(buildOption(data, filled, weekStart), true)
    chart.current.resize()
  }, [data, filled, weekStart])

  // 窄屏：滚到最右边，让「今天」直接可见（§4.1）。首次加载、切换年份都强制贴右；
  // 视口变化（手机旋转、开发者工具改宽）只在本来就贴右时才重新贴住，用户手动
  // 滚到左边看历史时不抢他的位置。
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    const record = () => {
      wasAtRight.current = el.scrollLeft + el.clientWidth >= el.scrollWidth - 2
    }
    const pin = (force: boolean) => {
      if (force || wasAtRight.current) {
        el.scrollLeft = el.scrollWidth
        wasAtRight.current = true
      }
    }
    pin(true)
    el.addEventListener('scroll', record, { passive: true })
    const ro = new ResizeObserver(() => pin(false))
    ro.observe(el)
    return () => {
      el.removeEventListener('scroll', record)
      ro.disconnect()
    }
  }, [data, filled])

  useEffect(() => {
    const onResize = () => chart.current?.resize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useEffect(() => () => chart.current?.dispose(), [])

  const years = data?.years ?? []
  const scopeLabel = range === RECENT ? '最近一年' : `${range} 年`

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
          投递热力图
        </span>
        {/* 年份切换：只列出真有投递记录的年份，点开空年份看不到东西。 */}
        <span style={{ marginLeft: 'auto' }}>
          <Select
            size="sm"
            fullWidth={false}
            value={range}
            onChange={(e) => setRange(e.target.value)}
            aria-label="热力图时间范围"
            style={{ width: 132 }}
            options={[
              { value: RECENT, label: '最近一年' },
              ...years.map((y) => ({ value: String(y), label: `${y} 年` })),
            ]}
          />
        </span>
      </div>

      {stats && data && data.total > 0 && (
        <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.5 }}>
          {scopeLabel}投递 <Num color="var(--text)">{stats.total}</Num> 个 · 活跃{' '}
          <Num color="var(--text)">{stats.activeDays}</Num> 天 · 单日最多{' '}
          <Num color="var(--text)">{stats.maxDay}</Num> · 当前连续{' '}
          <Num color="var(--text)">{stats.currentStreak}</Num> 天
        </p>
      )}

      <div style={{ position: 'relative', marginTop: 12 }}>
        {/* 图表位置始终占着：loading / 失败遮罩是 absolute + inset:0，画布不
            存在时容器会塌成 0 高，报错文案和重试按钮就浮到下一个面板上面——
            看着在，点不到。只有「确实一条投递都没有」才换成空态块。 */}
        {(!data || data.total > 0) && (
          <div ref={scroller} style={{ overflowX: 'auto' }}>
            <div style={{ minWidth: MIN_CANVAS_W }}>
              <div
                ref={box}
                style={{ width: '100%', height: CANVAS_H }}
                role="img"
                aria-label={
                  stats
                    ? `${scopeLabel}投递热力图：投递 ${stats.total} 个，活跃 ${stats.activeDays} 天，单日最多 ${stats.maxDay} 个，当前连续 ${stats.currentStreak} 天`
                    : '投递热力图'
                }
              />
            </div>
          </div>
        )}

        {data && data.total === 0 && !q.isLoading && (
          <div
            style={{
              border: '1px dashed var(--border)',
              padding: 'var(--space-6)',
              textAlign: 'center',
              color: 'var(--text-muted)',
              fontSize: 13,
            }}
          >
            {scopeLabel}还没有投递记录
          </div>
        )}

        {q.isLoading && (
          <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' }}>
            <Spinner size={22} />
          </div>
        )}

        {/* 图挂掉时必须说出来，不能留一块空白（沿用桑基图的写法）。 */}
        {!q.isLoading && q.isError && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              display: 'grid',
              placeItems: 'center',
              background: 'var(--bg)',
            }}
          >
            <div style={{ textAlign: 'center' }}>
              <ErrorText>热力图加载失败</ErrorText>
              <Button variant="secondary" size="sm" onClick={() => q.refetch()} style={{ marginTop: 10 }}>
                重试
              </Button>
            </div>
          </div>
        )}
      </div>

      {data && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            flexWrap: 'wrap',
            gap: 8,
            marginTop: 10,
            fontSize: 11,
            color: 'var(--text-muted)',
          }}
        >
          {/* 色阶图例画在 HTML 里，不交给 echarts：窄屏时画布是横向滚动的，
              图例若在画布内会被滚出可视区（只剩半截）。 */}
          <span>少</span>
          {HEATMAP_PIECES.map((p, i) => (
            <span key={p.label} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              <span
                aria-hidden
                style={{ width: 12, height: 12, background: BUCKET_COLORS[i], border: '1px solid var(--border-alt)' }}
              />
              {p.label}
            </span>
          ))}
          <span>多</span>
        </div>
      )}

      {data && (
        <p style={{ margin: '10px 0 0', fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 }}>
          {data.undated > 0 && (
            <>
              另有 <Num color="var(--text)">{data.undated}</Num> 条没有投递日期（内推 / 猎头免投递）·{' '}
            </>
          )}
          已归档也计入（与「已投递」指标卡口径不同，后者不含归档）· 按 {data.timezone} 划天
        </p>
      )}
    </Card>
  )
}

function buildOption(
  d: HeatmapData,
  filled: Array<[string, number]>,
  weekStart: number,
): echarts.EChartsOption {
  const muted = resolve('var(--text-muted)')
  // 窗口跨年（「最近一年」）才画年份标签；单年视图里切换器已经说明年份了。
  const spansYears = d.from.slice(0, 4) !== d.to.slice(0, 4)
  return {
    textStyle: { fontFamily: 'Barlow, "Noto Sans SC", system-ui, sans-serif' },
    tooltip: {
      trigger: 'item',
      formatter: (p: unknown) => {
        // p.data 是 [date, count]（heatmap 在 calendar 坐标系下的值对）。
        const v = (p as { data?: unknown }).data
        if (!Array.isArray(v) || typeof v[0] !== 'string') return ''
        const head = formatDayZh(v[0])
        const n = Number(v[1]) || 0
        return n > 0 ? `${head} · 投递 ${n} 个` : `${head} · 没有投递`
      },
    },
    visualMap: {
      type: 'piecewise',
      // 只用来给格子上色（§4.1 的 5 档）；图例交给 HTML 一层画，见上方注释。
      show: false,
      pieces: HEATMAP_PIECES.map((p, i) => ({
        min: p.min,
        max: p.max ?? undefined,
        label: p.label,
        color: resolve(BUCKET_COLORS[i]),
      })),
    },
    calendar: {
      range: [d.from, d.to],
      cellSize: ['auto', CELL_H],
      left: 40,
      right: 12,
      top: spansYears ? 38 : 26,
      bottom: 14,
      dayLabel: {
        firstDay: weekStart,
        nameMap: ['日', '一', '二', '三', '四', '五', '六'],
        color: muted,
        fontSize: 10,
        margin: 6,
      },
      monthLabel: { nameMap: 'ZH', color: muted, fontSize: 11, margin: 6 },
      yearLabel: { show: spansYears, color: muted, fontSize: 11 },
      itemStyle: { color: 'transparent', borderWidth: 0 },
      splitLine: { show: false },
    },
    series: [
      {
        type: 'heatmap',
        coordinateSystem: 'calendar',
        data: filled,
        // 空格子也要看得见轮廓，否则整块画布像没加载出来。
        itemStyle: { borderWidth: 1, borderColor: resolve('var(--bg)') },
        emphasis: { itemStyle: { borderWidth: 1, borderColor: resolve('var(--text)') } },
      },
    ],
  }
}
