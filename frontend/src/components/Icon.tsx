// Stroke icon set (20×20 grid, 1.6px round strokes) — one visual voice for
// navigation, actions and empty states; replaces the mixed emoji vocabulary.
import React from 'react'

export type IconName =
  | 'today'
  | 'database'
  | 'analytics'
  | 'files'
  | 'settings'
  | 'logout'
  | 'search'
  | 'plus'
  | 'close'
  | 'calendar'
  | 'upload'
  | 'trash'
  | 'archive'
  | 'restore'
  | 'external'
  | 'dots'
  | 'check'
  | 'clock'
  | 'table'
  | 'board'
  | 'list'
  | 'edit'

const GLYPHS: Record<IconName, React.ReactNode> = {
  today: (
    <>
      <rect x="3" y="4.5" width="14" height="12.5" rx="3" />
      <path d="M7 2.8v3.2M13 2.8v3.2M3 9.2h14" />
      <path d="M7.2 12.6l2 2.1 3.8-4.2" />
    </>
  ),
  database: (
    <>
      <ellipse cx="10" cy="5.4" rx="6.5" ry="2.6" />
      <path d="M3.5 5.4v9.2c0 1.45 2.9 2.6 6.5 2.6s6.5-1.15 6.5-2.6V5.4" />
      <path d="M3.5 10c0 1.45 2.9 2.6 6.5 2.6s6.5-1.15 6.5-2.6" />
    </>
  ),
  analytics: <path d="M4 16.5v-4.6M10 16.5V6M16 16.5v-7.8" />,
  files: (
    <path d="M3.5 6.4a2 2 0 012-2h2.8l1.9 2.1h6.3a2 2 0 012 2v6.9a2 2 0 01-2 2h-11a2 2 0 01-2-2z" />
  ),
  settings: (
    <>
      <path d="M3.8 6.6h12.4M3.8 13.4h12.4" />
      <circle cx="8.2" cy="6.6" r="2.1" />
      <circle cx="12.6" cy="13.4" r="2.1" />
    </>
  ),
  logout: (
    <>
      <path d="M8.5 3.5H6a2 2 0 00-2 2v9a2 2 0 002 2h2.5" />
      <path d="M12.5 6.8l3.4 3.2-3.4 3.2M15.6 10H8.8" />
    </>
  ),
  search: (
    <>
      <circle cx="9.2" cy="9.2" r="5.4" />
      <path d="M13.3 13.3L17 17" />
    </>
  ),
  plus: <path d="M10 4.2v11.6M4.2 10h11.6" />,
  close: <path d="M5.6 5.6l8.8 8.8M14.4 5.6l-8.8 8.8" />,
  calendar: (
    <>
      <rect x="3" y="4.8" width="14" height="12.4" rx="2.6" />
      <path d="M7 3v3.4M13 3v3.4M3 9.4h14" />
    </>
  ),
  upload: (
    <>
      <path d="M10 13.2V4.4M6.6 7.8L10 4.4l3.4 3.4" />
      <path d="M4 16.6h12" />
    </>
  ),
  trash: (
    <path d="M4.2 6h11.6M8 6V4.6a1.2 1.2 0 011.2-1.2h1.6A1.2 1.2 0 0112 4.6V6M6.4 6l.7 9.7a1.5 1.5 0 001.5 1.4h2.8a1.5 1.5 0 001.5-1.4L13.6 6M8.4 9.2v4.6M11.6 9.2v4.6" />
  ),
  archive: (
    <>
      <rect x="3.2" y="3.8" width="13.6" height="4.2" rx="1.3" />
      <path d="M4.8 8v7.2a1.6 1.6 0 001.6 1.6h7.2a1.6 1.6 0 001.6-1.6V8M8.2 11.4h3.6" />
    </>
  ),
  restore: (
    <>
      <path d="M4.6 4.8v4h4" />
      <path d="M5 8.8a5.6 5.6 0 11-.4 3.4" />
    </>
  ),
  external: (
    <>
      <path d="M8.6 4.6H5.6a2 2 0 00-2 2v7.8a2 2 0 002 2h7.8a2 2 0 002-2v-3" />
      <path d="M11.6 4.4h4.2v4.2M15.6 4.6L9.4 10.8" />
    </>
  ),
  dots: (
    <>
      <circle cx="4.6" cy="10" r="1.15" fill="currentColor" stroke="none" />
      <circle cx="10" cy="10" r="1.15" fill="currentColor" stroke="none" />
      <circle cx="15.4" cy="10" r="1.15" fill="currentColor" stroke="none" />
    </>
  ),
  check: <path d="M4.8 10.6l3.4 3.4 7-7.6" />,
  clock: (
    <>
      <circle cx="10" cy="10" r="6.6" />
      <path d="M10 6.4V10l2.6 1.6" />
    </>
  ),
  table: (
    <>
      <rect x="3.4" y="4" width="13.2" height="12" rx="2" />
      <path d="M3.4 8.6h13.2M8.6 8.6V16" />
    </>
  ),
  board: (
    <>
      <rect x="3.4" y="4" width="5.4" height="12" rx="1.6" />
      <rect x="11.2" y="4" width="5.4" height="8" rx="1.6" />
    </>
  ),
  list: (
    <>
      <path d="M7.2 5.4h9.4M7.2 10h9.4M7.2 14.6h9.4" />
      <circle cx="4.2" cy="5.4" r="1" fill="currentColor" stroke="none" />
      <circle cx="4.2" cy="10" r="1" fill="currentColor" stroke="none" />
      <circle cx="4.2" cy="14.6" r="1" fill="currentColor" stroke="none" />
    </>
  ),
  edit: (
    <>
      <path d="M4.5 15.5l.6-3L13.4 4.2a1.6 1.6 0 012.3 0l.1.1a1.6 1.6 0 010 2.3l-8.3 8.3z" />
      <path d="M12.4 5.9l1.7 1.7" />
    </>
  ),
}

export function Icon({ name, size = 16, className }: { name: IconName; size?: number; className?: string }) {
  return (
    <svg
      className={'icon' + (className ? ' ' + className : '')}
      width={size}
      height={size}
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {GLYPHS[name]}
    </svg>
  )
}

// Brand seal: a square cinnabar stamp reading 录 (录取) — the product's
// signature mark, used in the sidebar and login.
export function SealMark({ size = 26 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 28 28"
      aria-hidden="true"
      focusable="false"
      className="seal-mark"
    >
      <rect x="1" y="1" width="26" height="26" rx="6.5" fill="var(--seal)" />
      <rect x="4" y="4" width="20" height="20" rx="4.5" fill="none" stroke="rgb(255 255 255 / 0.55)" strokeWidth="1.2" />
      <text
        x="14"
        y="14.5"
        textAnchor="middle"
        dominantBaseline="central"
        fill="#fff"
        fontSize="14.5"
        fontWeight="700"
        fontFamily="'Noto Serif SC','Songti SC',serif"
      >
        录
      </text>
    </svg>
  )
}
