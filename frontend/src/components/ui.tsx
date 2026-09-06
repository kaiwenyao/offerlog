import { useEffect, useRef } from 'react'
import { statusMeta } from '../lib/status'

export function StatusChip({ status }: { status: string }) {
  const m = statusMeta(status)
  // dot + label (§4.1): the label carries the meaning; the colored dot is texture
  return <span className={`status-chip ${m.color}`}>{m.label}</span>
}

export function Modal({
  title,
  onClose,
  children,
  footer,
}: {
  title: string
  onClose: () => void
  children: React.ReactNode
  footer?: React.ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', h)
    ref.current?.querySelector<HTMLElement>('input,button,select,textarea')?.focus()
    return () => document.removeEventListener('keydown', h)
  }, [onClose])
  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <h2 className="panel-title" style={{ margin: 0 }}>{title}</h2>
          <button className="btn btn-ghost" style={{ minWidth: 36, padding: 0 }} onClick={onClose} aria-label="关闭">
            ✕
          </button>
        </div>
        <div className="mt16">{children}</div>
        {footer && <div className="mt16 row" style={{ justifyContent: 'flex-end' }}>{footer}</div>}
      </div>
    </div>
  )
}

export function Spinner() {
  return <span className="spinner" role="status" aria-label="加载中" />
}

export function EmptyHint({ children }: { children: React.ReactNode }) {
  return <div className="card empty-hint">{children}</div>
}
