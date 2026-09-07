import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import type { Me, Preferences } from '../../lib/types'
import { Button, Card, Input, PanelTitle, Select, Switch } from '../../ds'
import { ErrorText, Num, Spinner } from '../../components/ui'

const PROPERTY_TYPES = ['text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url']

interface ImportPreview {
  batch_id: number
  total_rows: number
  valid_rows: number
  errors: Array<{ row: number; column?: string; message: string }>
  duplicate_candidates: number[]
}

/** Common selectable timezones (any IANA name is accepted server-side). */
const TIMEZONE_PRESETS = [
  'Europe/Dublin',
  'Europe/London',
  'Europe/Berlin',
  'Europe/Stockholm',
  'Asia/Shanghai',
  'America/New_York',
  'UTC',
]

const REMINDER_HINTS: Record<string, string> = {
  overdue: '逾期待办会在你打开应用时置顶提醒',
  interview: '面试前一天提醒（按你的时区换算）',
  stale: '投递满 N 天未回复时提醒跟进；对方回复后自动停止，不自动判定拒绝',
  weekly: '每周一生成上一周复盘快照',
}

export function SettingsPage({ me }: { me: Me | null }) {
  return (
    <section className="split-grid">
      <AccountPanel me={me} />
      <RemindersPanel me={me} />
      <DataPanel />
      <PropertiesPanel />
    </section>
  )
}

function AccountPanel({ me }: { me: Me | null }) {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['preferences'],
    queryFn: () => api.get<Preferences>('/api/v1/preferences'),
  })
  const [saved, setSaved] = useState(false)
  const [err, setErr] = useState('')

  const prefs = q.data

  const save = useMutation({
    mutationFn: (b: { display_name?: string; timezone?: string }) =>
      api.put('/api/v1/preferences', {
        display_name: b.display_name ?? undefined,
        timezone: b.timezone ?? undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['preferences'] })
      qc.invalidateQueries({ queryKey: ['me'] })
      setSaved(true)
      window.dispatchEvent(new Event('offerlog:profile-changed'))
      window.setTimeout(() => setSaved(false), 2500)
    },
    onError: (e: unknown) => {
      setErr(e instanceof ApiError ? e.message : '保存失败')
      setSaved(false)
    },
  })

  if (q.isLoading) return null

  return (
    <Card padding="18px">
      <PanelTitle style={{ marginBottom: 14 }}>账户</PanelTitle>
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <ProfileField
          label="显示名称"
          defaultValue={prefs?.display_name ?? me?.display_name ?? ''}
          onSave={(v) => save.mutate({ display_name: v })}
          saving={save.isPending}
        />
        <Input label="邮箱" value={me?.email ?? ''} readOnly disabled hint="邮箱由管理员通过 admin CLI 维护" />
        <TimezoneField
          value={prefs?.timezone ?? me?.timezone ?? 'Europe/Dublin'}
          onSave={(v) => save.mutate({ timezone: v })}
          saving={save.isPending}
        />
        {saved && (
          <span role="status" style={{ fontSize: 13, color: 'var(--positive)' }}>
            已保存 ✓ 刷新或换设备会读到相同设置
          </span>
        )}
      </div>
    </Card>
  )
}

function ProfileField({
  label,
  defaultValue,
  onSave,
  saving,
}: {
  label: string
  defaultValue: string
  onSave: (v: string) => void
  saving: boolean
}) {
  const [value, setValue] = useState(defaultValue)
  const dirty = value !== defaultValue
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 8 }}>
      <div style={{ flex: 1 }}>
        <Input label={label} value={value} onChange={(e) => setValue(e.target.value)} />
      </div>
      <Button
        variant="primary"
        size="sm"
        disabled={!dirty || saving || !value.trim()}
        onClick={() => onSave(value.trim())}
        style={{ height: 'var(--control-h-md)' }}
      >
        {saving ? <Spinner size={14} /> : '保存'}
      </Button>
    </div>
  )
}

