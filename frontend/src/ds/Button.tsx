import { useState, type CSSProperties, type ReactNode } from 'react'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ControlSize = 'sm' | 'md' | 'lg'

const BASE: CSSProperties = {
  font: 'var(--type-ui)',
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  gap: 'var(--space-2)',
  border: '1px solid transparent',
  borderRadius: 'var(--radius-control)',
  cursor: 'pointer',
  transition: 'var(--transition-control)',
  whiteSpace: 'nowrap',
  textDecoration: 'none',
}

const SIZES: Record<ControlSize, CSSProperties> = {
  sm: { height: 'var(--control-h-sm)', padding: '0 var(--space-3)', fontSize: 'var(--text-13)' },
  md: { height: 'var(--control-h-md)', padding: '0 var(--space-5)', fontSize: 'var(--text-15)' },
  lg: { height: 'var(--control-h-lg)', padding: '0 var(--space-6)', fontSize: 'var(--text-17)' },
}

export interface ButtonProps
  extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'type'> {
  variant?: ButtonVariant
  size?: ControlSize
  fullWidth?: boolean
  iconLeft?: ReactNode
  iconRight?: ReactNode
  type?: 'button' | 'submit' | 'reset'
}

export function Button({
  variant = 'primary',
  size = 'md',
  disabled = false,
  fullWidth = false,
  iconLeft,
  iconRight,
  type = 'button',
  style,
  children,
  ...rest
}: ButtonProps) {
  const [hover, setHover] = useState(false)
  const [press, setPress] = useState(false)

  const variants: Record<ButtonVariant, CSSProperties> = {
    primary: {
      background: hover ? 'var(--accent-hover)' : 'var(--accent)',
      color: 'var(--text-on-accent)',
      boxShadow: press ? 'none' : 'var(--shadow-card)',
    },
    secondary: {
      background: hover ? 'var(--surface-hover)' : 'var(--surface)',
      color: 'var(--text)',
      borderColor: 'var(--border)',
      boxShadow: 'var(--shadow-card)',
    },
    ghost: {
      background: hover ? 'var(--surface-thin)' : 'transparent',
      color: 'var(--text-muted)',
    },
    danger: {
      background: hover ? 'var(--danger-hover)' : 'var(--danger)',
      color: 'var(--text-on-accent)',
      boxShadow: press ? 'none' : 'var(--shadow-card)',
    },
  }

  return (
    <button
      type={type}
      disabled={disabled}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => {
        setHover(false)
        setPress(false)
      }}
      onMouseDown={() => setPress(true)}
      onMouseUp={() => setPress(false)}
      style={{
        ...BASE,
        ...SIZES[size],
        ...variants[variant],
        width: fullWidth ? '100%' : undefined,
        opacity: disabled ? 0.45 : 1,
        pointerEvents: disabled ? 'none' : undefined,
        transform: press ? 'scale(.98)' : 'none',
        ...style,
      }}
      {...rest}
    >
      {iconLeft}
      {children}
      {iconRight}
    </button>
  )
}

export interface LinkButtonProps
  extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  variant?: ButtonVariant
  size?: ControlSize
  fullWidth?: boolean
  iconLeft?: ReactNode
  iconRight?: ReactNode
}

/** Anchor sharing the Button skin — for real navigations and downloads. */
export function LinkButton({
  variant = 'secondary',
  size = 'md',
  fullWidth = false,
  iconLeft,
  iconRight,
  style,
  children,
  ...rest
}: LinkButtonProps) {
  const [hover, setHover] = useState(false)
  const variants: Record<ButtonVariant, CSSProperties> = {
    primary: {
      background: hover ? 'var(--accent-hover)' : 'var(--accent)',
      color: 'var(--text-on-accent)',
      boxShadow: 'var(--shadow-card)',
    },
    secondary: {
      background: hover ? 'var(--surface-hover)' : 'var(--surface)',
      color: 'var(--text)',
      borderColor: 'var(--border)',
      boxShadow: 'var(--shadow-card)',
    },
    ghost: { background: hover ? 'var(--surface-thin)' : 'transparent', color: 'var(--text-muted)' },
    danger: { background: hover ? 'var(--danger-hover)' : 'var(--danger)', color: 'var(--text-on-accent)' },
  }
  return (
    <a
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        ...BASE,
        ...SIZES[size],
        ...variants[variant],
        width: fullWidth ? '100%' : undefined,
        ...style,
      }}
      {...rest}
    >
      {iconLeft}
      {children}
      {iconRight}
    </a>
  )
}

const ICON_DIM: Record<ControlSize, number> = { sm: 32, md: 40, lg: 48 }

export interface IconButtonProps extends Omit<ButtonProps, 'iconLeft' | 'iconRight'> {
  label: string
}

export function IconButton({ variant = 'secondary', size = 'md', label, style, children, ...rest }: IconButtonProps) {
  return (
    <Button
      variant={variant}
      size={size}
      aria-label={label}
      title={label}
      style={{ width: ICON_DIM[size], padding: 0, borderRadius: 'var(--radius-control)', ...style }}
      {...rest}
    >
      {children}
    </Button>
  )
}
