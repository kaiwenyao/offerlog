import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { api, ApiError, fmtDate } from '../../lib/api'
import type { AppRow, SavedView } from '../../lib/types'
import { statusMeta } from '../../lib/status'
import { StatusChip, Spinner, EmptyHint, Modal } from '../../components/ui'
import { Drawer } from './drawer'

interface FilterCond {
  field: string
  op: string
  value: unknown
}
interface FilterGroup {
  op: 'and' | 'or'
  conditions: (FilterCond | FilterGroup)[]
}

const BUILTIN: SavedView[] = [
  { id: -1, name: '全部机会', layout: 'table', is_builtin: true, columns: null, filter_ast: null, sort: null, group_by: null, schema_version: 1 },
  { id: -2, name: '待投递', layout: 'board', is_builtin: true, columns: null, filter_ast: { conditions: [] }, sort: null, group_by: null, schema_version: 1 },
  { id: -3, name: '进行中', layout: 'board', is_builtin: true, columns: null, filter_ast: { conditions: [] }, sort: null, group_by: null, schema_version: 1 },
  { id: -4, name: '收到 Offer', layout: 'board', is_builtin: true, columns: null, filter_ast: { conditions: [] }, sort: null, group_by: null, schema_version: 1 },
  { id: -5, name: '已结束', layout: 'board', is_builtin: true, columns: null, filter_ast: { conditions: [] }, sort: null, group_by: null, schema_version: 1 },
]

function statusClause(statuses: string[]): FilterGroup {
  return {
    op: 'or',
    conditions: statuses.map((s) => ({ field: 'status', op: 'eq', value: s })),
  }
}
BUILTIN[1].filter_ast = statusClause(['saved', 'preparing'])
BUILTIN[2].filter_ast = statusClause(['applied', 'screening', 'assessment', 'interviewing'])
BUILTIN[3].filter_ast = statusClause(['offer', 'accepted'])
BUILTIN[4].filter_ast = statusClause(['accepted', 'rejected', 'withdrawn', 'closed'])

