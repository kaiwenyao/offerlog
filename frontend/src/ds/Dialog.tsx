import { useEffect, useRef, type ReactNode } from 'react'

export interface DialogProps {
  open?: boolean
  title?: ReactNode
  description?: ReactNode
  onClose: () => void
  footer?: ReactNode
  width?: number
  children?: ReactNode
}

/**
 * Glass modal. Keeps the `.modal` / `.modal-backdrop` class hooks the
 * Playwright suites select on, and closes on Escape or backdrop click.
 */
export function Dialog({ open = true, title, description, onClose, footer, width = 440, children }: DialogProps) {
  const panel = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    panel.current?.querySelector<HTMLElement>('input,button,select,textarea')?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 60,
        display: 'grid',
        placeItems: 'center',
        padding: 'var(--space-6)',
        background: 'rgba(15,15,20,.18)',
        backdropFilter: 'blur(6px)',
        WebkitBackdropFilter: 'blur(6px)',
      }}
    >
      <div
        ref={panel}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === 'string' ? title : undefined}
        style={{
          width: '100%',
          maxWidth: width,
          maxHeight: 'calc(100vh - var(--space-12))',
          overflowY: 'auto',
          borderRadius: 'var(--radius-panel)',
          padding: 'var(--space-6)',
          background: 'var(--surface-strong)',
          border: '1px solid var(--border)',
          backdropFilter: 'var(--blur-xl)',
          WebkitBackdropFilter: 'var(--blur-xl)',
          boxShadow: 'var(--highlight-inner),var(--shadow-pop)',
        }}
      >
        {title && (
          <h2 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)', letterSpacing: 'var(--tracking-display)' }}>
            {title}
          </h2>
        )}
        {description && (
          <p style={{ font: 'var(--type-body-sm)', color: 'var(--text-muted)', margin: '0 0 var(--space-5)' }}>
            {description}
          </p>
        )}
        {children}
        {footer && (
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 'var(--space-3)', marginTop: 'var(--space-6)' }}>
            {footer}
          </div>
        )}
      </div>
    </div>
  )
}
