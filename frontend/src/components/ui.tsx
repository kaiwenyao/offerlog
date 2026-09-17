import type { ReactNode } from 'react'
import { comboLabel, statusMeta } from '../lib/status'
import { Badge, Button, Card, Dialog } from '../ds'

/**
 * Status pill. Keeps the `.status-chip` hook the Playwright suites assert on
 * while rendering the design's dot + label badge.
 *
 * Status badge. With a substatus it shows the concrete progress（「准备 OA」
 * 而不是笼统的「OA / 作业」），方案 §5。子状态为空时保持大阶段标签。
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

/**
 * 二次确认弹窗（破坏性操作专用）。
 *
 * 附件、备注、时间线事件、自定义属性、面试 / OA / 待办的删除都是硬删、不可撤销，
 * 而它们的按钮往往就挨着「预览」「下载」。确认框必须说清**后果**（删的是什么、
 * 会连带什么），而不是一句「确定删除？」——所以正文由调用方传入。
 *
 * 确认按钮在 `pending` 期间禁用，避免连点发出两次删除。
 */
export function ConfirmDialog({
  title,
  children,
  confirmLabel = '删除',
  danger = true,
  pending = false,
  onConfirm,
  onClose,
}: {
  title: string
  children: ReactNode
  confirmLabel?: string
  /** 破坏性操作默认用警示色；「确实要继续」类确认可关掉。 */
  danger?: boolean
  pending?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  return (
    <Dialog
      title={title}
      onClose={onClose}
      width={420}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose} disabled={pending}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={pending}
            onClick={onConfirm}
            style={danger ? { background: 'var(--danger)', borderColor: 'var(--danger)' } : undefined}
          >
            {pending ? <Spinner size={14} /> : confirmLabel}
          </Button>
        </>
      }
    >
      <div style={{ fontSize: 13, lineHeight: 1.6, color: 'var(--text-muted)' }}>{children}</div>
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
