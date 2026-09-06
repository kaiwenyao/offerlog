import type { CSSProperties, ReactNode } from 'react'

export type Tone = 'neutral' | 'accent' | 'positive' | 'warning' | 'danger' | 'info'

const TONES: Record<Tone, [string, string]> = {
  neutral: ['var(--surface-thin)', 'var(--text-muted)'],
  accent: ['var(--accent-soft)', 'var(--accent-hover)'],
  positive: ['var(--positive-soft)', 'var(--positive-strong)'],
  warning: ['var(--warning-soft)', 'var(--warning-strong)'],
  danger: ['var(--danger-soft)', 'var(--danger-strong)'],
  info: ['var(--info-soft)', 'var(--info-strong)'],
}

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone
  dot?: boolean
  children?: ReactNode
}

export function Badge({ tone = 'neutral', dot = false, style, children, ...rest }: BadgeProps) {
  const [bg, fg] = TONES[tone] ?? TONES.neutral
  const base: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 'var(--space-2)',
    height: 22,
    padding: '0 var(--space-3)',
    borderRadius: 'var(--radius-pill)',
    background: bg,
    color: fg,
    font: 'var(--type-caption)',
  }
  return (
    <span style={{ ...base, ...style }} {...rest}>
      {dot && <span aria-hidden style={{ width: 6, height: 6, borderRadius: '50%', background: 'currentColor' }} />}
      {children}
    </span>
  )
}

export interface TagProps extends React.HTMLAttributes<HTMLSpanElement> {
  selected?: boolean
  onRemove?: (e: React.MouseEvent) => void
}

export function Tag({ selected = false, onRemove, onClick, style, children, ...rest }: TagProps) {
  return (
    <span
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      aria-pressed={onClick ? selected : undefined}
      onKeyDown={
        onClick
          ? (e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                onClick(e as unknown as React.MouseEvent<HTMLSpanElement>)
              }
            }
          : undefined
      }
      className="ds-tag"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--space-2)',
        height: 28,
        padding: '0 var(--space-3)',
        borderRadius: 'var(--radius-pill)',
        background: selected ? 'var(--accent-soft)' : 'var(--surface)',
        color: selected ? 'var(--accent-hover)' : 'var(--text)',
        border: '1px solid ' + (selected ? 'var(--accent-border)' : 'var(--border)'),
        font: 'var(--type-caption)',
        cursor: onClick ? 'pointer' : 'default',
        transition: 'var(--transition-control)',
        ...style,
      }}
      {...rest}
    >
      {children}
      {onRemove && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onRemove(e)
          }}
          aria-label="移除"
          style={{ all: 'unset', cursor: 'pointer', lineHeight: 0, opacity: 0.5, fontSize: 14 }}
        >
          ×
        </button>
      )}
    </span>
  )
}