export function DatabasePage() {
  const { id: routeApp } = useParams()
  const nav = useNavigate()
  const qc = useQueryClient()
  const [viewId, setViewId] = useState(-1)
  const [search, setSearch] = useState('')
  const [layout, setLayout] = useState<'table' | 'board' | 'list'>('table')
  const [page, setPage] = useState(1)
  const [selApp, setSelApp] = useState<number | null>(routeApp ? Number(routeApp) : null)
  const [showCreate, setShowCreate] = useState(false)
  const [extraFilters, setExtraFilters] = useState<FilterCond[]>([])
  const [trashMode, setTrashMode] = useState(false)
  const [cCompany, setCCompany] = useState('')
  const [cPosition, setCPosition] = useState('')
  const [cUrl, setCUrl] = useState('')
  const [cErr, setCErr] = useState('')
  const [selRows, setSelRows] = useState<Set<number>>(new Set())
  const [bulkTag, setBulkTag] = useState('')
  const [bulkPriority, setBulkPriority] = useState('')

  const viewsQ = useQuery({ queryKey: ['views'], queryFn: () => api.get<{ items: SavedView[] }>('/api/v1/views') })
  const appsQ = useQuery({
    queryKey: ['apps', 'db', viewId, search, page, JSON.stringify(extraFilters), trashMode],
    queryFn: async () => {
      const current = [...BUILTIN, ...(viewsQ.data?.items ?? [])].find((v) => v.id === viewId)
      if (trashMode) {
        const res = await api.get<{ items: AppRow[]; total: number }>('/api/v1/applications?trash=1&page=1&page_size=60')
        return res
      }
      const body: Record<string, unknown> = { page, page_size: 60, sort: [{ field: 'updated_at', dir: 'desc' }] }
      let conds: (FilterCond | FilterGroup)[] = []
      if (current && current.id >= 0 && current.filter_ast) {
        const ast = current.filter_ast as FilterGroup
        if (Array.isArray(ast.conditions)) conds = [...ast.conditions]
      }
      if (current && current.id < 0 && current.filter_ast && !Array.isArray(current.filter_ast)) {
        conds = [current.filter_ast as FilterGroup]
      }
      if (search) conds = [...conds, { field: 'position', op: 'contains', value: search }]
      conds = [...conds, ...extraFilters]
      body.filters = conds
      const res = await api.post<{ items: AppRow[]; total: number }>('/api/v1/views/query', body)
      return res
    },
  })
  const showCount = appsQ.data?.total ?? 0
  const appliedCount = extraFilters.length

  const statusForBuiltin = (view: SavedView): string[] => {
    if (view.id === -2) return ['saved', 'preparing']
    if (view.id === -3) return ['applied', 'screening', 'assessment', 'interviewing']
    if (view.id === -4) return ['offer', 'accepted']
    if (view.id === -5) return ['accepted', 'rejected', 'withdrawn', 'closed']
    return []
  }

  const groups = useMemo(() => {
    const rows = appsQ.data?.items ?? []
    const sts = viewId === -1 ? [] : statusForBuiltin(BUILTIN.find((v) => v.id === viewId) ?? BUILTIN[0])
    if (sts.length === 0) return null
    const out = sts.map((s) => ({ status: s, items: rows.filter((r) => r.status === s) }))
    return out
  }, [appsQ.data, viewId])

  const bulkMut = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = { ids: [...selRows] }
      if (bulkTag.trim()) body.add_tags = [bulkTag.trim()]
      if (bulkPriority) body.priority = bulkPriority
      return api.post('/api/v1/applications/bulk', body)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      setSelRows(new Set())
      setBulkTag('')
      setBulkPriority('')
    },
  })
  const toggleRow = (id: number) => {
    const next = new Set(selRows)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelRows(next)
  }
  const restoreMut = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/applications/${id}/restore`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['apps'] }),
  })

  const createApp = useMutation({
    mutationFn: (b: { company_name: string; position: string; job_url?: string }) => api.post('/api/v1/applications', b),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      setShowCreate(false)
      setCCompany('')
      setCPosition('')
      setCUrl('')
    },
    onError: (e) => setCErr(e instanceof ApiError ? e.message : '创建失败'),
  })

  const layoutTabs = (
    <div className="row" role="tablist" aria-label="视图布局">
      {(['table', 'board', 'list'] as const).map((l) => (
        <button
          key={l}
          role="tab"
          aria-selected={layout === l}
          className={'btn btn-ghost small ' + (layout === l ? 'active-layout' : '')}
          onClick={() => setLayout(l)}
        >
          {l === 'table' ? '表格' : l === 'board' ? '看板' : '列表'}
        </button>
      ))}
    </div>
  )

  return (
    <div>
      <div className="row" style={{ justifyContent: 'space-between', marginBottom: 12 }}>
        <h1 className="display" style={{ fontSize: 24, margin: 0 }}>求职数据库</h1>
        <button className="btn btn-primary" onClick={() => setShowCreate(true)}>＋ 新增岗位</button>
      </div>

      <div className="row mb8">
        {[...BUILTIN, ...(viewsQ.data?.items ?? [])].map((v) => (
          <button
            key={v.id}
            className={'btn btn-ghost small ' + (viewId === v.id ? 'active-layout' : '')}
            onClick={() => {
              setViewId(v.id)
              setPage(1)
              if (v.layout === 'board') setLayout('board')
              else if (v.id === -1) setLayout('table')
            }}
          >
            {v.name}
          </button>
        ))}
      </div>

      <div className="row mb8" style={{ justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
        <div className="row grow">
          <input
            className="input"
            style={{ maxWidth: 280 }}
            placeholder="搜索岗位 / 公司…"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              setPage(1)
            }}
            aria-label="搜索"
          />
          {layoutTabs}
        </div>
        <div className="row">
          <button
            className={'btn btn-ghost btn-small ' + (trashMode ? 'active-layout' : '')}
            onClick={() => setTrashMode((v) => !v)}
            aria-pressed={trashMode}
          >
            🗑 回收站
          </button>
          <span className="small muted num">
            共 {showCount} 条{appliedCount > 0 ? `，已应用 ${appliedCount} 个条件` : ''}
          </span>
        </div>
      </div>

      {appsQ.isLoading ? (
        <Spinner />
      ) : appsQ.isError ? (
        <EmptyHint>
          <p role="alert">加载失败，请刷新重试</p>
        </EmptyHint>
      ) : showCount === 0 ? (
        <EmptyHint>
          <p style={{ marginTop: 0 }}>{search || extraFilters.length ? '没有符合条件的记录' : '还没有岗位记录'}</p>
          {search || extraFilters.length ? (
            <button
              className="btn btn-ghost"
              onClick={() => {
                setSearch('')
                setExtraFilters([])
              }}
            >
              清除筛选
            </button>
          ) : (
            <button className="btn btn-primary" onClick={() => setShowCreate(true)}>
              添加第一个岗位
            </button>
          )}
        </EmptyHint>
      ) : layout === 'board' ? (
        <div className="board">
          {(groups ?? []).map((g) => {
            const m = statusMeta(g.status)
            return (
              <section key={g.status} className="board-col" aria-label={m.label}>
                <h3>
                  <span>
                    <span aria-hidden>{m.icon}</span> {m.label}
                  </span>
                  <span className="num">{g.items.length}</span>
                </h3>
                {g.items.map((a) => (
                  <button key={a.id} className="board-card" onClick={() => setSelApp(a.id)}>
                    <span className="bc-title">{a.position}</span>
                    <span className="bc-sub">{a.company_name}</span>
                    {a.next_action && <span className="small muted">→ {a.next_action}</span>}
                  </button>
                ))}
              </section>
            )
          })}
        </div>
      ) : layout === 'list' ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {(appsQ.data?.items ?? []).map((a) => (
            <button key={a.id} className="card" style={{ padding: 12, textAlign: 'left', cursor: 'pointer' }} onClick={() => setSelApp(a.id)}>
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <strong>
                  {a.company_name} · {a.position}
                </strong>
                <StatusChip status={a.status} />
              </div>
              <div className="row mt8 small muted" style={{ gap: 12 }}>
                {a.next_action && <span>下一步：{a.next_action}</span>}
                {a.next_action_due_at && <span>截止 {fmtDate(a.next_action_due_at)}</span>}
              </div>
            </button>
          ))}
        </div>
      ) : (
        <div className="card tbl-wrap">
          {selRows.size > 0 && (
            <div className="row" style={{ padding: '8px 10px', borderBottom: '1px solid #e5eaf0', background: '#f4f7fd', gap: 10 }}>
              <b className="small">已选 {selRows.size} 条</b>
              <input className="input" style={{ maxWidth: 150, minHeight: 30 }} placeholder="加标签" value={bulkTag} onChange={(e) => setBulkTag(e.target.value)} />
              <select className="select" style={{ maxWidth: 110, minHeight: 30 }} value={bulkPriority} onChange={(e) => setBulkPriority(e.target.value)}>
                <option value="">优先级…</option>
                <option value="high">高</option>
                <option value="medium">中</option>
                <option value="low">低</option>
              </select>
              <button className="btn btn-primary btn-small" disabled={!bulkTag.trim() && !bulkPriority} onClick={() => bulkMut.mutate()}>
                {bulkMut.isPending ? <Spinner /> : '应用'}
              </button>
              <button className="btn btn-ghost btn-small" onClick={() => setSelRows(new Set())}>取消</button>
            </div>
          )}
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 32 }}>
                  <input
                    type="checkbox"
                    aria-label="选择本页"
                    checked={(appsQ.data?.items ?? []).length > 0 && (appsQ.data?.items ?? []).every((a) => selRows.has(a.id))}
                    onChange={(e) => {
                      const ids = (appsQ.data?.items ?? []).map((a) => a.id)
                      setSelRows(e.target.checked ? new Set(ids) : new Set())
                    }}
                  />
                </th>
                <th>公司 / 岗位</th>
                <th>状态</th>
                <th>渠道</th>
                <th>优先级</th>
                <th>下一步</th>
                <th>截止</th>
                {trashMode && <th>操作</th>}
              </tr>
            </thead>
            <tbody>
              {(appsQ.data?.items ?? []).map((a) => (
                <tr key={a.id} onClick={() => setSelApp(a.id)} style={{ cursor: 'pointer' }}>
                  <td onClick={(e) => e.stopPropagation()}>
                    <input type="checkbox" aria-label={`选择 ${a.position}`} checked={selRows.has(a.id)} onChange={() => toggleRow(a.id)} />
                  </td>                  <td>
                    <div style={{ fontWeight: 600 }}>{a.position}</div>
                    <div className="small muted">{a.company_name}</div>
                  </td>
                  <td>
                    <StatusChip status={a.status} />
                  </td>
                  <td className="muted">{a.channel || '—'}</td>                  <td>
                    <span className={a.priority === 'high' ? 'priority-high' : a.priority === 'low' ? 'priority-low' : 'priority-med'}>
                      {a.priority === 'high' ? '高' : a.priority === 'low' ? '低' : '中'}
                    </span>
                  </td>
                  <td className="muted small" onClick={(e) => e.stopPropagation()}>
                    <InlineNextAction app={a} onDone={() => qc.invalidateQueries({ queryKey: ['apps'] })} />
                  </td>
                  <td className="muted small num">{fmtDate(a.next_action_due_at ?? a.deadline)}</td>
                  {trashMode && (
                    <td onClick={(e) => e.stopPropagation()}>
                      <button className="btn btn-ghost btn-small" onClick={() => restoreMut.mutate(a.id)}>
                        恢复
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {showCount > 60 && (
        <div className="row mt16" style={{ justifyContent: 'center' }}>
          <button className="btn btn-ghost" disabled={page === 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>
            上一页
          </button>
          <span className="small muted num">第 {page} 页</span>
          <button className="btn btn-ghost" disabled={page * 60 >= showCount} onClick={() => setPage((p) => p + 1)}>
            下一页
          </button>
        </div>
      )}

      {showCreate && (
        <Modal
          title="新增岗位"
          onClose={() => setShowCreate(false)}
          footer={
            <button
              className="btn btn-primary"
              disabled={createApp.isPending || !cCompany.trim() || !cPosition.trim()}
              onClick={() => createApp.mutate({ company_name: cCompany.trim(), position: cPosition.trim(), job_url: cUrl.trim() })}
            >
              {createApp.isPending ? <Spinner /> : '创建'}
            </button>
          }
        >
          {cErr && <p role="alert" className="err">{cErr}</p>}
          <div className="fgrid">
            <div className="fld">
              <label htmlFor="cf-company">公司 *</label>
              <input
                id="cf-company"
                className="input"
                value={cCompany}
                onChange={(e) => setCCompany(e.target.value)}
                placeholder="公司名称"
                autoFocus
              />
            </div>
            <div className="fld">
              <label htmlFor="cf-pos">岗位 *</label>
              <input
                id="cf-pos"
                className="input"
                value={cPosition}
                onChange={(e) => setCPosition(e.target.value)}
                placeholder="岗位名称"
              />
            </div>
            <div className="fld full">
              <label htmlFor="cf-url">链接（可选）</label>
              <input id="cf-url" className="input" value={cUrl} onChange={(e) => setCUrl(e.target.value)} placeholder="https://…" />
            </div>
            <p className="small muted full">保存后仍可继续编辑完整信息、上传附件并更新进度。</p>
          </div>
        </Modal>
      )}

      {selApp !== null && <Drawer appId={selApp} onClose={() => setSelApp(null)} />}
    </div>
  )
}

// InlineNextAction renders the 下一步 cell with inline editing: Enter saves,
// Esc cancels, request failures keep the input so the user can retry (§4.3).
function InlineNextAction({ app, onDone }: { app: AppRow; onDone: () => void }) {
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [val, setVal] = useState(app.next_action)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const save = async () => {
    setSaving(true)
    setErr('')
    try {
      await api.patch(`/api/v1/applications/${app.id}`, {
        version: app.version,
        next_action: val.trim() || null,
      })
      setEditing(false)
      onDone()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }
  if (!editing) {
    return (
      <button
        className="btn btn-ghost btn-small"
        style={{ minHeight: 24, padding: '0 6px', border: 'none', color: app.next_action ? undefined : '#aab4c2' }}
        title="点击编辑下一步"
        onClick={() => {
          setVal(app.next_action)
          setEditing(true)
        }}
      >
        {app.next_action || '＋ 添加'}
      </button>
    )
  }
  void qc
  return (
    <span>
      <input
        autoFocus
        className="input"
        style={{ minHeight: 28, width: 180 }}
        value={val}
        disabled={saving}
        aria-label="下一步行动"
        onChange={(e) => setVal(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') void save()
          else if (e.key === 'Escape') setEditing(false)
        }}
        onBlur={() => {
          if (!saving) setEditing(false)
        }}
      />
      {saving && <span className="spinner" style={{ width: 12, height: 12 }} />}
      {err && <span className="err small" role="alert">{err}</span>}
    </span>
  )
}
