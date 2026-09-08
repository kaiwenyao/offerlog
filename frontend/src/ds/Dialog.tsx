import { useEffect, useRef, type ReactNode } from 'react'
import { BlueprintCorners } from './Card'

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
 * Blueprint modal: square frame with registration marks over an inked
 * backdrop. Keeps the `.modal` / `.modal-backdrop` class hooks the Playwright
 * suites select on, and closes on Escape or backdrop click.
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
        padding: 'var(--space-4)',
        background: 'color-mix(in srgb, var(--neutral-900) 50%, transparent)',
      }}
    >
      <div
        ref={panel}
        className="modal blueprint"
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === 'string' ? title : undefined}
        style={{
          width: '100%',
          maxWidth: width,
          borderRadius: 0,
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          boxShadow: 'var(--shadow-pop)',
        }}
      >
        {/* Corner marks sit OUTSIDE the frame, so the scroll box has to be the
            inner element — clipping here would eat them. */}
        <BlueprintCorners />
        <div style={{ maxHeight: 'calc(100vh - var(--space-12))', overflowY: 'auto', padding: 'var(--space-5)' }}>
          {title && (
            <h2 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)', letterSpacing: 'var(--tracking-display)' }}>
              {title}
            </h2>
          )}
          {description && (
            <p style={{ font: 'var(--type-body-sm)', color: 'var(--text-muted)', margin: '0 0 var(--space-4)' }}>
              {description}
            </p>
          )}
          {children}
          {footer && (
            <div
              style={{ display: 'flex', justifyContent: 'flex-end', gap: 'var(--space-2)', marginTop: 'var(--space-5)' }}
            >
              {footer}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
