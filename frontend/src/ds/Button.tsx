import { useState, type CSSProperties, type ReactNode } from 'react'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ControlSize = 'sm' | 'md' | 'lg'

/** Industry controls: square, hairline-bordered, condensed label, no shadow. */
const BASE: CSSProperties = {
  font: 'var(--type-ui)',
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  gap: 6,
  border: '1px solid var(--border)',
  borderRadius: 0,
  cursor: 'pointer',
  transition: 'var(--transition-control)',
  whiteSpace: 'nowrap',
  textDecoration: 'none',
}

const SIZES: Record<ControlSize, CSSProperties> = {
  sm: { height: 'var(--control-h-sm)', padding: '0 var(--space-3)', fontSize: 12 },
  md: { height: 'var(--control-h-md)', padding: '0 var(--space-4)', fontSize: 14 },
  lg: { height: 'var(--control-h-lg)', padding: '0 var(--space-5)', fontSize: 15 },
}

/** Fill/edge pairs for each variant, resolved against hover + press state. */
function skin(variant: ButtonVariant, hover: boolean, press: boolean): CSSProperties {
  switch (variant) {
    case 'primary':
      return {
        background: press ? 'var(--accent-700)' : hover ? 'var(--accent-600)' : 'var(--accent)',
        borderColor: press ? 'var(--accent-700)' : hover ? 'var(--accent-600)' : 'var(--accent)',
        color: 'var(--text-on-accent)',
      }
    case 'secondary':
      return {
        background: press
          ? 'color-mix(in srgb, var(--text) 14%, transparent)'
          : hover
            ? 'color-mix(in srgb, var(--text) 7%, transparent)'
            : 'transparent',
        borderColor: 'var(--border)',
        color: 'var(--text)',
      }
    case 'ghost':
      return {
        background: press
          ? 'color-mix(in srgb, var(--accent) 18%, transparent)'
          : hover
            ? 'color-mix(in srgb, var(--accent) 10%, transparent)'
            : 'transparent',
        borderColor: 'transparent',
        color: 'var(--accent-700)',
      }
    case 'danger':
      return {
        background: hover ? 'var(--danger-hover)' : 'var(--danger)',
        borderColor: hover ? 'var(--danger-hover)' : 'var(--danger)',
        color: 'var(--text-on-accent)',
      }
  }
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
        ...skin(variant, hover, press),
        width: fullWidth ? '100%' : undefined,
        opacity: disabled ? 0.45 : 1,
        pointerEvents: disabled ? 'none' : undefined,
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
  return (
    <a
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        ...BASE,
        ...SIZES[size],
        ...skin(variant, hover, false),
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

const ICON_DIM: Record<ControlSize, number> = { sm: 28, md: 34, lg: 40 }

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
      style={{ width: ICON_DIM[size], padding: 0, ...style }}
      {...rest}
    >
      {children}
    </Button>
  )
}
