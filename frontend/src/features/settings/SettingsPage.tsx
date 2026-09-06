import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import type { Me, PropertyDef } from '../../lib/types'
import { Button, Card, Input, PanelTitle, Select, Switch } from '../../ds'
import { ErrorText, Num, Spinner } from '../../components/ui'

const TIMEZONES = ['Europe/Berlin', 'Europe/Stockholm', 'Europe/London', 'Asia/Shanghai', 'America/New_York', 'UTC']
const PROPERTY_TYPES = ['text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url']

interface ImportPreview {
  batch_id: number
  total_rows: number
  valid_rows: number
  errors: Array<{ row: number; column?: string; message: string }>
  duplicate_candidates: number[]
}

export function SettingsPage({ me }: { me: Me | null }) {
  return (
    <section className="split-grid">
      <AccountPanel me={me} />
      <RemindersPanel />
      <DataPanel />
      <PropertiesPanel />
    </section>
  )
}

function AccountPanel({ me }: { me: Me | null }) {
  const [displayName, setDisplayName] = useState(me?.display_name ?? '')
  const [timezone, setTimezone] = useState(me?.timezone || 'UTC')

  return (
    <Card padding="18px">
      <PanelTitle style={{ marginBottom: 14 }}>账户</PanelTitle>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <Input label="显示名称" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        <Input label="邮箱" value={me?.email ?? ''} readOnly disabled hint="邮箱由管理员通过 admin CLI 维护" />
        <Select
          label="时区"
          options={TIMEZONES.includes(timezone) ? TIMEZONES : [timezone, ...TIMEZONES]}
          value={timezone}
          onChange={(e) => setTimezone(e.target.value)}
        />
        <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
          个人资料写接口尚未开放；当前显示的是会话中的值，改动不会持久化。
        </p>
      </div>
    </Card>
  )
}

/**
 * Reminder preferences are stored per browser until the backend exposes a
 * preferences endpoint — the design's copy already scopes them to in-app hints.
 */
const REMINDER_KEY = 'offerlog.reminders'

const REMINDERS: Array<{ key: string; label: string; hint: string; fallback: boolean }> = [
  { key: 'overdue', label: '逾期待办提醒', hint: '打开应用时置顶显示', fallback: true },
  { key: 'interview', label: '面试前一天提示', hint: '含时区换算', fallback: true },
  { key: 'stale', label: '投递满 14 天未回复', hint: '提示发跟进邮件', fallback: true },
  { key: 'weekly', label: '周报汇总', hint: '每周一生成本周进展快照', fallback: false },
]

function readReminders(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(REMINDER_KEY)
    return raw ? (JSON.parse(raw) as Record<string, boolean>) : {}
  } catch {
    return {}
  }
}

function RemindersPanel() {
  const [state, setState] = useState<Record<string, boolean>>(() => {
    const stored = readReminders()
    return Object.fromEntries(REMINDERS.map((r) => [r.key, stored[r.key] ?? r.fallback]))
  })

  const toggle = (key: string, next: boolean) => {
    const updated = { ...state, [key]: next }
    setState(updated)
    try {
      localStorage.setItem(REMINDER_KEY, JSON.stringify(updated))
    } catch {
      /* private mode — the toggle still applies for this session */
    }
  }

  return (
    <Card padding="18px">
      <PanelTitle style={{ marginBottom: 6 }}>提醒</PanelTitle>
      <p style={{ margin: '0 0 14px', fontSize: 12, color: 'var(--text-muted)' }}>
        只在你打开应用时提示，不发邮件。
      </p>
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {REMINDERS.map((r) => (
          <div key={r.key} className="panel-row" style={{ padding: '10px 0' }}>
            <span className="grow">
              <span style={{ display: 'block', fontSize: 14 }}>{r.label}</span>
              <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{r.hint}</span>
            </span>
            <Switch ariaLabel={r.label} checked={state[r.key]} onChange={(next) => toggle(r.key, next)} />
          </div>
        ))}
      </div>
    </Card>
  )
}

