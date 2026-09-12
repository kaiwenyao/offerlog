import type { SearchItem } from './CommandPalette'

/** One selectable row of the ⌘K palette: a server search hit or a command. */
export type PaletteRow =
  | { kind: 'item'; data: SearchItem }
  | { kind: 'cmd'; data: { key: string } }

/**
 * Flatten the palette's two result sources (search hits, then commands) into
 * one keyboard-navigable list. Order matches the visual list, so the highlight
 * index addresses the same row the user sees.
 */
export function paletteRows(items: SearchItem[], cmds: Array<{ key: string }>): PaletteRow[] {
  return [
    ...items.map((it) => ({ kind: 'item' as const, data: it })),
    ...cmds.map((c) => ({ kind: 'cmd' as const, data: c })),
  ]
}

/**
 * Next highlight index for a palette key press, or null when the key is not a
 * navigation key (Enter/Esc are handled by the caller). Wraps around both ends
 * and clamps to `len - 1` so a stale index can never point past the list after
 * results shrink.
 */
export function moveHighlight(current: number, len: number, key: string): number | null {
  if (len <= 0) return null
  const clamp = (i: number) => Math.min(Math.max(i, 0), len - 1)
  switch (key) {
    case 'ArrowDown':
      return (clamp(current) + 1) % len
    case 'ArrowUp':
      return (clamp(current) - 1 + len) % len
    case 'Home':
      return 0
    case 'End':
      return len - 1
    default:
      return null
  }
}