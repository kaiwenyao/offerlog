import type { CSSProperties } from 'react'

export type TabItem = string | { value: string; label: string }

export interface TabsProps {
  items: TabItem[]
  value: string
  onChange: (value: string) => void
  size?: 'sm' | 'md'
  fullWidth?: boolean
  ariaLabel?: string
  style?: CSSProperties
}

export function Tabs({ items, value, onChange, size = 'md', fullWidth = false, ariaLabel, style }: TabsProps) {
  const h = size === 'sm' ? 32 : 40
  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      style={{
        display: 'inline-flex',
        gap: 'var(--space-1)',
        padding: 3,
        borderRadius: 'var(--radius-pill)',
        background: 'var(--surface-thin)',
        border: '1px solid var(--border)',
        width: fullWidth ? '100%' : undefined,
        ...style,
      }}
    >
      {items.map((it) => {
        const v = typeof it === 'string' ? it : it.value
        const l = typeof it === 'string' ? it : it.label
        const on = v === value
        return (
          <button
            key={v}
            type="button"
            role="tab"
            aria-selected={on}
            onClick={() => onChange(v)}
            style={{
              all: 'unset',
              boxSizing: 'border-box',
              flex: fullWidth ? 1 : undefined,
              textAlign: 'center',
              height: h - 6,
              padding: '0 var(--space-4)',
              borderRadius: 'var(--radius-pill)',
              font: 'var(--type-ui)',
              fontSize: size === 'sm' ? 'var(--text-13)' : 'var(--text-15)',
              cursor: 'pointer',
              background: on ? 'var(--surface)' : 'transparent',
              color: on ? 'var(--text)' : 'var(--text-muted)',
              boxShadow: on ? 'var(--shadow-card)' : 'none',
              transition: 'var(--transition-control)',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 'var(--space-2)',
            }}
          >
            {l}
          </button>
        )
      })}
    </div>
  )
}
