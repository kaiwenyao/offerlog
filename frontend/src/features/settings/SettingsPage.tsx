import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import { Spinner } from '../../components/ui'
import type { PropertyDef } from '../../lib/types'

export function SettingsPage() {
  const qc = useQueryClient()
  const [toast, setToast] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const [importInfo, setImportInfo] = useState('')
  const batchRef = useRef<{ id: number; file: File } | null>(null)

  const preview = useMutation({
    mutationFn: async (f: File) => {
      const fd = new FormData()
      fd.append('file', f)
      return api.post<{
        batch_id: number
        total_rows: number
        valid_rows: number
        errors: Array<{ row: number; column?: string; message: string }>
        duplicate_candidates: number[]
      }>('/api/v1/imports/preview', fd, true)
    },
    onSuccess: (pv) => {
      const errCount = pv.errors.length
      setImportInfo(
        `预检完成：共 ${pv.total_rows} 行，有效 ${pv.valid_rows} 行，错误 ${errCount} 行，疑似重复 ${pv.duplicate_candidates.length} 行。` +
          (errCount ? ' 可下载逐行错误报告或修正 CSV 后重试。' : '')
      )
      batchRef.current = { id: pv.batch_id, file: fileRef.current?.files?.[0] as File }
    },
    onError: (e) => setImportInfo(e instanceof ApiError ? e.message : '预检失败'),
  })

  const commit = useMutation({
    mutationFn: async () => {
      const b = batchRef.current
      if (!b) throw new Error('请先预检')
      const fd = new FormData()
      fd.append('file', b.file)
      return api.post<{ inserted: number }>(`/api/v1/imports/${b.id}/commit`, fd, true)
    },
    onSuccess: (r) => {
      setImportInfo(`导入完成：成功写入 ${r.inserted} 条。当前状态按 CSV 记录，不伪造历史。`)
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (e) => setImportInfo(e instanceof ApiError ? e.message : '导入失败'),
  })

  const exportCsv = () => {
    window.open('/api/v1/exports/applications.csv', '_blank')
  }

  return (
    <div>
      <h1 className="display" style={{ fontSize: 24 }}>设置</h1>
      <div className="card mt16" style={{ padding: 16, maxWidth: 640 }}>
        <h2 className="panel-title">CSV 导入</h2>
        <p className="small muted">
          支持列：公司、岗位、链接、地点、远程、类型、渠道、状态、优先级、标签、备注、截止日期、投递时间、薪资等。
          先预检（字段映射 + 类型错误 + 重复候选），确认后写入；重复项默认新建，不覆盖已有记录。
        </p>
        <div className="row mt8">
          <input
            ref={fileRef}
            type="file"
            accept=".csv"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) preview.mutate(f)
            }}
          />
          {batchRef.current && (
            <button className="btn btn-primary" disabled={commit.isPending} onClick={() => commit.mutate()}>
              {commit.isPending ? '导入中…' : '确认导入'}
            </button>
          )}
        </div>
        {preview.isPending && <p className="small mt8"><span className="spinner" /> 预检中…</p>}
        {importInfo && <p role="status" className="small mt8" style={{ color: '#147d70' }}>{importInfo}</p>}
        {preview.data && preview.data.errors.length > 0 && (
          <details className="mt8">
            <summary className="small muted" style={{ cursor: 'pointer' }}>逐行错误（{preview.data.errors.length}）</summary>
            <ul className="small">
              {preview.data.errors.slice(0, 50).map((e, i) => (
                <li key={i}>第 {e.row} 行{e.column ? `（${e.column}）` : ''}: {e.message}</li>
              ))}
            </ul>
          </details>
        )}
      </div>

      <div className="card mt16" style={{ padding: 16, maxWidth: 640 }}>
        <h2 className="panel-title">CSV 导出</h2>
        <p className="small muted">业务导出便于分析与迁移；完整备份包含事件、视图与文件，请使用运维备份流程（见 docs）。</p>
        <button className="btn btn-primary mt8" onClick={exportCsv}>导出岗位 CSV</button>
      </div>

      <div className="card mt16" style={{ padding: 16, maxWidth: 640 }}>
        <h2 className="panel-title">自定义属性</h2>
        <p className="small muted">
          自定义字段存于每条记录的 JSONB。选项重命名不影响历史值；有数据的字段变更类型需显式转换（首版在编辑表单提示）。
        </p>
        <PropertyManager />
      </div>

      {toast && <p role="status" className="small mt8">{toast}</p>}
    </div>
  )
}

function PropertyManager() {
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
      setName(''); setKey(''); setErr('')
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : '创建失败'),
  })
  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/api/v1/properties/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['properties'] }),
  })
  const props = q.data?.items ?? []
  return (
    <div className="mt8">
      {err && <p role="alert" className="err">{err}</p>}
      <div className="fgrid">
        <div className="fld">
          <label>显示名</label>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="期望职级" />
        </div>
        <div className="fld">
          <label>键（可选，留空自动生成）</label>
          <input className="input" value={key} onChange={(e) => setKey(e.target.value)} placeholder="level" />
        </div>
        <div className="fld">
          <label>类型</label>
          <select className="select" value={dataType} onChange={(e) => setDataType(e.target.value)}>
            {['text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url'].map((t) => (
              <option key={t} value={t}>{t}</option>
            ))}
          </select>
        </div>
        <div className="fld" style={{ justifyContent: 'flex-end' }}>
          <button className="btn btn-primary" disabled={!name.trim() || create.isPending} onClick={() => create.mutate()}>
            {create.isPending ? <Spinner /> : '＋ 添加'}
          </button>
        </div>
      </div>
      {props.length === 0 ? (
        <p className="small muted mt8">还没有自定义属性</p>
      ) : (
        <ul style={{ listStyle: 'none', padding: 0 }}>
          {props.map((p) => (
            <li key={p.id} className="row" style={{ padding: '8px 0', borderBottom: '1px solid #f0f3f7', justifyContent: 'space-between' }}>
              <span>
                <b>{p.name}</b> <span className="small muted">{p.key} · {p.data_type}</span>
              </span>
              <button className="btn btn-ghost btn-small" onClick={() => remove.mutate(p.id)}>删除</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
