import { ENDED, FLOW_ORDER, statusMeta } from '../lib/status'
import { effectiveZone } from '../lib/tz'

/** Statuses that end a pipeline without reaching 已接受. */
const DEAD = new Set(['rejected', 'withdrawn', 'closed'])

/**
 * YYYY-MM-DD → the compact label under a rail node: MM-DD for the current
 * year, the full date for any other year (applications routinely span year
 * boundaries and a bare "12-03" would hide which December).
 */
function shortDay(day: string): string {
  const [y, ...rest] = day.split('-')
  const zone = effectiveZone() ?? undefined
  const thisYear = Number(new Date().toLocaleString('en-CA', { timeZone: zone, year: 'numeric' }))
  return Number(y) === thisYear ? rest.join('-') : day
}

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

/** Reached status keys → the walked pipeline path (used as the rail path). */
function walkedPath(dates?: Record<string, string> | null): string[] {
  if (!dates) return []
  return FLOW_ORDER.filter((s) => s !== 'accepted' && dates[s])
}

/**
 * 工序线: the whole pipeline as a run of process blocks, filled up to the
 * stage the application has reached and hairline-outlined beyond it. When the
 * server's stage history is available its reached-status dates double as the
 * walked path (终态岗位画到它真正走到的那一格) and render under each reached
 * block's label.
 */
export function StageTrail({
  current,
  path,
  dates,
}: {
  current: string
  path: string[]
  /** stage → YYYY-MM-DD（最早到达日，include=stage_history） */
  dates?: Record<string, string> | null
}) {
  const at = railIndex(current, path.length ? path : walkedPath(dates))
  const dead = DEAD.has(current)
  const done = ENDED.has(current)

  return (
    <div className="stage-trail" aria-label={`工序线 ${statusMeta(current).label}`}>
      {FLOW_ORDER.map((stage, i) => {
        const skin = cellSkin(i, at, dead)
        const full = dates?.[stage]
        const date = full ? shortDay(full) : undefined // compact inside the tight block
        const isTip = i === at
        const sub = isTip
          ? date
            ? `${date} · ${dead ? statusMeta(current).label : done ? '完成' : '当前'}`
            : dead
              ? statusMeta(current).label
              : done
                ? '完成'
                : '当前'
          : i < at && date
            ? date
            : ''
        return (
          <span
            key={stage}
            className="stage-node"
            title={full ? `${statusMeta(stage).label} · ${full.split('-').join('/')}` : statusMeta(stage).label}
          >
            <span
              aria-hidden
              className="bar"
              style={{ background: skin.fill, boxShadow: `inset 0 0 0 1px ${skin.edge}` }}
            />
            <span className="label" style={{ color: skin.label }}>
              {statusMeta(stage).label}
            </span>
            {sub && (
              <span style={{ fontSize: 10, letterSpacing: '.06em', color: 'var(--neutral-600)' }}>{sub}</span>
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

  return (
    <span className={'rail' + (thin ? ' thin' : '')} aria-label={`工序进度 ${statusMeta(status).label}`}>
      {pips.map((p, i) => {
        const skin = cellSkin(i, capped, dead)
        const date = dates?.[p]
        return (
          <span
            key={p}
            title={statusMeta(p).label + (date ? ` · ${date}` : '')}
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
