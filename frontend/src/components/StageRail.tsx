// 工序进度条：表格行与看板卡片上的那一排小格子。
//
// 迁移 00006 起，抽屉里那条带标签的完整工序线（StageTrail）已经被参考流程图
// components/FlowGuide.tsx 取代——固定流水线不再是记录进度的方式，它只剩「一眼
// 看到走到哪了」这一个用途，所以这里只保留紧凑版。
import { FLOW_ORDER, statusMeta } from '../lib/status'

/** Statuses that end a pipeline without reaching 已接受. */
const DEAD = new Set(['rejected', 'withdrawn', 'closed'])

/**
 * Where the rail's highlight sits. For a live application that is the current
 * status; for one that ended badly it is the last stage actually walked, so
 * the rail still shows how far the process got before it stopped. Returns -1
 * when neither is known — the rail then stays empty rather than implying the
 * application never left 待投递.
 */
function railIndex(current: string, path: string[]): number {
  const direct = FLOW_ORDER.indexOf(current)
  if (direct >= 0) return direct
  const walked = path.map((p) => FLOW_ORDER.indexOf(p)).filter((i) => i >= 0)
  return walked.length ? Math.max(...walked) : -1
}

interface CellSkin {
  fill: string
  edge: string
  label: string
}

/**
 * 方案 §5：当前 / 曾经历 / 未经历 三态，不再按序号推断。跳过初筛直接做 OA
 * 时初筛保持空心「未经历」，回退到后面的阶段时早先走过的节点仍保留「曾经历」
 * 标记。没有 stage_history 时（旧数据、列表接口未带）才退回按序号填充。
 */
function cellSkin(i: number, at: number, dead: boolean, visited: boolean): CellSkin {
  if (i === at) {
    return {
      fill: dead ? 'var(--neutral-500)' : 'var(--accent)',
      edge: 'transparent',
      label: 'var(--text)',
    }
  }
  if (visited) {
    return {
      fill: dead ? 'var(--neutral-300)' : 'var(--accent-300)',
      edge: 'transparent',
      label: 'var(--text)',
    }
  }
  return { fill: 'transparent', edge: 'var(--border)', label: 'var(--neutral-500)' }
}

/** Reached status keys → the walked pipeline path (used as the rail path). */
function walkedPath(dates?: Record<string, string> | null): string[] {
  if (!dates) return []
  return FLOW_ORDER.filter((s) => s !== 'accepted' && dates[s])
}

/**
 * Stages the record really passed through: the explicit path plus every stage
 * with an arrival date. `known` is false when neither source exists, in which
 * case the caller must fall back to index-based filling.
 */
function visitedStages(path: string[], dates?: Record<string, string> | null) {
  const set = new Set<string>(path)
  if (dates) for (const s of Object.keys(dates)) if (dates[s]) set.add(s)
  return { set, known: set.size > 0 }
}

interface StageRailProps {
  status: string
  pips: string[]
  path?: string[]
  /** stage → YYYY-MM-DD（最早到达日）——作 path 并在 title 里显示 */
  dates?: Record<string, string> | null
  /** Board cards get a 4px bar with no inline label. */
  thin?: boolean
}

/**
 * Compact rail rendered inside a table row or board card — same semantics as
 * StageTrail, without the stage labels (design: the 工序进度 column). When the
 * server's stage history is available it supplies the walked path, so a
 * terminated application still fills the pips up to the last stage it really
 * reached; every reached pip shows its arrival date in the tooltip.
 */
export function StageRail({ status, pips, path = [], dates = null, thin = false }: StageRailProps) {
  const effPath = path.length ? path : walkedPath(dates)
  const at = railIndex(status, effPath)
  const dead = DEAD.has(status)
  const capped = at < 0 ? -1 : Math.min(at, pips.length - 1)
  const { set: reached, known } = visitedStages(effPath, dates)

  return (
    <span className={'rail' + (thin ? ' thin' : '')} aria-label={`工序进度 ${statusMeta(status).label}`}>
      {pips.map((p, i) => {
        const skin = cellSkin(i, capped, dead, known ? reached.has(p) : i < capped)
        const date = dates?.[p]
        return (
          <span
            key={p}
            title={
              statusMeta(p).label +
              (date ? ` · ${date}` : '') +
              (known && !reached.has(p) && i !== capped ? ' · 未经历' : '')
            }
            style={{
              background: skin.fill,
              boxShadow: `inset 0 0 0 1px ${skin.edge}`,
              color: i === capped ? 'var(--text-on-accent)' : 'var(--text)',
            }}
          >
            {!thin && i === capped ? statusMeta(p).label.slice(0, 2) : ''}
          </span>
        )
      })}
    </span>
  )
}
