import {
  Activity,
  Archive,
  ArrowLeft,
  ArrowRight,
  Calendar,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock,
  Download,
  ExternalLink,
  FileText,
  Folder,
  Kanban,
  LayoutList,
  List,
  LogOut,
  MoreHorizontal,
  Pencil,
  Plus,
  RotateCcw,
  Search,
  Settings,
  Table,
  Trash2,
  Upload,
  X,
  Bell,
  type LucideIcon,
} from 'lucide-react'

/**
 * Icon set for the app. The Glass design system renders Lucide glyphs at
 * strokeWidth 1.75; `lucide-react` is bundled rather than pulled from a CDN so
 * the Docker deployment stays offline-capable.
 */
const GLYPHS = {
  today: Calendar,
  calendar: Calendar,
  database: List,
  list: List,
  analytics: Activity,
  activity: Activity,
  files: Folder,
  folder: Folder,
  file: FileText,
  settings: Settings,
  logout: LogOut,
  search: Search,
  plus: Plus,
  close: X,
  x: X,
  upload: Upload,
  download: Download,
  trash: Trash2,
  archive: Archive,
  restore: RotateCcw,
  external: ExternalLink,
  dots: MoreHorizontal,
  check: Check,
  clock: Clock,
  table: Table,
  board: Kanban,
  rows: LayoutList,
  edit: Pencil,
  back: ArrowLeft,
  forward: ArrowRight,
  prev: ChevronLeft,
  next: ChevronRight,
  bell: Bell,
} satisfies Record<string, LucideIcon>

export type IconName = keyof typeof GLYPHS

export interface IconProps {
  name: IconName
  size?: number
  strokeWidth?: number
  color?: string
  className?: string
  style?: React.CSSProperties
}

export function Icon({ name, size = 18, strokeWidth = 1.75, color = 'currentColor', className, style }: IconProps) {
  const Glyph = GLYPHS[name]
  return (
    <Glyph
      width={size}
      height={size}
      strokeWidth={strokeWidth}
      color={color}
      className={className}
      aria-hidden
      style={{ flex: '0 0 auto', display: 'block', ...style }}
    />
  )
}

/**
 * The OfferLog mark: an empty square drawn in the accent — a registration box,
 * matching the brand lockup in the Industry canvas.
 */
export function SealMark({ size = 15, color = 'var(--accent)' }: { size?: number; color?: string }) {
  return (
    <span
      aria-hidden
      style={{
        width: size,
        height: size,
        flex: '0 0 auto',
        border: '1.5px solid ' + color,
        display: 'block',
      }}
    />
  )
}

/** Deterministic flat plate colour for company monograms across the app. */
const WASH = [
  'var(--accent-200)',
  'var(--neutral-200)',
  'var(--accent-300)',
  'var(--neutral-300)',
  'var(--accent-100)',
  'var(--neutral-100)',
]

export function washFor(seed: string | number): string {
  const n =
    typeof seed === 'number'
      ? Math.abs(seed)
      : [...seed].reduce((acc, ch) => (acc * 31 + ch.charCodeAt(0)) >>> 0, 7)
  return WASH[n % WASH.length]
}

/** Square company monogram plate — used in tables, boards and the drawer head. */
export function CompanyMark({ name, size = 22, seed }: { name: string; size?: number; seed?: string | number }) {
  return (
    <span
      aria-hidden
      style={{
        width: size,
        height: size,
        flex: '0 0 auto',
        background: washFor(seed ?? name),
        fontFamily: 'var(--font-display)',
        fontSize: Math.max(10, Math.round(size * 0.5)),
        fontWeight: 600,
        display: 'grid',
        placeItems: 'center',
        color: 'var(--accent-900)',
      }}
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  )
}
