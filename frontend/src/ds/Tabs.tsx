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

/**
 * Segmented control in the Industry idiom: one hairline box divided by
 * hairlines, the active segment filled with the accent. No pills, no shadow.
 */
export function Tabs({ items, value, onChange, size = 'md', fullWidth = false, ariaLabel, style }: TabsProps) {
  const h = size === 'sm' ? 28 : 34
  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      style={{
        display: 'inline-flex',
        border: '1px solid var(--border)',
        width: fullWidth ? '100%' : undefined,
        ...style,
      }}
    >
      {items.map((it, i) => {
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
              height: h,
              padding: '0 var(--space-4)',
              fontFamily: 'var(--font-display)',
              fontWeight: 600,
              letterSpacing: '.04em',
              fontSize: size === 'sm' ? 12 : 13,
              cursor: 'pointer',
              background: on ? 'var(--accent)' : 'transparent',
              color: on ? 'var(--text-on-accent)' : 'var(--text)',
              borderLeft: i === 0 ? undefined : '1px solid var(--border)',
              transition: 'var(--transition-control)',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 6,
            }}
          >
            {l}
          </button>
        )
      })}
    </div>
  )
}
