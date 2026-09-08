// Shared visual shell for every form control (Input / Textarea / Select /
// Listbox). Kept in one place so a custom popover trigger is pixel-identical to
// a native field: square well filled a step below the ground, hairline edge
// that goes accent on focus.
import type { CSSProperties } from 'react'
import type { ControlSize } from './Button'

export const CONTROL_HEIGHTS: Record<ControlSize, string> = {
  sm: 'var(--control-h-sm)',
  md: 'var(--control-h-md)',
  lg: 'var(--control-h-lg)',
}

export function labelStyle(): CSSProperties {
  return {
    fontSize: 12,
    color: 'color-mix(in srgb, var(--text) 70%, transparent)',
  }
}

/** Square well: filled a step below the ground, edge goes accent on focus. */
export function shellStyle(focus: boolean, invalid: boolean): CSSProperties {
  const edge = invalid ? 'var(--danger)' : focus ? 'var(--accent)' : 'var(--border)'
  return {
    background: 'var(--surface-input)',
    borderRadius: 0,
    border: '1px solid ' + edge,
    transition: 'var(--transition-control)',
  }
}

export const FIELD_RESET: CSSProperties = {
  all: 'unset',
  flex: 1,
  minWidth: 0,
  font: 'var(--type-body-sm)',
  color: 'var(--text)',
  caretColor: 'var(--accent)',
}
