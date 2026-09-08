import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, logout } from '../lib/api'
import type { FileItem, HomeSummary, Me } from '../lib/types'
import { Icon, SealMark, type IconName } from '../components/Icon'
import { BlueprintCorners, Button } from '../ds'
import { NotificationsBell, useUnreadCount } from '../features/notifications/Bell'

interface NavEntry {
  to: string
  label: string
  short: string
  icon: IconName
  end?: boolean
  /** Which counter to show at the row's trailing edge, if any. */
  count?: 'open' | 'apps' | 'files' | 'unread'
}

/** Seven numbered stations, matching the canvas's 01–07 sidebar index. */
const NAV: NavEntry[] = [
  { to: '/', label: '今日待办', short: '今日', icon: 'today', end: true, count: 'open' },
  { to: '/database', label: '求职数据库', short: '岗位', icon: 'database', count: 'apps' },
  { to: '/calendar', label: '面试日历', short: '日历', icon: 'calendar' },
  { to: '/analytics', label: '统计分析', short: '统计', icon: 'analytics' },
  { to: '/files', label: '文件库', short: '文件', icon: 'files', count: 'files' },
  { to: '/notifications', label: '通知中心', short: '通知', icon: 'bell', count: 'unread' },
  { to: '/settings', label: '设置', short: '设置', icon: 'settings' },
]

/** The five entries that fit the mobile bottom bar. */
const BOTTOM_NAV = NAV.filter((n) => n.to !== '/notifications' && n.to !== '/analytics')

/** Saved views mirror the canvas's 已保存视图 list; each seeds the database page. */
const SAVED_VIEWS: Array<{ label: string; dot: string; view: number; layout: string }> = [
  { label: '本周面试', dot: 'var(--accent)', view: -3, layout: 'board' },
  { label: '待跟进', dot: 'var(--warning)', view: -1, layout: 'list' },
  { label: '已归档', dot: 'var(--neutral-400)', view: -5, layout: 'table' },
]

const PAGE_META: Record<string, { eyebrow: string; title: string }> = {
  '/': { eyebrow: 'TODAY / 今日工位', title: '今日待办' },
  '/database': { eyebrow: 'DATABASE / 全量岗位', title: '求职数据库' },
  '/calendar': { eyebrow: 'CALENDAR / 跨岗位日程', title: '面试日历' },
  '/analytics': { eyebrow: 'ANALYTICS / 全量聚合', title: '统计分析' },
  '/files': { eyebrow: 'FILES / 私有存储', title: '文件库' },
  '/notifications': { eyebrow: 'INBOX / 站内通知', title: '通知中心' },
  '/settings': { eyebrow: 'SETTINGS / 账号与字段', title: '设置' },
}

function pageMeta(pathname: string) {
  if (pathname.startsWith('/apps/')) return { eyebrow: 'DATABASE / 岗位详情', title: '岗位详情' }
  if (pathname.startsWith('/database')) return PAGE_META['/database']
  return PAGE_META[pathname] ?? PAGE_META['/']
}

/** Counts shown beside the nav entries — cheap queries the pages already cache. */
function useSidebarCounts() {
  // Server-side aggregates (full data set) — the badge next to 今日待办 is the
  // unified open-action count, and the 岗位 badge is the real total; neither is
  // derived from a 200-row page.
  const home = useQuery({
    queryKey: ['home', 'summary', { limit: 1 }],
    queryFn: () => api.get<HomeSummary>('/api/v1/home/summary?limit=1'),
    staleTime: 30_000,
  })
  const files = useQuery({
    queryKey: ['files'],
    queryFn: () => api.get<{ items: FileItem[] }>('/api/v1/files'),
    staleTime: 30_000,
  })
  const unread = useUnreadCount()
  return {
    apps: home.data?.total ?? home.data?.active ?? 0,
    files: (files.data?.items ?? []).length,
    open: home.data?.todos.open ?? 0,
    unread,
  }
}

/** First letter of the display name, used for the square monogram plate. */
function initialOf(me: Me | null): string {
  const source = me?.display_name || me?.email || '?'
  return source.slice(0, 2).toUpperCase()
}

