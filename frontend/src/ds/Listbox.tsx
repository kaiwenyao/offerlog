import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import type { ControlSize } from './Button'
import { Eyebrow } from './Card'
import { CONTROL_HEIGHTS, labelStyle, shellStyle } from './fieldShell'

export interface ListboxOption {
  value: string
  label: string
  /** Leading glyph — status emoji, round number, anything short. */
  icon?: ReactNode
  /** Solid color pip drawn before the icon (StatusMeta.dot). */
  dot?: string
  /** Muted trailing copy explaining what picking this means. */
  hint?: string
  disabled?: boolean
  /** Replaces `hint` when disabled — says WHY it can't be picked. */
  disabledReason?: string
}

export interface ListboxGroup {
  label: string
  options: ListboxOption[]
}

export interface ListboxProps {
  label?: ReactNode
  value: string
  onChange: (value: string) => void
  /** Groups render a section header each; pass one unlabeled group for a flat list. */
  groups: ListboxGroup[]
  placeholder?: string
  size?: ControlSize
  invalid?: boolean
  disabled?: boolean
  hint?: ReactNode
  /** Marks the trigger for tests / e2e selectors. */
  triggerClassName?: string
}

const PANEL_MAX_HEIGHT = 320
/** Keep the panel off the very edge of the viewport when it flips or shrinks. */
const VIEWPORT_MARGIN = 8

function flatten(groups: ListboxGroup[]): ListboxOption[] {
  return groups.flatMap((g) => g.options)
}

interface PanelRect {
  left: number
  width: number
  /** Set when the panel hangs below the trigger. */
  top?: number
  /** Set instead of `top` when the panel flips above the trigger. */
  bottom?: number
  maxHeight: number
}

/**
 * Anchor the fixed-position panel to the trigger, flipping above it when the
 * space below is too tight. Fixed + portal is what keeps the panel from being
 * clipped by an ancestor's `overflow: auto` — the dialog body scrolls, so an
 * in-flow popover would be cut off at the modal's edge.
 */
function measure(trigger: HTMLElement): PanelRect {
  const r = trigger.getBoundingClientRect()
  const below = window.innerHeight - r.bottom - VIEWPORT_MARGIN
  const above = r.top - VIEWPORT_MARGIN
  const flip = below < Math.min(PANEL_MAX_HEIGHT, above) && above > below
  return {
    left: r.left,
    width: r.width,
    ...(flip ? { bottom: window.innerHeight - r.top + 2 } : { top: r.bottom + 2 }),
    maxHeight: Math.min(PANEL_MAX_HEIGHT, flip ? above : below),
  }
}

/**
 * Blueprint listbox: a square popover that replaces the OS-drawn `<select>`
 * menu, so a dropdown looks like the rest of the drawing instead of a rounded
 * system-blue sheet. Options can carry a color pip, an icon and a hint, and can
 * be disabled with a stated reason.
 *
 * Not a combobox — there is no text entry, so the trigger is a plain button
 * owning a `role="listbox"` panel via `aria-controls`.
 */
