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
 * The OfferLog mark: a flat accent square with a subtle inner highlight.
 */
export function SealMark({ size = 26 }: { size?: number }) {
  return (
    <span
      aria-hidden
      style={{
        width: size,
        height: size,
        flex: '0 0 auto',
        borderRadius: Math.round(size * 0.35),
        background: 'linear-gradient(135deg, var(--accent), var(--accent-hover))',
        display: 'block',
      }}
    />
  )
}

/** Deterministic neutral wash used for company avatars across the app. */
const WASH = [
  'linear-gradient(135deg, oklch(95% 0.03 145), oklch(91% 0.04 145))',
  'linear-gradient(135deg, oklch(95% 0.012 250), oklch(91% 0.014 250))',
  'linear-gradient(135deg, oklch(94% 0.03 155), oklch(91% 0.02 240))',
  'linear-gradient(135deg, oklch(95% 0.012 240), oklch(91% 0.03 145))',
  'linear-gradient(135deg, oklch(95% 0.025 145), oklch(92% 0.014 250))',
  'linear-gradient(135deg, oklch(95% 0.014 250), oklch(91% 0.03 155))',
]

export function washFor(seed: string | number): string {
  const n =
    typeof seed === 'number'
      ? Math.abs(seed)
      : [...seed].reduce((acc, ch) => (acc * 31 + ch.charCodeAt(0)) >>> 0, 7)
  return WASH[n % WASH.length]
}

/** Rounded company monogram tile — used in tables, boards and the drawer head. */
export function CompanyMark({ name, size = 22, seed }: { name: string; size?: number; seed?: string | number }) {
  return (
    <span
      aria-hidden
      style={{
        width: size,
        height: size,
        flex: '0 0 auto',
        borderRadius: Math.round(size * 0.32),
        background: washFor(seed ?? name),
        border: '1px solid var(--border-alt)',
        fontSize: Math.max(10, Math.round(size * 0.5)),
        fontWeight: 500,
        display: 'grid',
        placeItems: 'center',
        color: 'var(--text)',
      }}
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  )
}
