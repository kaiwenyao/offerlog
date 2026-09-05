import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useState } from 'react'
import { logout } from '../lib/api'

const NAV = [
  { to: '/', label: '今日待办', icon: '✅', end: true },
  { to: '/database', label: '求职数据库', icon: '🗃️', end: false },
  { to: '/analytics', label: '统计分析', icon: '📊', end: false },
  { to: '/files', label: '文件库', icon: '📎', end: false },
  { to: '/settings', label: '设置', icon: '⚙️', end: false },
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
          <span aria-hidden>{n.icon}</span> {n.label}
        </NavLink>
      ))}
    </nav>
  )
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark" aria-hidden>◈</span>
          <span className="display brand-name">OfferLogs</span>
        </div>
        {navList}
        <div className="sidebar-foot">
          <button className="btn btn-ghost" style={{ width: '100%' }} onClick={doLogout}>
            退出登录
          </button>
        </div>
      </aside>
      <header className="topbar">
        <button className="btn btn-ghost menu-btn" aria-label="打开菜单" onClick={() => setMenuOpen((v) => !v)}>
          ☰
        </button>
        <span className="topbar-title display">求职工作台</span>
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
            <span aria-hidden className="bn-icon">{n.icon}</span>
            <span className="bn-label">{n.label.replace('求职', '').replace('数据', '岗位')}</span>
          </NavLink>
        ))}
      </nav>
    </div>
  )
}