function TimezoneField({
  value,
  onSave,
  saving,
}: {
  value: string
  onSave: (v: string) => void
  saving: boolean
}) {
  const [tz, setTz] = useState(value)
  const dirty = tz !== value
  const options = TIMEZONE_PRESETS.includes(tz) ? TIMEZONE_PRESETS : [tz, ...TIMEZONE_PRESETS]
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 8 }}>
      <div style={{ flex: 1 }}>
        <Select
          label="时区"
          options={options}
          value={tz}
          onChange={(e) => setTz(e.target.value)}
          aria-label="时区（IANA 名称）"
        />
      </div>
      <Button
        variant="primary"
        size="sm"
        disabled={!dirty || saving}
        onClick={() => onSave(tz)}
        style={{ height: 'var(--control-h-md)' }}
      >
        {saving ? <Spinner size={14} /> : '保存'}
      </Button>
    </div>
  )
}

function RemindersPanel({ me }: { me: Me | null }) {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['preferences'],
    queryFn: () => api.get<Preferences>('/api/v1/preferences'),
  })
  const [err, setErr] = useState('')
  const [savingKey, setSavingKey] = useState('')

  const prefs = q.data

  const save = useMutation({
    mutationFn: (patch: Partial<Record<string, boolean | number>>) => api.put('/api/v1/preferences', patch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['preferences'] })
      setSavingKey('')
    },
    onError: (e: unknown) => {
      setErr(e instanceof ApiError ? e.message : '保存失败')
      setSavingKey('')
    },
  })

  const toggle = (key: string, next: boolean | number, label: string) => {
    setSavingKey(key)
    setErr('')
    save.mutate({ [key]: next } as never)
  }

  const remindRows: Array<{ key: string; label: string; hint: string; value: boolean; control: 'switch' }> = [
    { key: 'remind_overdue', label: '逾期待办提醒', hint: REMINDER_HINTS.overdue, value: prefs?.remind_overdue ?? true, control: 'switch' },
    { key: 'remind_interview', label: '面试前一天提示', hint: REMINDER_HINTS.interview, value: prefs?.remind_interview ?? true, control: 'switch' },
    { key: 'remind_weekly', label: '周报汇总', hint: REMINDER_HINTS.weekly, value: prefs?.remind_weekly ?? false, control: 'switch' },
  ]

  if (q.isLoading) return null

  return (
    <Card padding="18px">
      <PanelTitle style={{ marginBottom: 6 }}>提醒</PanelTitle>
      <p style={{ margin: '0 0 4px', fontSize: 12, color: 'var(--text-muted)' }}>
        由服务端在你打开应用时生成站内提醒，不发邮件。关闭某个开关后不再生成新提醒；已生成的提醒保留，可在通知列表里处理或忽略。
      </p>
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', marginTop: 8 }}>
        {remindRows.map((r) => (
          <div key={r.key} className="panel-row" style={{ padding: '10px 0' }}>
            <span className="grow">
              <span style={{ display: 'block', fontSize: 14 }}>{r.label}</span>
              <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{r.hint}</span>
            </span>
            {savingKey === r.key ? (
              <Spinner size={14} />
            ) : (
              <Switch ariaLabel={r.label} checked={r.value} onChange={(next) => toggle(r.key, next, r.label)} />
            )}
          </div>
        ))}
        <div className="panel-row" style={{ padding: '10px 0' }}>
          <span className="grow">
            <span style={{ display: 'block', fontSize: 14 }}>投递满 N 天未回复提醒</span>
            <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
              {REMINDER_HINTS.stale}（0 = 关闭）
            </span>
          </span>
          <Select
            aria-label="未回复提醒天数"
            options={[0, 7, 14, 21, 30].map((n) => ({ value: String(n), label: n === 0 ? '关闭' : `${n} 天` }))}
            value={String(prefs?.remind_stale_days ?? 14)}
            onChange={(e) => toggle('remind_stale_days', Number(e.target.value), 'stale')}
            fullWidth={false}
            style={{ width: 110 }}
          />
        </div>
      </div>
      <p style={{ margin: '12px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
        偏好存于服务端（{me?.timezone || 'Europe/Dublin'}），与账号绑定，换设备一致。
      </p>
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
      qc.invalidateQueries({ queryKey: ['home'] })
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
    queryFn: () => api.get<{ items: import('../../lib/types').PropertyDef[] }>('/api/v1/properties'),
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
