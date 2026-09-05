import { FLOW_ORDER, statusMeta } from '../lib/status'

/**
 * 阶段轨迹 (plan §4.1): a fine line connects real stages the application has
 * walked; the current stage is highlighted; unvisited stages are not filled.
 * Only used in detail views, not squeezed into list rows.
 */
export function StageTrail({ current, path }: { current: string; path: string[] }) {
  // order statuses by the canonical flow, but show any visited status in path
  const meta = new Map<string, string[]>()
  const walked: string[] = []
  const seen = new Set<string>()
  const ordered = [...FLOW_ORDER]
  for (const p of path) {
    if (!seen.has(p) && ordered.includes(p)) {
      seen.add(p)
      walked.push(p)
    }
  }
  void meta
  const idx = walked.indexOf(current)
  // If current is not on the flow order (e.g. withdrawn), still mark it.
  const showCurrent = ordered.includes(current) ? walked : [...walked, current]
  return (
    <div className="stage-trail" aria-label="阶段轨迹">
      {showCurrent.map((st, i) => {
        const m = statusMeta(st)
        const isCurrent = st === current
        const passed = idx === -1 ? showCurrent.indexOf(st) < showCurrent.length - 1 : i <= idx && !isCurrent
        return (
          <span key={st} className="stage-trail-part" style={{ display: 'contents' }}>
            <span className={`stage-node${passed ? ' passed' : ''}${isCurrent ? ' current' : ''}`}>
              <span className="dot" aria-hidden />
              {isCurrent && <span aria-hidden>{m.icon}</span>}
              <span>{m.label}</span>
            </span>
            {i < showCurrent.length - 1 && <span className={`stage-link${passed ? ' passed' : ''}`} aria-hidden />}
          </span>
        )
      })}
    </div>
  )
}
