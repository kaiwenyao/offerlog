import { useState, type CSSProperties, type ReactNode } from 'react'
import type { ControlSize } from './Button'

const HEIGHTS: Record<ControlSize, string> = {
  sm: 'var(--control-h-sm)',
  md: 'var(--control-h-md)',
  lg: 'var(--control-h-lg)',
}

function labelStyle(): CSSProperties {
  return { font: 'var(--type-caption)', color: 'var(--text-muted)' }
}

function shellStyle(focus: boolean, invalid: boolean): CSSProperties {
  return {
    background: 'var(--surface-input)',
    borderRadius: 'var(--radius-control)',
    border: '1px solid ' + (invalid ? 'var(--danger)' : 'var(--border)'),
    boxShadow: focus
      ? `0 0 0 2px var(--surface-strong),0 0 0 4px ${invalid ? 'var(--danger)' : 'var(--focus-ring)'}`
      : 'none',
    transition: 'var(--transition-control)',
  }
}

const FIELD_RESET: CSSProperties = {
  all: 'unset',
  flex: 1,
  minWidth: 0,
  font: 'var(--type-body-sm)',
  color: 'var(--text)',
}

export interface InputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'size'> {
  label?: ReactNode
  size?: ControlSize
  invalid?: boolean
  iconLeft?: ReactNode
  hint?: ReactNode
  fullWidth?: boolean
  shellStyleOverride?: CSSProperties
}

export function Input({
  label,
  size = 'md',
  invalid = false,
  disabled = false,
  iconLeft,
  hint,
  fullWidth = true,
  shellStyleOverride,
  style,
  ...rest
}: InputProps) {
  const [focus, setFocus] = useState(false)
  return (
    <label
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-2)',
        width: fullWidth ? '100%' : undefined,
        opacity: disabled ? 0.5 : 1,
        ...style,
      }}
    >
      {label && <span style={labelStyle()}>{label}</span>}
      <span
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-2)',
          height: HEIGHTS[size],
          padding: '0 var(--space-4)',
          ...shellStyle(focus, invalid),
          ...shellStyleOverride,
        }}
      >
        {iconLeft}
        <input
          disabled={disabled}
          onFocus={() => setFocus(true)}
          onBlur={() => setFocus(false)}
          style={FIELD_RESET}
          {...rest}
        />
      </span>
      {hint && (
        <span
          style={{
            font: 'var(--type-caption)',
            fontWeight: 400,
            color: invalid ? 'var(--danger)' : 'var(--text-muted)',
          }}
        >
          {hint}
        </span>
      )}
    </label>
  )
}

export interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: ReactNode
  hint?: ReactNode
  invalid?: boolean
}

export function Textarea({ label, hint, invalid = false, disabled, style, ...rest }: TextareaProps) {
  const [focus, setFocus] = useState(false)
  return (
    <label
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-2)',
        width: '100%',
        opacity: disabled ? 0.5 : 1,
        ...style,
      }}
    >
      {label && <span style={labelStyle()}>{label}</span>}
      <span style={{ display: 'flex', padding: 'var(--space-3) var(--space-4)', ...shellStyle(focus, invalid) }}>
        <textarea
          disabled={disabled}
          onFocus={() => setFocus(true)}
          onBlur={() => setFocus(false)}
          style={{ ...FIELD_RESET, resize: 'vertical', lineHeight: 1.5 }}
          {...rest}
        />
      </span>
      {hint && <span style={{ font: 'var(--type-caption)', fontWeight: 400, color: 'var(--text-muted)' }}>{hint}</span>}
    </label>
  )
}

export type SelectOption = string | { value: string; label: string }

export interface SelectProps extends Omit<React.SelectHTMLAttributes<HTMLSelectElement>, 'size'> {
  label?: ReactNode
  options: SelectOption[]
  size?: ControlSize
  fullWidth?: boolean
}