function DataPanel() {
  const qc = useQueryClient()
  const fileRef = useRef<HTMLInputElement>(null)
  const batchRef = useRef<{ id: number; file: File } | null>(null)
  const [info, setInfo] = useState('')
  const [infoTone, setInfoTone] = useState<'ok' | 'err'>('ok')

  const preview = useMutation({
    mutationFn: (f: File) => {
      const fd = new FormData()
      fd.append('file', f)
      return api.post<ImportPreview>('/api/v1/imports/preview', fd, true)
    },
    onSuccess: (pv) => {
      setInfoTone('ok')
      setInfo(
        `预检完成：共 ${pv.total_rows} 行，有效 ${pv.valid_rows} 行，错误 ${pv.errors.length} 行，` +
          `疑似重复 ${pv.duplicate_candidates.length} 行。` +
          (pv.errors.length ? ' 可下载逐行错误报告或修正 CSV 后重试。' : ''),
      )
      const file = fileRef.current?.files?.[0]
      if (file) batchRef.current = { id: pv.batch_id, file }
    },
    onError: (e: unknown) => {
      setInfoTone('err')
      setInfo(e instanceof ApiError ? e.message : '预检失败')
    },
  })

  const commit = useMutation({
    mutationFn: () => {
      const b = batchRef.current
      if (!b) throw new Error('请先预检')
      const fd = new FormData()
      fd.append('file', b.file)
      return api.post<{ inserted: number }>(`/api/v1/imports/${b.id}/commit`, fd, true)
    },
    onSuccess: (r) => {
      setInfoTone('ok')
      setInfo(`导入完成：成功写入 ${r.inserted} 条。当前状态按 CSV 记录，不伪造历史。`)
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (e: unknown) => {
      setInfoTone('err')
      setInfo(e instanceof ApiError ? e.message : '导入失败')
    },
  })

  return (
    <Card padding="18px">
      <PanelTitle style={{ marginBottom: 6 }}>数据</PanelTitle>
      <p style={{ margin: '0 0 14px', fontSize: 12, color: 'var(--text-muted)' }}>
        导出为 CSV 或从其它追踪表导入，导入前会做预检（字段映射 + 类型错误 + 重复候选）；重复项默认新建，不覆盖已有记录。
      </p>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        <Button variant="secondary" size="sm" onClick={() => window.open('/api/v1/exports/applications.csv', '_blank')}>
          导出全部
        </Button>
        <Button variant="secondary" size="sm" onClick={() => fileRef.current?.click()}>
          {preview.isPending ? <Spinner size={14} /> : null}导入 CSV
        </Button>
        <input
          ref={fileRef}
          type="file"
          accept=".csv"
          style={{ display: 'none' }}
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) preview.mutate(f)
          }}
        />
        {batchRef.current && (
          <Button variant="primary" size="sm" disabled={commit.isPending} onClick={() => commit.mutate()}>
            {commit.isPending ? <Spinner size={14} /> : '确认导入'}
          </Button>
        )}
      </div>

      {info && (
        <p
          role="status"
          style={{ marginTop: 12, fontSize: 13, color: infoTone === 'err' ? 'var(--danger)' : 'var(--positive)' }}
        >
          {info}
        </p>
      )}

      {preview.data && preview.data.errors.length > 0 && (
        <details style={{ marginTop: 8 }}>
          <summary style={{ cursor: 'pointer', fontSize: 13, color: 'var(--text-muted)' }}>
            逐行错误（{preview.data.errors.length}）
          </summary>
          <ul style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            {preview.data.errors.slice(0, 50).map((e, i) => (
              <li key={i}>
                第 {e.row} 行{e.column ? `（${e.column}）` : ''}: {e.message}
              </li>
            ))}
          </ul>
        </details>
      )}

      <div
        style={{
          marginTop: 16,
          paddingTop: 14,
          borderTop: '1px solid var(--border-alt)',
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
          fontSize: 13,
          color: 'var(--text-muted)',
        }}
      >
        <span>完整备份（事件、视图与文件）请使用运维备份流程，见 docs/runbook.md</span>
      </div>
    </Card>
  )
}

function PropertiesPanel() {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [key, setKey] = useState('')
  const [dataType, setDataType] = useState('text')
  const [err, setErr] = useState('')

  const q = useQuery({
    queryKey: ['properties'],
    queryFn: () => api.get<{ items: PropertyDef[] }>('/api/v1/properties'),
  })

  const create = useMutation({
    mutationFn: () =>
      api.post('/api/v1/properties', { name, key: key || undefined, data_type: dataType, options: [] }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['properties'] })
      setName('')
      setKey('')
      setErr('')
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '创建失败'),
  })

  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/properties/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['properties'] }),
  })

  const props = q.data?.items ?? []

  return (
    <Card padding="18px">
      <PanelTitle style={{ marginBottom: 6 }}>自定义属性</PanelTitle>
      <p style={{ margin: '0 0 14px', fontSize: 12, color: 'var(--text-muted)' }}>
        自定义字段存于每条记录的 JSONB。选项重命名不影响历史值；有数据的字段变更类型需显式转换。
      </p>
      {err && <ErrorText>{err}</ErrorText>}
      <div className="field-grid" style={{ marginBottom: 12 }}>
        <Input label="显示名" value={name} onChange={(e) => setName(e.target.value)} placeholder="期望职级" />
        <Input label="键（可选）" value={key} onChange={(e) => setKey(e.target.value)} placeholder="level" />
        <Select label="类型" options={PROPERTY_TYPES} value={dataType} onChange={(e) => setDataType(e.target.value)} />
      </div>
      <Button variant="primary" size="sm" disabled={!name.trim() || create.isPending} onClick={() => create.mutate()}>
        {create.isPending ? <Spinner size={14} /> : '＋ 添加'}
      </Button>

      {props.length === 0 ? (
        <p style={{ marginTop: 12, fontSize: 13, color: 'var(--text-muted)' }}>还没有自定义属性</p>
      ) : (
        <div style={{ marginTop: 12, display: 'flex', flexDirection: 'column' }}>
          {props.map((p) => (
            <div key={p.id} className="panel-row" style={{ padding: '8px 0' }}>
              <span className="grow">
                <b style={{ fontSize: 14 }}>{p.name}</b>{' '}
                <Num color="var(--text-muted)">
                  {p.key} · {p.data_type}
                </Num>
              </span>
              <Button variant="ghost" size="sm" onClick={() => remove.mutate(p.id)}>
                删除
              </Button>
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}
