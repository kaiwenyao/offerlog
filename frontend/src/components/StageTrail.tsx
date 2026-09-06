import { FLOW_ORDER, statusMeta } from '../lib/status'

/**
 * 阶段轨迹: a dot per stage the application has actually walked, sized up at
 * the current stage with the design's focus ring. Unvisited stages stay hollow.
 */
export function StageTrail({ current, path }: { current: string; path: string[] }) {
  const walked: string[] = []
  const seen = new Set<string>()
  for (const p of path) {
    if (!seen.has(p) && FLOW_ORDER.includes(p)) {
      seen.add(p)
      walked.push(p)
    }
  }
  const stages = FLOW_ORDER.includes(current) ? walked : [...walked, current]
  if (stages.length === 0) stages.push(current)

  const idx = stages.indexOf(current)
  const currentColor = statusMeta(current).dot

  return (
    <div className="stage-trail" aria-label="阶段轨迹">
      {stages.map((st, i) => {
        const reached = idx === -1 ? i < stages.length : i <= idx
        const isCurrent = st === current
        const isLast = i === stages.length - 1
        return (
          <span key={st + i} style={{ display: 'flex', alignItems: 'center', minWidth: 0 }}>
            <span className="stage-node">
              <span
                aria-hidden
                style={{
                  width: isCurrent ? 12 : 9,
                  height: isCurrent ? 12 : 9,
                  borderRadius: '50%',
                  background: reached ? currentColor : 'rgba(15,15,20,.12)',
                  boxShadow: isCurrent ? '0 0 0 4px rgba(139,92,246,.18)' : 'none',
                }}
              />
              <span style={{ color: reached ? 'var(--text)' : 'var(--text-muted)' }}>{statusMeta(st).label}</span>
            </span>
            {!isLast && (
              <span
                aria-hidden
                className="stage-link"
                style={{ background: i < idx ? currentColor : 'rgba(15,15,20,.1)' }}
              />
            )}
          </span>
        )
      })}
    </div>
  )
}

/**
 * Seven compact pips summarising pipeline progress inside a table row
 * (design: the 阶段推进 column).
 */
export function StagePips({ status, pips }: { status: string; pips: string[] }) {
  const idx = pips.indexOf(status)
  const color = statusMeta(status).dot
  return (
    <span className="pips" aria-label={`阶段推进 ${statusMeta(status).label}`}>
      {pips.map((p, i) => (
        <span
          key={p}
          className="pip"
          style={{
            background:
              idx === -1
                ? i < 3
                  ? color
                  : 'rgba(15,15,20,.09)'
                : i <= idx
                  ? color
                  : 'rgba(15,15,20,.09)',
          }}
        />
      ))}
    </span>
  )
}
