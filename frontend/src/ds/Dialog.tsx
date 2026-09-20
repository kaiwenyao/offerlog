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
    // 排除关闭按钮：它在 DOM 里排在标题和表单之前，不排除的话每个弹窗打开时
    // 焦点都会落在 ✕ 上，CreateDialog 的 autoFocus(公司) 就永远拿不到焦点。
    panel.current?.querySelector<HTMLElement>('input,select,textarea,button:not([data-dialog-close])')?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  // 背景不跟着滚：没有这一行时，在 backdrop 上滚轮滚的是后面那张长列表，关掉
  // 弹窗才发现位置跑了。嵌套弹窗各自加一层 class，最后一层卸载时才解锁。
  useEffect(() => {
    if (!open) return
    document.body.classList.add('ol-modal-open')
    return () => {
      if (document.querySelectorAll('.modal-backdrop').length <= 1) {
        document.body.classList.remove('ol-modal-open')
      }
    }
  }, [open])

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
          position: 'relative',
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
          {/* 每个弹窗都要有一个**看得见**的出口。Esc 和点 backdrop 一直是可以的，
              但有几个表单的 footer 里只有「保存」，而手机上 backdrop 只剩窄窄
              一圈 padding——没有 × 的话就像被关在弹窗里了。 */}
          <button
            type="button"
            // 刻意不叫「关闭」：抽屉头部的关闭按钮就是 aria-label="关闭"，而弹窗是
            // .drawer 的 DOM 后代（Dialog 不走 portal），同名会让
            // `.drawer button[aria-label="关闭"]` 这类选择器一次命中两个。
            aria-label="关闭弹窗"
            data-dialog-close=""
            title="关闭（Esc）"
            onClick={onClose}
            style={{
              all: 'unset',
              boxSizing: 'border-box',
              position: 'absolute',
              top: 'var(--space-3)',
              right: 'var(--space-3)',
              width: 26,
              height: 26,
              display: 'grid',
              placeItems: 'center',
              cursor: 'pointer',
              fontSize: 15,
              lineHeight: 1,
              color: 'var(--text-muted)',
            }}
          >
            ✕
          </button>
          {title && (
            <h2
              style={{
                font: 'var(--type-h4)',
                margin: '0 0 var(--space-2)',
                paddingRight: 'var(--space-6)',
                letterSpacing: 'var(--tracking-display)',
              }}
            >
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
