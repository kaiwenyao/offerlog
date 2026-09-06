import { useState, type CSSProperties, type ReactNode } from 'react'

export type CardVariant = 'glass' | 'strong' | 'outline'

const VARIANTS: Record<CardVariant, CSSProperties> = {
  glass: {
    background: 'var(--surface-card)',
    border: '1px solid var(--border)',
    boxShadow: 'var(--shadow-card)',
  },
  strong: {
    background: 'var(--surface-strong)',
    border: '1px solid var(--border)',
    boxShadow: 'var(--shadow-card)',
  },
  outline: {
    background: 'var(--surface-thin)',
    border: '1px solid var(--border-alt)',
  },
}

export interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  padding?: string | number
  radius?: string | number
  variant?: CardVariant
  interactive?: boolean
  children?: ReactNode
}

export function Card({
  padding = 'var(--space-6)',
  radius = 'var(--radius-card)',
  variant = 'glass',
  interactive = false,
  style,
  children,
  ...rest
}: CardProps) {
  const [hover, setHover] = useState(false)
  const v = VARIANTS[variant]
  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        borderRadius: radius,
        padding,
        ...v,
        transform: interactive && hover ? 'translateY(-2px)' : 'none',
        boxShadow: interactive && hover ? 'var(--shadow-pop)' : v.boxShadow,
        transition: 'transform var(--dur-base) var(--ease-glass),box-shadow var(--dur-base) var(--ease-glass)',
        cursor: interactive ? 'pointer' : undefined,
        ...style,
      }}
      {...rest}
    >
      {children}
    </div>
  )
}

/** Section heading used on every panel. */
export function PanelTitle({ children, style, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      style={{ font: 'var(--type-ui)', fontSize: 'var(--text-15)', fontWeight: 500, ...style }}
      {...rest}
    >
      {children}
    </div>
  )
}

/** 11px uppercase-ish tracking label above titles and section groups. */
export function Eyebrow({ children, style, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      style={{
        fontSize: 11,
        fontWeight: 500,
        letterSpacing: '.1em',
        color: 'var(--text-muted)',
        ...style,
      }}
      {...rest}
    >
      {children}
    </div>
  )
}
