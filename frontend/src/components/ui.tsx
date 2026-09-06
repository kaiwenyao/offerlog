import type { ReactNode } from 'react'
import { statusMeta } from '../lib/status'
import { Badge, Card, Dialog } from '../ds'

/**
 * Status pill. Keeps the `.status-chip` hook the Playwright suites assert on
 * while rendering the design's dot + label badge.
 */
export function StatusChip({ status }: { status: string }) {
  const m = statusMeta(status)
  return (
    <Badge className="status-chip" tone={m.tone} dot>
      {m.label}
    </Badge>
  )
}

/** Colored dot used by group headers, board columns and the activity feed. */
export function Dot({ color, size = 7 }: { color: string; size?: number }) {
  return (
    <span
      aria-hidden
      style={{ width: size, height: size, borderRadius: '50%', background: color, flex: '0 0 auto' }}
    />
  )
}

export function Modal({
  title,
  onClose,
  children,
  footer,
  width,
}: {
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  width?: number
}) {
  return (
    <Dialog title={title} onClose={onClose} footer={footer} width={width}>
      {children}
    </Dialog>
  )
}

export function Spinner({ size = 18 }: { size?: number }) {
  return <span className="spinner" role="status" aria-label="加载中" style={{ width: size, height: size }} />
}

export function PageSpinner() {
  return (
    <div style={{ display: 'grid', placeItems: 'center', padding: 'var(--space-16) 0' }}>
      <Spinner size={22} />
    </div>
  )
}

export function EmptyHint({ children }: { children: ReactNode }) {
  return (
    <Card style={{ textAlign: 'center', color: 'var(--text-muted)' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)', alignItems: 'center' }}>
        {children}
      </div>
    </Card>
  )
}

export function ErrorText({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="err">
      {children}
    </p>
  )
}

/** Monospace figure — dates, counts, ids. */
export function Num({ children, color }: { children: ReactNode; color?: string }) {
  return <span style={{ fontFamily: 'var(--font-mono)', fontSize: 'var(--text-13)', color }}>{children}</span>
}
