import { useState, type CSSProperties, type ReactNode } from 'react'

export type CardVariant = 'glass' | 'strong' | 'outline'

interface Skin {
  background: string
  borderColor: string
}

const VARIANTS: Record<CardVariant, Skin> = {
  /* The blueprint frame: transparent body, one hairline edge. */
  glass: { background: 'transparent', borderColor: 'var(--border)' },
  strong: { background: 'var(--bg)', borderColor: 'var(--border)' },
  /* Filled inset block, used for sub-panels sitting inside a frame. */
  outline: { background: 'var(--neutral-100)', borderColor: 'var(--border-alt)' },
}

/** Registration marks drawn just outside a framed box's corners. */
export function BlueprintCorners() {
  return (
    <>
      <i className="corner tl" aria-hidden />
      <i className="corner tr" aria-hidden />
      <i className="corner bl" aria-hidden />
      <i className="corner br" aria-hidden />
    </>
  )
}

export interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  padding?: string | number
  radius?: string | number
  variant?: CardVariant
  interactive?: boolean
  /** Draw the corner registration marks. Off for filled/inset blocks. */
  marks?: boolean
  children?: ReactNode
}

export function Card({
  padding = 'var(--space-4)',
  radius = 0,
  variant = 'glass',
  interactive = false,
  marks,
  className,
  style,
  children,
  ...rest
}: CardProps) {
  const [hover, setHover] = useState(false)
  const v = VARIANTS[variant]
  const showMarks = marks ?? variant !== 'outline'
  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      className={(showMarks ? 'blueprint' : '') + (className ? ' ' + className : '')}
      style={{
        position: showMarks ? 'relative' : undefined,
        borderRadius: radius,
        padding,
        borderStyle: 'solid',
        borderWidth: 1,
        borderColor: interactive && hover ? 'var(--accent)' : v.borderColor,
        background: interactive && hover ? 'var(--accent-100)' : v.background,
        transition: 'var(--transition-control)',
        cursor: interactive ? 'pointer' : undefined,
        ...style,
      }}
      {...rest}
    >
      {showMarks && <BlueprintCorners />}
      {children}
    </div>
  )
}

/** Section heading used on every panel — condensed face, like the mock's h5. */
export function PanelTitle({ children, style, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      style={{
        fontFamily: 'var(--font-display)',
        fontWeight: 'var(--weight-semibold)' as unknown as number,
        fontSize: 16,
        lineHeight: 1.2,
        ...style,
      }}
      {...rest}
    >
      {children}
    </div>
  )
}

/** Uppercase micro annotation above titles and section groups. */
export function Eyebrow({ children, style, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      style={{
        fontSize: 10,
        fontWeight: 400,
        letterSpacing: 'var(--tracking-micro)',
        textTransform: 'uppercase',
        color: 'var(--accent-700)',
        ...style,
      }}
      {...rest}
    >
      {children}
    </div>
  )
}