export function Listbox({
  label,
  value,
  onChange,
  groups,
  placeholder = '选择…',
  size = 'md',
  invalid = false,
  disabled = false,
  hint,
  triggerClassName,
}: ListboxProps) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [rect, setRect] = useState<PanelRect | null>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const baseId = useId()

  const options = useMemo(() => flatten(groups), [groups])
  const selectable = useMemo(() => options.filter((o) => !o.disabled), [options])
  const selected = options.find((o) => o.value === value)
  const optionId = (v: string) => `${baseId}-opt-${v}`

  const close = useCallback((refocus: boolean) => {
    setOpen(false)
    setActive(-1)
    setRect(null)
    if (refocus) triggerRef.current?.focus()
  }, [])

  // Opening lands the highlight on the current value (or the first pickable
  // option) so ↑/↓ starts from somewhere meaningful.
  const openPanel = useCallback(() => {
    if (disabled) return
    const start = selectable.findIndex((o) => o.value === value)
    setActive(start >= 0 ? start : 0)
    setOpen(true)
  }, [disabled, selectable, value])

  // Position before paint so the panel never flashes at the wrong spot, and
  // re-measure while it is open: the dialog body can scroll under it.
  useLayoutEffect(() => {
    if (!open || !triggerRef.current) return
    const reposition = () => {
      if (triggerRef.current) setRect(measure(triggerRef.current))
    }
    reposition()
    window.addEventListener('resize', reposition)
    window.addEventListener('scroll', reposition, true)
    return () => {
      window.removeEventListener('resize', reposition)
      window.removeEventListener('scroll', reposition, true)
    }
  }, [open])

  // Keep the highlighted row in view when arrowing past the panel's edge.
  useEffect(() => {
    if (!open || active < 0) return
    const el = panelRef.current?.querySelector<HTMLElement>('[data-active="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [open, active])

  const commit = (opt: ListboxOption | undefined) => {
    if (!opt || opt.disabled) return
    onChange(opt.value)
    close(true)
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        openPanel()
      }
      return
    }
    // The dialog listens for Escape on document (ds/Dialog.tsx); without this
    // one Escape would close the popover AND the modal behind it.
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      close(true)
      return
    }
    if (e.key === 'Tab') {
      close(false)
      return
    }
    const last = selectable.length - 1
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((i) => (i >= last ? 0 : i + 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => (i <= 0 ? last : i - 1))
    } else if (e.key === 'Home') {
      e.preventDefault()
      setActive(0)
    } else if (e.key === 'End') {
      e.preventDefault()
      setActive(last)
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      commit(selectable[active])
    }
  }

  const triggerStyle: CSSProperties = {
    all: 'unset',
    boxSizing: 'border-box',
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--space-2)',
    width: '100%',
    height: CONTROL_HEIGHTS[size],
    padding: '0 10px',
    font: 'var(--type-body-sm)',
    color: selected ? 'var(--text)' : 'var(--text-muted)',
    cursor: disabled ? 'default' : 'pointer',
    ...shellStyle(open, invalid),
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-2)',
        width: '100%',
      }}
    >
      {label && (
        <span style={labelStyle()} id={`${baseId}-label`}>
          {label}
        </span>
      )}
      <div style={{ position: 'relative', opacity: disabled ? 0.5 : 1 }}>
        <button
          ref={triggerRef}
          type="button"
          className={triggerClassName}
          disabled={disabled}
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-controls={open ? `${baseId}-panel` : undefined}
          aria-labelledby={label ? `${baseId}-label` : undefined}
          onClick={() => (open ? close(false) : openPanel())}
          onKeyDown={onKeyDown}
          style={triggerStyle}
        >
          {selected ? <OptionFace opt={selected} /> : <span style={{ flex: 1 }}>{placeholder}</span>}
          <span aria-hidden style={{ color: 'var(--text-muted)', fontSize: 10 }}>
            ▾
          </span>
        </button>

        {open &&
          rect &&
          createPortal(
            <>
              {/* Click-catcher: same pattern the row menu uses, so a click
                  anywhere outside dismisses without a document listener race. */}
              <div style={{ position: 'fixed', inset: 0, zIndex: 80 }} onMouseDown={() => close(false)} />
              <div
                ref={panelRef}
                id={`${baseId}-panel`}
                role="listbox"
                aria-labelledby={label ? `${baseId}-label` : undefined}
                aria-activedescendant={active >= 0 ? optionId(selectable[active]?.value ?? '') : undefined}
                tabIndex={-1}
                onKeyDown={onKeyDown}
                style={{
                  position: 'fixed',
                  zIndex: 81,
                  left: rect.left,
                  width: rect.width,
                  top: rect.top,
                  bottom: rect.bottom,
                  maxHeight: rect.maxHeight,
                  overflowY: 'auto',
                  padding: 'var(--space-2) 0',
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  borderRadius: 0,
                  boxShadow: 'var(--shadow-pop)',
                }}
              >
                {groups.map((group) =>
                  group.options.length === 0 ? null : (
                    <div key={group.label} role="group" aria-label={group.label}>
                      {group.label && (
                        <div style={{ padding: '6px 10px 4px' }}>
                          <Eyebrow>{group.label}</Eyebrow>
                        </div>
                      )}
                      {group.options.map((opt) => {
                        const isActive = !opt.disabled && selectable[active]?.value === opt.value
                        return (
                          <div
                            key={opt.value}
                            id={optionId(opt.value)}
                            role="option"
                            aria-selected={opt.value === value}
                            aria-disabled={opt.disabled || undefined}
                            data-value={opt.value}
                            data-active={isActive || undefined}
                            onMouseDown={(e) => {
                              e.preventDefault()
                              commit(opt)
                            }}
                            onMouseEnter={() => {
                              if (opt.disabled) return
                              const i = selectable.findIndex((s) => s.value === opt.value)
                              if (i >= 0) setActive(i)
                            }}
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: 'var(--space-2)',
                              padding: '7px 10px',
                              font: 'var(--type-body-sm)',
                              color: 'var(--text)',
                              cursor: opt.disabled ? 'default' : 'pointer',
                              opacity: opt.disabled ? 0.45 : 1,
                              background: isActive ? 'var(--accent-soft)' : 'transparent',
                              borderLeft: '2px solid ' + (opt.value === value ? 'var(--accent)' : 'transparent'),
                            }}
                          >
                            <OptionFace opt={opt} />
                          </div>
                        )
                      })}
                    </div>
                  ),
                )}
              </div>
            </>,
            document.body,
          )}
      </div>
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
    </div>
  )
}

/** Pip + icon + label + trailing hint — shared by the trigger and the rows. */
function OptionFace({ opt }: { opt: ListboxOption }) {
  const trailing = opt.disabled ? opt.disabledReason : opt.hint
  return (
    <>
      {opt.dot && (
        <span
          aria-hidden
          style={{
            width: 8,
            height: 8,
            flex: '0 0 auto',
            background: opt.dot,
            display: 'block',
          }}
        />
      )}
      {opt.icon && (
        <span aria-hidden style={{ flex: '0 0 auto', fontSize: 13, lineHeight: 1 }}>
          {opt.icon}
        </span>
      )}
      <span
        style={{
          flex: 1,
          minWidth: 0,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {opt.label}
      </span>
      {trailing && (
        <span
          style={{
            flex: '0 0 auto',
            font: 'var(--type-caption)',
            fontWeight: 400,
            color: 'var(--text-muted)',
          }}
        >
          {trailing}
        </span>
      )}
    </>
  )
}
