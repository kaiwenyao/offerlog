import type { CSSProperties, ReactNode } from 'react'

export type Tone = 'neutral' | 'accent' | 'positive' | 'warning' | 'danger' | 'info'

const TONES: Record<Tone, [string, string]> = {
  neutral: ['rgba(15,15,20,.06)', 'var(--text-muted)'],
  accent: ['rgba(139,92,246,.14)', '#6d31d9'],
  positive: ['var(--positive-soft)', '#1f7d53'],
  warning: ['var(--warning-soft)', '#8a5510'],
  danger: ['var(--danger-soft)', '#a82f3d'],
  info: ['var(--info-soft)', '#2f5cb0'],
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
        background: selected ? 'rgba(139,92,246,.16)' : 'var(--surface)',
        color: selected ? '#6d31d9' : 'var(--text)',
        border: '1px solid ' + (selected ? 'rgba(139,92,246,.28)' : 'var(--border)'),
        backdropFilter: 'var(--blur-sm)',
        WebkitBackdropFilter: 'var(--blur-sm)',
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
