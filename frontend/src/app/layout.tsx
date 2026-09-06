import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useState } from 'react'
import { logout } from '../lib/api'
import { Icon, SealMark, type IconName } from '../components/Icon'

const NAV: Array<{ to: string; label: string; icon: IconName; end?: boolean }> = [
  { to: '/', label: '今日待办', icon: 'today', end: true },
  { to: '/database', label: '求职数据库', icon: 'database' },
  { to: '/analytics', label: '统计分析', icon: 'analytics' },
  { to: '/files', label: '文件库', icon: 'files' },
  { to: '/settings', label: '设置', icon: 'settings' },
]

export function AppLayout() {
  const nav = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const doLogout = async () => {
    await logout()
    nav('/')
    window.location.reload()
  }
  const navList = (
    <nav className="nav">
      {NAV.map((n) => (
        <NavLink
          key={n.to}
          to={n.to}
          end={n.end}
          className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}
          onClick={() => setMenuOpen(false)}
        >
          <Icon name={n.icon} size={17} />
          <span>{n.label}</span>
        </NavLink>
      ))}
    </nav>
  )
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <SealMark size={28} />
          <span className="display brand-name">OfferLog</span>
        </div>
        {navList}
        <div className="sidebar-foot">
          <button className="btn btn-ghost btn-small" style={{ width: '100%' }} onClick={doLogout}>
            <Icon name="logout" size={15} />
            <span>退出登录</span>
          </button>
        </div>
      </aside>
      <header className="topbar">
        <button className="menu-btn" aria-label="打开菜单" onClick={() => setMenuOpen((v) => !v)}>
          ☰
        </button>
        <span className="topbar-title display">OfferLog</span>
      </header>
      {menuOpen && (
        <div className="mobile-menu" onClick={() => setMenuOpen(false)}>
          {navList}
        </div>
      )}
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
            <span aria-hidden className="bn-icon">
              <Icon name={n.icon} size={19} />
            </span>
            <span className="bn-label">{n.label.replace('求职', '').replace('数据', '岗位')}</span>
          </NavLink>
        ))}
      </nav>
    </div>
  )
}
