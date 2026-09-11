import type { ReactNode } from 'react'
import { comboLabel, statusMeta } from '../lib/status'
import { Badge, Card, Dialog } from '../ds'

/**
 * Status pill. Keeps the `.status-chip` hook the Playwright suites assert on
 * while rendering the design's dot + label badge.
 *
 * Status badge. With a substatus it shows the concrete progress（「准备 OA」
 * 而不是笼统的「笔试作业」），方案 §5。子状态为空时保持大阶段标签。
 */
export function StatusChip({ status, substatus }: { status: string; substatus?: string | null }) {
  const m = statusMeta(status)
  const label = substatus ? comboLabel(status, substatus) : m.label
  return (
    <Badge className="status-chip" tone={m.tone} dot>
      {label}
    </Badge>
  )
}

/**
 * Colour marker used by group headers, board columns and the activity feed.
 * Square, matching the system's registration-mark language.
 */
export function Dot({ color, size = 7 }: { color: string; size?: number }) {
  return <span aria-hidden style={{ width: size, height: size, background: color, flex: '0 0 auto' }} />
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
    <Card padding="var(--space-6)" style={{ textAlign: 'center', color: 'var(--text-muted)' }}>
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

/** Tabular figure — dates, counts, ids. Barlow's lining numerals align in
 *  columns without switching to a monospace face. */
export function Num({ children, color }: { children: ReactNode; color?: string }) {
  return <span style={{ fontVariantNumeric: 'tabular-nums', fontSize: 12, color }}>{children}</span>
}