export function Select({
  label,
  options,
  size = 'md',
  disabled = false,
  fullWidth = true,
  style,
  children,
  ...rest
}: SelectProps) {
  const [focus, setFocus] = useState(false)
  return (
    <label
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-2)',
        width: fullWidth ? '100%' : undefined,
        opacity: disabled ? 0.5 : 1,
        ...style,
      }}
    >
      {label && <span style={labelStyle()}>{label}</span>}
      <span
        style={{
          position: 'relative',
          display: 'flex',
          alignItems: 'center',
          height: HEIGHTS[size],
          ...shellStyle(focus, false),
        }}
      >
        <select
          disabled={disabled}
          onFocus={() => setFocus(true)}
          onBlur={() => setFocus(false)}
          style={{
            all: 'unset',
            flex: 1,
            padding: '0 var(--space-8) 0 var(--space-4)',
            font: 'var(--type-body-sm)',
            color: 'var(--text)',
            cursor: 'pointer',
          }}
          {...rest}
        >
          {children}
          {options.map((o) => {
            const v = typeof o === 'string' ? o : o.value
            const l = typeof o === 'string' ? o : o.label
            return (
              <option key={v} value={v}>
                {l}
              </option>
            )
          })}
        </select>
        <span
          aria-hidden
          style={{
            position: 'absolute',
            right: 'var(--space-4)',
            color: 'var(--text-muted)',
            fontSize: 10,
            pointerEvents: 'none',
          }}
        >
          ▾
        </span>
      </span>
    </label>
  )
}

export interface SwitchProps {
  /** Rendered next to the track. Omit when the row already carries a label. */
  label?: ReactNode
  /** Accessible name — required when `label` is omitted or is not plain text. */
  ariaLabel?: string
  checked?: boolean
  onChange?: (next: boolean) => void
  disabled?: boolean
  style?: CSSProperties
}

export function Switch({ label, ariaLabel, checked = false, onChange, disabled = false, style }: SwitchProps) {
  return (
    <label
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--space-3)',
        cursor: disabled ? 'default' : 'pointer',
        opacity: disabled ? 0.5 : 1,
        font: 'var(--type-body-sm)',
        color: 'var(--text)',
        ...style,
      }}
    >
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={ariaLabel ?? (typeof label === 'string' ? label : undefined)}
        disabled={disabled}
        onClick={() => onChange?.(!checked)}
        style={{
          all: 'unset',
          boxSizing: 'border-box',
          width: 44,
          height: 26,
          flex: '0 0 auto',
          borderRadius: 'var(--radius-pill)',
          padding: 3,
          cursor: 'inherit',
          background: checked ? 'var(--accent)' : 'var(--border)',
          border: '1px solid ' + (checked ? 'var(--accent)' : 'var(--border-alt)'),
          transition: 'background var(--dur-base) var(--ease-glass)',
        }}
      >
        <span
          style={{
            display: 'block',
            width: 20,
            height: 20,
            borderRadius: '50%',
            background: 'var(--surface)',
            boxShadow: 'var(--shadow-card)',
            transform: checked ? 'translateX(18px)' : 'translateX(0)',
            transition: 'transform var(--dur-base) var(--ease-glass)',
          }}
        />
      </button>
      {label}
    </label>
  )
}

export interface CheckboxProps {
  label?: ReactNode
  checked?: boolean
  onChange?: (next: boolean) => void
  disabled?: boolean
  'aria-label'?: string
  style?: CSSProperties
}

export function Checkbox({ label, checked = false, onChange, disabled = false, style, ...rest }: CheckboxProps) {
  return (
    <label
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--space-3)',
        cursor: disabled ? 'default' : 'pointer',
        opacity: disabled ? 0.5 : 1,
        font: 'var(--type-body-sm)',
        color: 'var(--text)',
        position: 'relative',
        ...style,
      }}
    >
      <span
        aria-hidden
        style={{
          width: 20,
          height: 20,
          flex: '0 0 auto',
          borderRadius: 'var(--radius-sm)',
          display: 'grid',
          placeItems: 'center',
          background: checked ? 'var(--accent)' : 'var(--surface)',
          border: '1px solid ' + (checked ? 'var(--accent)' : 'var(--border)'),
          color: 'var(--text-on-accent)',
          fontSize: 12,
          lineHeight: 1,
          transition: 'var(--transition-control)',
        }}
      >
        {checked ? '✓' : ''}
      </span>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange?.(e.target.checked)}
        style={{ position: 'absolute', opacity: 0, width: 0, height: 0 }}
        {...rest}
      />
      {label}
    </label>
  )
}
