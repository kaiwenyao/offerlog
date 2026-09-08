import { useState, type CSSProperties, type ReactNode } from 'react'
import type { ControlSize } from './Button'
import { CONTROL_HEIGHTS as HEIGHTS, FIELD_RESET, labelStyle, shellStyle } from './fieldShell'

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
          padding: '0 10px',
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
      <span style={{ display: 'flex', padding: '8px 10px', ...shellStyle(focus, invalid) }}>
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
            padding: '0 var(--space-8) 0 10px',
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
            right: 10,
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
          width: 42,
          height: 22,
          flex: '0 0 auto',
          borderRadius: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: checked ? 'flex-end' : 'flex-start',
          cursor: 'inherit',
          background: checked ? 'var(--accent-100)' : 'transparent',
          border: '1px solid ' + (checked ? 'var(--accent)' : 'var(--border)'),
          transition: 'background var(--dur-base) var(--ease-glass)',
        }}
      >
        <span
          style={{
            display: 'block',
            width: 16,
            height: 16,
            margin: '0 2px',
            background: checked ? 'var(--accent)' : 'var(--neutral-400)',
            transition: 'background var(--dur-base) var(--ease-glass)',
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
          width: 18,
          height: 18,
          flex: '0 0 auto',
          borderRadius: 0,
          display: 'grid',
          placeItems: 'center',
          background: checked ? 'var(--accent)' : 'transparent',
          border: '1px solid ' + (checked ? 'var(--accent)' : 'var(--neutral-500)'),
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
