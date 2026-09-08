import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, logout } from '../lib/api'
import type { FileItem, HomeSummary, Me } from '../lib/types'
import { Icon, SealMark, type IconName } from '../components/Icon'
import { Button, Eyebrow } from '../ds'
import { Dot } from '../components/ui'
import { NotificationsBell } from '../features/notifications/Bell'

interface NavEntry {
  to: string
  label: string
  short: string
  icon: IconName
  end?: boolean
  /** Which counter from the sidebar summary to show, if any. */
  count?: 'open' | 'apps' | 'files'
}

const NAV: NavEntry[] = [
  { to: '/', label: '今日待办', short: '今日', icon: 'today', end: true, count: 'open' },
  { to: '/database', label: '求职数据库', short: '岗位', icon: 'database', count: 'apps' },
  { to: '/calendar', label: '面试日历', short: '日历', icon: 'calendar' },
  { to: '/analytics', label: '统计分析', short: '统计', icon: 'analytics' },
  { to: '/files', label: '文件库', short: '文件', icon: 'files', count: 'files' },
  { to: '/settings', label: '设置', short: '设置', icon: 'settings' },
]

/** Saved views mirror the design's 已保存视图 list; each seeds the database page. */
const SAVED_VIEWS: Array<{ label: string; dot: string; view: number; layout: string }> = [
  { label: '本周面试', dot: 'var(--accent)', view: -3, layout: 'board' },
  { label: '待跟进', dot: 'var(--warning)', view: -1, layout: 'list' },
  { label: '已归档', dot: 'var(--neutral)', view: -5, layout: 'table' },
]

/** Extra sidebar entries below saved views (reachability for full pages). */
const SIDEBAR_LINKS: Array<{ to: string; label: string; icon: IconName; countKey?: 'open' }> = [
  { to: '/notifications', label: '通知中心', icon: 'bell' },
]

const PAGE_META: Record<string, { eyebrow: string; title: string }> = {
  '/': { eyebrow: 'TODAY', title: '今日待办' },
  '/database': { eyebrow: 'DATABASE', title: '求职数据库' },
  '/calendar': { eyebrow: 'CALENDAR · 跨岗位日程', title: '面试日历' },
  '/analytics': { eyebrow: 'ANALYTICS', title: '统计分析' },
  '/files': { eyebrow: 'FILES · 私有存储', title: '文件库' },
  '/settings': { eyebrow: 'SETTINGS', title: '设置' },
}

function pageMeta(pathname: string) {
  if (pathname.startsWith('/apps/')) return { eyebrow: 'DATABASE · 岗位详情', title: '岗位详情' }
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
  return {
    apps: home.data?.total ?? home.data?.active ?? 0,
    files: (files.data?.items ?? []).length,
    open: home.data?.todos.open ?? 0,
  }
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

  const countFor = (entry: NavEntry) => (entry.count ? counts[entry.count] : undefined)

  return (
    <div className="app-shell">
      <div className="app-panel">
        {menuOpen && <div className="sidebar-sheet-backdrop" onClick={() => setMenuOpen(false)} />}

        <aside className={'sidebar' + (menuOpen ? ' open' : '')}>
          <div className="sidebar-brand">
            <SealMark size={24} />
            <span className="brand-name">OfferLog</span>
          </div>

          <div className="sidebar-scroll">
            <nav className="nav">
              {NAV.map((n) => (
                <NavLink
                  key={n.to}
                  to={n.to}
                  end={n.end}
                  className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}
                >
                  <Icon name={n.icon} size={16} />
                  <span className="grow">{n.label}</span>
                  {countFor(n) !== undefined && <span className="nav-count">{countFor(n)}</span>}
                </NavLink>
              ))}
            </nav>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <Eyebrow style={{ padding: '0 10px' }}>已保存视图</Eyebrow>
              {SAVED_VIEWS.map((v) => (
                <button
                  key={v.label}
                  type="button"
                  className="saved-view"
                  onClick={() => nav(`/database?view=${v.view}&layout=${v.layout}`)}
                >
                  <Dot color={v.dot} size={6} />
                  <span className="grow ellipsis">{v.label}</span>
                </button>
              ))}
            </div>
            <nav className="nav" aria-label="更多">
              {SIDEBAR_LINKS.map((l) => (
                <NavLink
                  key={l.to}
                  to={l.to}
                  className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}
                >
                  <Icon name={l.icon} size={16} />
                  <span className="grow">{l.label}</span>
                </NavLink>
              ))}
            </nav>
          </div>

          <div className="sidebar-foot">
            <div className="user-card">
              <span className="user-avatar" aria-hidden />
              <span className="grow" style={{ minWidth: 0 }}>
                <span className="ellipsis" style={{ display: 'block', fontSize: 13, fontWeight: 500 }}>
                  {me?.display_name || me?.email || '未登录'}
                </span>
                <span className="ellipsis" style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}>
                  {me?.timezone || 'UTC'}
                </span>
              </span>
            </div>
            <Button variant="ghost" size="sm" fullWidth onClick={doLogout} iconLeft={<Icon name="logout" size={15} />}>
              退出登录
            </Button>
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
            <div style={{ minWidth: 0 }}>
              <Eyebrow>{meta.eyebrow}</Eyebrow>
              <div className="topbar-title">{meta.title}</div>
            </div>
            <div style={{ flex: 1, display: 'flex', justifyContent: 'center', minWidth: 0 }}>
              <form className="topbar-search" onSubmit={submitSearch} role="search">
                <Icon name="search" size={15} color="var(--text-muted)" />
                <input
                  id="global-search"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="搜公司、岗位、备注"
                  aria-label="搜索"
                />
                <span className="kbd" aria-hidden>
                  ⌘K
                </span>
              </form>
            </div>
            <NotificationsBell />
          </header>

          <main className="content">
            <Outlet />
          </main>

          <nav className="bottom-nav">
            {NAV.map((n) => (
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
