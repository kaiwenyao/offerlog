import { useEffect, useState } from 'react'
import { fetchAuthConfig, login, register } from '../../lib/api'
import type { Me } from '../../lib/types'
import { SealMark } from '../../components/Icon'
import { BlueprintCorners, Button, Input, Tabs } from '../../ds'
import { ErrorText, Spinner } from '../../components/ui'

type Mode = 'login' | 'register'

/** Static plate copy — the production-line framing from the design canvas. */
const PLATE_STATS: Array<[string, string]> = [
  ['收藏 → Offer', '六道工序'],
  ['节点留痕', '日期 / 截图'],
  ['自建实例', '数据自持'],
]

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
    <div className="login-shell">
      <div className="login-plate">
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            fontFamily: 'var(--font-display)',
            fontWeight: 600,
            fontSize: 19,
            letterSpacing: '.04em',
          }}
        >
          <SealMark size={18} color="#f2f2f3" />
          OFFERLOG
        </div>

        <div style={{ marginTop: 'auto', maxWidth: 420 }}>
          <div style={{ fontSize: 11, letterSpacing: '.18em', opacity: 0.7, marginBottom: 14 }}>
            求职进程记录系统 / v2
          </div>
          <div className="login-plate-headline">
            把一次求职
            <br />
            当成一条产线来管
          </div>
          <p style={{ margin: '18px 0 0', fontSize: 15, lineHeight: 1.6, opacity: 0.82 }}>
            收藏、投递、笔试、面试、Offer —— 每个岗位一条工序线，节点有日期、有截图、有下一步。
          </p>
          <div className="login-stats">
            {PLATE_STATS.map(([value, label]) => (
              <div key={label}>
                <div style={{ fontFamily: 'var(--font-display)', fontWeight: 600, fontSize: 22 }}>{value}</div>
                <div style={{ fontSize: 11, letterSpacing: '.1em', opacity: 0.7 }}>{label}</div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="login-wrap">
        <form className="login-card blueprint" onSubmit={submit}>
          <BlueprintCorners />

          {regOpen ? (
            <div style={{ marginBottom: 26 }}>
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
          ) : (
            <h1 style={{ font: 'var(--type-h3)', margin: '0 0 22px' }}>登录你的求职档案</h1>
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
            <Button type="submit" variant="primary" size="lg" fullWidth disabled={busy}>
              {busy ? <Spinner size={16} /> : mode === 'login' ? '进入工作台' : '注册并进入'}
            </Button>
          </div>

          <div style={{ marginTop: 18, fontSize: 12, color: 'var(--text-muted)' }}>
            {mode === 'login'
              ? '会话有效期 14 天，数据只保存在你自己的实例里。'
              : '密码以 Argon2id 哈希存储，不会明文保存。'}
          </div>
        </form>
      </div>
    </div>
  )
}
