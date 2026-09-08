import { ENDED, FLOW_ORDER, statusMeta } from '../lib/status'

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

function cellSkin(i: number, at: number, dead: boolean): CellSkin {
  if (i === at) {
    return {
      fill: dead ? 'var(--neutral-500)' : 'var(--accent)',
      edge: 'transparent',
      label: 'var(--text)',
    }
  }
  if (i < at) {
    return {
      fill: dead ? 'var(--neutral-300)' : 'var(--accent-300)',
      edge: 'transparent',
      label: 'var(--text)',
    }
  }
  return { fill: 'transparent', edge: 'var(--border)', label: 'var(--neutral-500)' }
}

/**
 * 工序线: the whole pipeline as a run of process blocks, filled up to the
 * stage the application has reached and hairline-outlined beyond it.
 */
export function StageTrail({ current, path }: { current: string; path: string[] }) {
  const at = railIndex(current, path)
  const dead = DEAD.has(current)
  const done = ENDED.has(current)

  return (
    <div className="stage-trail" aria-label={`工序线 ${statusMeta(current).label}`}>
      {FLOW_ORDER.map((stage, i) => {
        const skin = cellSkin(i, at, dead)
        return (
          <span key={stage} className="stage-node">
            <span
              aria-hidden
              className="bar"
              style={{ background: skin.fill, boxShadow: `inset 0 0 0 1px ${skin.edge}` }}
            />
            <span className="label" style={{ color: skin.label }}>
              {statusMeta(stage).label}
            </span>
            {i === at && (
              <span style={{ fontSize: 10, letterSpacing: '.06em', color: 'var(--neutral-600)' }}>
                {dead ? statusMeta(current).label : done ? '完成' : '当前'}
              </span>
            )}
          </span>
        )
      })}
    </div>
  )
}

interface StageRailProps {
  status: string
  pips: string[]
  path?: string[]
  /** Board cards get a 4px bar with no inline label. */
  thin?: boolean
}

/**
 * Compact rail rendered inside a table row or board card — same semantics as
 * StageTrail, without the stage labels (design: the 工序进度 column).
 */
export function StageRail({ status, pips, path = [], thin = false }: StageRailProps) {
  const at = railIndex(status, path)
  const dead = DEAD.has(status)
  const capped = at < 0 ? -1 : Math.min(at, pips.length - 1)

  return (
    <span className={'rail' + (thin ? ' thin' : '')} aria-label={`工序进度 ${statusMeta(status).label}`}>
      {pips.map((p, i) => {
        const skin = cellSkin(i, capped, dead)
        return (
          <span
            key={p}
            title={statusMeta(p).label}
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
