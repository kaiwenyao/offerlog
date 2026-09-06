import { useState } from 'react'
import { login } from '../../lib/api'
import type { Me } from '../../lib/types'
import { SealMark } from '../../components/Icon'
import { Button, Input } from '../../ds'
import { ErrorText, Spinner } from '../../components/ui'

export function LoginPage({ onLoggedIn }: { onLoggedIn: (me: Me) => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      onLoggedIn(await login(email, password))
    } catch (error: unknown) {
      setErr(error instanceof Error ? error.message : '登录失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="app-shell">
      <div className="login-wrap">
        <form className="login-card" onSubmit={submit}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <SealMark size={26} />
            <span style={{ fontFamily: 'var(--font-display)', fontWeight: 500, fontSize: 20, letterSpacing: '-.03em' }}>
              OfferLog
            </span>
          </div>

          <h1 style={{ font: 'var(--type-h3)', letterSpacing: 'var(--tracking-display)', margin: '24px 0 6px' }}>
            登录你的求职档案
          </h1>
          <p style={{ margin: '0 0 24px', font: 'var(--type-body-sm)', color: 'var(--text-muted)' }}>
            收藏、投递、面试、Offer 的完整轨迹，只保存在你自己的实例里。
          </p>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <Input
              label="邮箱"
              type="email"
              autoComplete="username"
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
            <Input
              label="密码"
              type="password"
              autoComplete="current-password"
              placeholder="••••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            {err && <ErrorText>{err}</ErrorText>}
            <Button type="submit" variant="primary" size="md" fullWidth disabled={busy}>
              {busy ? <Spinner size={16} /> : '登录'}
            </Button>
          </div>

          <div style={{ marginTop: 18, fontSize: 13, color: 'var(--text-muted)' }}>会话有效期 14 天</div>
        </form>
      </div>
    </div>
  )
}
