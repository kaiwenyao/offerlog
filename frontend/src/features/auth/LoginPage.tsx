import { useState } from 'react'
import { login } from '../../lib/api'
import type { Me } from '../../lib/types'
import { SealMark } from '../../components/Icon'

export function LoginPage({ onLoggedIn }: { onLoggedIn: (m: Me) => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      const me = await login(email, password)
      onLoggedIn(me)
    } catch (e) {
      setErr(e instanceof Error ? e.message : '登录失败')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="login-wrap">
      <form className="card login-card" onSubmit={submit}>
        <div className="login-brand">
          <SealMark size={40} />
          <div>
            <h1 className="display login-title">OfferLogs</h1>
            <p className="login-sub small">个人求职追踪工作台</p>
          </div>
        </div>
        <label className="lbl" htmlFor="email">邮箱</label>
        <input
          id="email"
          className="input"
          type="email"
          autoComplete="username"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
        />
        <label className="lbl" htmlFor="pw">密码</label>
        <input
          id="pw"
          className="input"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        {err && <p role="alert" className="err">{err}</p>}
        <button className="btn btn-primary" style={{ width: '100%', marginTop: 14 }} disabled={busy}>
          {busy ? <span className="spinner" /> : '登录'}
        </button>
      </form>
    </div>
  )
}