export function AppLayout({ me }: { me: Me | null }) {
  const nav = useNavigate()
  const loc = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)
  const [search, setSearch] = useState('')
  const counts = useSidebarCounts()
  const meta = pageMeta(loc.pathname)

  // Close the mobile sheet whenever the route changes.
  useEffect(() => setMenuOpen(false), [loc.pathname])

  // ⌘K / Ctrl+K focuses the header search, as the design advertises.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        document.getElementById('global-search')?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const doLogout = async () => {
    await logout()
    nav('/')
    window.location.reload()
  }

  const submitSearch = (e: React.FormEvent) => {
    e.preventDefault()
    const q = search.trim()
    nav(q ? `/database?q=${encodeURIComponent(q)}` : '/database')
  }

  const countFor = (entry: NavEntry) => {
    if (!entry.count) return undefined
    const n = counts[entry.count]
    return n > 0 ? n : undefined
  }

  return (
    <div className="app-shell">
      <div className="app-panel">
        {menuOpen && <div className="sidebar-sheet-backdrop" onClick={() => setMenuOpen(false)} />}

        <aside className={'sidebar' + (menuOpen ? ' open' : '')}>
          <div className="sidebar-brand">
            <SealMark size={15} />
            <span className="brand-name">OFFERLOG</span>
          </div>

          <div className="sidebar-scroll">
            <nav className="nav">
              {NAV.map((n, i) => (
                <NavLink
                  key={n.to}
                  to={n.to}
                  end={n.end}
                  className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}
                >
                  <span className="nav-index" aria-hidden>
                    {String(i + 1).padStart(2, '0')}
                  </span>
                  <span className="grow">{n.label}</span>
                  {countFor(n) !== undefined && <span className="nav-count">{countFor(n)}</span>}
                </NavLink>
              ))}
            </nav>

            <div>
              <div className="nav-group-label">已保存视图</div>
              {SAVED_VIEWS.map((v) => (
                <button
                  key={v.label}
                  type="button"
                  className="saved-view"
                  onClick={() => nav(`/database?view=${v.view}&layout=${v.layout}`)}
                >
                  <span aria-hidden style={{ width: 7, height: 7, background: v.dot, flex: '0 0 auto' }} />
                  <span className="grow ellipsis">{v.label}</span>
                </button>
              ))}
            </div>
          </div>

          <div className="sidebar-foot">
            <div className="user-card blueprint">
              <BlueprintCorners />
              <span className="user-avatar" aria-hidden>
                {initialOf(me)}
              </span>
              <span className="grow" style={{ minWidth: 0 }}>
                <span className="ellipsis" style={{ display: 'block', fontSize: 13, fontWeight: 500 }}>
                  {me?.display_name || me?.email || '未登录'}
                </span>
                <span className="ellipsis" style={{ display: 'block', fontSize: 11, color: 'var(--text-muted)' }}>
                  {me?.timezone || 'UTC'}
                </span>
              </span>
              <button
                type="button"
                onClick={doLogout}
                title="退出登录"
                style={{
                  all: 'unset',
                  cursor: 'pointer',
                  fontSize: 11,
                  color: 'var(--neutral-600)',
                  flex: '0 0 auto',
                }}
              >
                退出
              </button>
            </div>
          </div>
        </aside>

        <div className="content-col">
          <header className="topbar">
            <button
              type="button"
              className="menu-btn"
              aria-label="打开菜单"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((v) => !v)}
            >
              <Icon name="rows" size={18} />
            </button>
            <div style={{ minWidth: 0, flex: '0 0 auto', width: 170 }}>
              <div
                style={{
                  fontSize: 10,
                  letterSpacing: 'var(--tracking-micro)',
                  color: 'var(--accent-700)',
                }}
              >
                {meta.eyebrow}
              </div>
              <div className="topbar-title ellipsis">{meta.title}</div>
            </div>
            <form className="topbar-search" onSubmit={submitSearch} role="search">
              <Icon name="search" size={15} color="var(--neutral-600)" />
              <input
                id="global-search"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="搜岗位、公司、备注…"
                aria-label="搜索"
              />
              <span className="kbd" aria-hidden>
                ⌘K
              </span>
            </form>
            <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 8 }}>
              <NotificationsBell />
              <Button variant="primary" size="md" onClick={() => nav('/database?new=1')}>
                ＋ 记一个岗位
              </Button>
            </div>
          </header>

          <main className="content">
            <Outlet />
          </main>

          <nav className="bottom-nav">
            {BOTTOM_NAV.map((n) => (
              <NavLink
                key={n.to}
                to={n.to}
                end={n.end}
                className={({ isActive }) => 'bn-item' + (isActive ? ' active' : '')}
              >
                <Icon name={n.icon} size={18} />
                <span>{n.short}</span>
              </NavLink>
            ))}
          </nav>
        </div>
      </div>
    </div>
  )
}
