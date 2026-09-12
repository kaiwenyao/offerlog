import type { CSSProperties, ReactNode } from 'react'

export type Tone = 'neutral' | 'accent' | 'positive' | 'warning' | 'danger' | 'info'

/* Industry tags: flat tonal fill, square, no border. */
const TONES: Record<Tone, [string, string]> = {
  neutral: ['var(--neutral-100)', 'var(--neutral-800)'],
  accent: ['var(--accent-100)', 'var(--accent-800)'],
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
    gap: 6,
    height: 20,
    padding: '0 10px',
    borderRadius: 0,
    background: bg,
    color: fg,
    fontSize: 11,
    letterSpacing: '.02em',
    lineHeight: 1,
  }
  return (
    <span style={{ ...base, ...style }} {...rest}>
      {dot && <span aria-hidden style={{ width: 6, height: 6, background: 'currentColor', flex: '0 0 auto' }} />}
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
        height: 26,
        padding: '0 11px',
        borderRadius: 0,
        background: selected ? 'var(--accent)' : 'transparent',
        color: selected ? 'var(--text-on-accent)' : 'var(--text)',
        border: '1px solid ' + (selected ? 'var(--accent)' : 'var(--border)'),
        fontSize: 12,
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
          style={{
            all: 'unset',
            cursor: 'pointer',
            opacity: 0.5,
            fontSize: 14,
            // all:unset + lineHeight:0 会把按钮压成 0 高——几何上点不到。
            // 给它真实占位，鼠标才能命中（搜索 chip 的 × 就是靠它移除的）。
            display: 'inline-grid',
            placeItems: 'center',
            width: 14,
            height: 14,
            flex: '0 0 auto',
            borderRadius: 2,
          }}
        >
          ×
        </button>
      )}
    </span>
  )
}
