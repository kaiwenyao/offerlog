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
 * Query that isolates one application row in the database-page search box.
 * The palette's application label is「公司 · 岗位」; the search takes
 * whitespace-separated terms ANDed together (每个词都要命中)， so feeding
 * 公司 + 岗位 as two terms lands on exactly that row（同名岗位自然一起列出）。
 */
export function applicationSearchQuery(label: string): string {
  return label
    .split(' · ')
    .map((part) => part.trim())
    .filter((part) => part !== '' && !/^[·\s]+$/.test(part))
    .join(' ')
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