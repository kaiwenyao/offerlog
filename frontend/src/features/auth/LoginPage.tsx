import { useEffect, useState } from 'react'
import { fetchAuthConfig, login, register } from '../../lib/api'
import type { Me } from '../../lib/types'
import { SealMark } from '../../components/Icon'
import { Button, Input, Tabs } from '../../ds'
import { ErrorText, Spinner } from '../../components/ui'

type Mode = 'login' | 'register'

export function LoginPage({ onLoggedIn }: { onLoggedIn: (me: Me) => void }) {
  const [mode, setMode] = useState<Mode>('login')
  const [regOpen, setRegOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    // Public endpoint; a failure just means "don't offer signup".
    fetchAuthConfig()
      .then((c) => setRegOpen(!!c.registration_open))
      .catch(() => setRegOpen(false))
  }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      const me = mode === 'login'
        ? await login(email, password)
        : await register(email, password, displayName || undefined)
      onLoggedIn(me)
    } catch (error: unknown) {
      setErr(error instanceof Error ? error.message : mode === 'login' ? '登录失败' : '注册失败')
    } finally {
      setBusy(false)
    }
  }

  const switchMode = (v: string) => {
    setMode(v as Mode)
    setErr('')
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
            {mode === 'login' ? '登录你的求职档案' : '创建你的求职档案'}
          </h1>
          <p style={{ margin: '0 0 24px', font: 'var(--type-body-sm)', color: 'var(--text-muted)' }}>
            {mode === 'login'
              ? '收藏、投递、面试、Offer 的完整轨迹，只保存在你自己的实例里。'
              : '注册后数据完全独立，仅属于你这个账号。'}
          </p>

          {regOpen && (
            <div style={{ marginBottom: 18 }}>
              <Tabs
                ariaLabel="登录或注册"
                items={[
                  { value: 'login', label: '登录' },
                  { value: 'register', label: '注册' },
                ]}
                value={mode}
                onChange={switchMode}
                fullWidth
              />
            </div>
          )}

          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <Input
              label="邮箱"
              type="email"
              autoComplete={mode === 'login' ? 'username' : 'email'}
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
            <Input
              label="密码"
              type="password"
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              placeholder={mode === 'login' ? '••••••••••' : '至少 8 位'}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              minLength={mode === 'register' ? 8 : undefined}
              required
            />
            {mode === 'register' && (
              <Input
                label="显示名称（可选）"
                type="text"
                autoComplete="nickname"
                placeholder="怎么称呼你"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                maxLength={80}
              />
            )}
            {err && <ErrorText>{err}</ErrorText>}
            <Button type="submit" variant="primary" size="md" fullWidth disabled={busy}>
              {busy ? <Spinner size={16} /> : mode === 'login' ? '登录' : '注册并进入'}
            </Button>
          </div>

          <div style={{ marginTop: 18, fontSize: 13, color: 'var(--text-muted)' }}>
            {mode === 'login' ? '会话有效期 14 天' : '密码以 Argon2id 哈希存储，不会明文保存。'}
          </div>
        </form>
      </div>
    </div>
  )
}
