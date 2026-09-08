import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, ApiError, fmtDate, fmtDay, localDateTimeToInstant, toDayString } from '../../lib/api'
import { effectiveZone } from '../../lib/tz'
import type { AppRow, SavedView } from '../../lib/types'
import { FLOW_PIPS, priorityLabel, statusMeta } from '../../lib/status'
import { Button, Card, Input, Select, Tabs, Tag } from '../../ds'
import { CompanyMark } from '../../components/Icon'
import { StageRail } from '../../components/StageTrail'
import { Dot, EmptyHint, ErrorText, Modal, Num, PageSpinner, Spinner, StatusChip } from '../../components/ui'
import { Drawer } from './drawer'
import { BUILTIN, boardBuckets, buildFilters, type BoardBucket, type FilterCond, type Layout } from './views'

const PAGE_SIZE = 60

/**
 * Ad-hoc chips layered on top of the active view. Kept distinct from the
 * built-in view names so the same filter never appears twice in the bar.
 */
const QUICK_FILTERS: Array<{ label: string; cond: FilterCond }> = [
  { label: '高优先级', cond: { field: 'priority', op: 'eq', value: 'high' } },
  { label: '面试中', cond: { field: 'status', op: 'eq', value: 'interviewing' } },
]

const LAYOUT_TABS = [
  { value: 'table', label: '表格' },
  { value: 'board', label: '看板' },
  { value: 'list', label: '列表' },
]

/** Table geometry from the design's 工序进度 grid, as fixed table columns. The
 *  rail column is wide enough for the current stage's label to sit inside its
 *  block, the way the canvas draws it. */
const COLS = ['236px', '176px', '116px', 'auto', '92px', '88px', '104px', '56px']

/** YYYY-MM-DD strictly before today (user-zone day-key compare). */
function isDayBeforeToday(dayStr: string): boolean {
  const today = toDayString(new Date().toISOString(), effectiveZone()) ?? ''
  return dayStr < today
}

export function DatabasePage() {
  const { id: routeApp } = useParams()
  const [params, setParams] = useSearchParams()
  const nav = useNavigate()
  const qc = useQueryClient()

  const [viewId, setViewId] = useState(() => Number(params.get('view')) || -1)
  const [layout, setLayout] = useState<Layout>(() => (params.get('layout') as Layout) || 'table')
  const [search, setSearch] = useState(() => params.get('q') ?? '')
  const [page, setPage] = useState(1)
  const [selApp, setSelApp] = useState<number | null>(routeApp ? Number(routeApp) : null)
  const [showCreate, setShowCreate] = useState(false)
  const [extraFilters, setExtraFilters] = useState<FilterCond[]>([])
  const [trashMode, setTrashMode] = useState(false)
  const [selRows, setSelRows] = useState<Set<number>>(new Set())
  const [bulkTag, setBulkTag] = useState('')
  const [bulkPriority, setBulkPriority] = useState('')

  // Header search / saved views / the header's ＋ 记一个岗位 button all
  // navigate here with query params.
  useEffect(() => {
    const q = params.get('q')
    if (q !== null) setSearch(q)
    const v = params.get('view')
    if (v !== null) setViewId(Number(v))
    const l = params.get('layout') as Layout | null
    if (l) setLayout(l)
    if (params.get('new') === '1') {
      setShowCreate(true)
      // Consume the flag so re-clicking the header button re-opens the dialog
      // instead of navigating to an unchanged URL.
      const next = new URLSearchParams(params)
      next.delete('new')
      setParams(next, { replace: true })
    }
    setPage(1)
  }, [params, setParams])

  const viewsQ = useQuery({ queryKey: ['views'], queryFn: () => api.get<{ items: SavedView[] }>('/api/v1/views') })
  const allViews = useMemo(() => [...BUILTIN, ...(viewsQ.data?.items ?? [])], [viewsQ.data])

  const appsQ = useQuery({
    queryKey: ['apps', 'db', viewId, search, page, JSON.stringify(extraFilters), trashMode],
    queryFn: async () => {
      if (trashMode) {
        return api.get<{ items: AppRow[]; total: number }>('/api/v1/applications?trash=1&page=1&page_size=60&include=stage_history')
      }
      const view = allViews.find((v) => v.id === viewId)
      return api.post<{ items: AppRow[]; total: number }>('/api/v1/views/query', {
        page,
        page_size: PAGE_SIZE,
        sort: [{ field: 'updated_at', dir: 'desc' }],
        filters: buildFilters(view, search, extraFilters),
        include: ['stage_history'],
      })
    },
  })

  const rows = appsQ.data?.items ?? []
  const total = appsQ.data?.total ?? 0

  const boardGroups = useMemo(
    () =>
      boardBuckets(viewId).map((bucket) => ({
        bucket,
        items: rows.filter((r) => bucket.statuses.includes(r.status)),
      })),
    [rows, viewId],
  )

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

  const restoreMut = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/applications/${id}/restore`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['apps'] }),
  })

  const toggleRow = (id: number) => {
    const next = new Set(selRows)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelRows(next)
  }

  const toggleQuickFilter = (cond: FilterCond) => {
    const key = JSON.stringify(cond)
    setExtraFilters((prev) =>
      prev.some((f) => JSON.stringify(f) === key) ? prev.filter((f) => JSON.stringify(f) !== key) : [...prev, cond],
    )
    setPage(1)
  }

  const isFilterOn = (cond: FilterCond) => extraFilters.some((f) => JSON.stringify(f) === JSON.stringify(cond))

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <Tabs
          items={LAYOUT_TABS}
          value={layout}
          onChange={(v) => setLayout(v as Layout)}
          size="sm"
          ariaLabel="视图布局"
        />
        <span aria-hidden style={{ width: 1, height: 22, background: 'var(--border-alt)' }} />

        {allViews.map((v) => (
          <Tag
            key={v.id}
            selected={viewId === v.id && !trashMode}
            onClick={() => {
              setViewId(v.id)
              setTrashMode(false)
              setPage(1)
              if (v.layout === 'board' || v.layout === 'list' || v.layout === 'table') setLayout(v.layout as Layout)
            }}
          >
            {v.name}
          </Tag>
        ))}
        {QUICK_FILTERS.map((f) => (
          <Tag key={f.label} selected={isFilterOn(f.cond)} onClick={() => toggleQuickFilter(f.cond)}>
            {f.label}
          </Tag>
        ))}
        <Tag selected={trashMode} onClick={() => setTrashMode((v) => !v)}>
          回收站
        </Tag>

        <span style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            共 <Num color="var(--text)">{total}</Num> 条
            {extraFilters.length > 0 && (
              <>
                {' '}
                · 已应用 <Num color="var(--text)">{extraFilters.length}</Num> 个条件
              </>
            )}
          </span>
          <Button variant="primary" size="sm" onClick={() => setShowCreate(true)}>
            ＋ 新增岗位
          </Button>
        </span>
      </div>

      {search && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-muted)' }}>
          搜索：
          <Tag selected onRemove={() => nav('/database')}>
            {search}
          </Tag>
        </div>
      )}

      {selRows.size > 0 && (
        <Card padding="10px 14px" variant="strong">
          <div style={{ display: 'flex', alignItems: 'flex-end', gap: 10, flexWrap: 'wrap' }}>
            <b style={{ fontSize: 13 }}>已选 {selRows.size} 条</b>
            <Input
              placeholder="加标签"
              value={bulkTag}
              size="sm"
              fullWidth={false}
              onChange={(e) => setBulkTag(e.target.value)}
              style={{ width: 150 }}
              aria-label="批量加标签"
            />
            <Select
              options={[
                { value: '', label: '优先级…' },
                { value: 'high', label: '高' },
                { value: 'medium', label: '中' },
                { value: 'low', label: '低' },
              ]}
              value={bulkPriority}
              size="sm"
              fullWidth={false}
              onChange={(e) => setBulkPriority(e.target.value)}
              style={{ width: 120 }}
              aria-label="批量设置优先级"
            />
            <Button
              variant="primary"
              size="sm"
              disabled={(!bulkTag.trim() && !bulkPriority) || bulkMut.isPending}
              onClick={() => bulkMut.mutate()}
            >
              {bulkMut.isPending ? <Spinner size={14} /> : '应用'}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setSelRows(new Set())}>
              取消
            </Button>
          </div>
        </Card>
      )}

      {appsQ.isLoading ? (
        <PageSpinner />
      ) : appsQ.isError ? (
        <EmptyHint>
          <ErrorText>加载失败，请刷新重试</ErrorText>
        </EmptyHint>
      ) : rows.length === 0 ? (
        <EmptyHint>
          <p style={{ margin: 0 }}>{search || extraFilters.length ? '没有符合条件的记录' : '还没有岗位记录'}</p>
          {search || extraFilters.length ? (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                setExtraFilters([])
                nav('/database')
              }}
            >
              清除筛选
            </Button>
          ) : (
            <Button variant="primary" size="sm" onClick={() => setShowCreate(true)}>
              ＋ 新增岗位
            </Button>
          )}
        </EmptyHint>
      ) : layout === 'board' ? (
        <BoardView groups={boardGroups} onOpen={setSelApp} />
      ) : layout === 'list' ? (
        <ListView rows={rows} onOpen={setSelApp} />
      ) : (
        <TableView
          rows={rows}
          selected={selRows}
          trashMode={trashMode}
          onToggle={toggleRow}
          onToggleAll={(checked) => setSelRows(checked ? new Set(rows.map((r) => r.id)) : new Set())}
          onOpen={setSelApp}
          onRestore={(id) => restoreMut.mutate(id)}
        />
      )}

      {total > PAGE_SIZE && (
        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 12 }}>
          <Button variant="ghost" size="sm" disabled={page === 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>
            上一页
          </Button>
          <Num color="var(--text-muted)">第 {page} 页</Num>
          <Button variant="ghost" size="sm" disabled={page * PAGE_SIZE >= total} onClick={() => setPage((p) => p + 1)}>
            下一页
          </Button>
        </div>
      )}

      {showCreate && <CreateDialog onClose={() => setShowCreate(false)} onCreated={() => setShowCreate(false)} />}
      {selApp !== null && <Drawer appId={selApp} onClose={() => setSelApp(null)} />}
    </section>
  )
}

function TableView({
  rows,
  selected,
  trashMode,
  onToggle,
  onToggleAll,
  onOpen,
  onRestore,
}: {
  rows: AppRow[]
  selected: Set<number>
  trashMode: boolean
  onToggle: (id: number) => void
  onToggleAll: (checked: boolean) => void
  onOpen: (id: number) => void
  onRestore: (id: number) => void
}) {
  const allChecked = rows.length > 0 && rows.every((r) => selected.has(r.id))
  return (
    <Card padding={0} style={{ overflow: 'auto' }}>
      <table className="tbl" style={{ minWidth: 980, tableLayout: 'fixed' }}>
        <colgroup>
          <col style={{ width: 36 }} />
          {COLS.map((w, i) => (
            <col key={i} style={{ width: w }} />
          ))}
          {trashMode && <col style={{ width: 80 }} />}
        </colgroup>
        <thead>
          <tr>
            <th>
              <input
                type="checkbox"
                aria-label="选择本页"
                checked={allChecked}
                onChange={(e) => onToggleAll(e.target.checked)}
              />
            </th>
            <th>公司 / 岗位</th>
            <th>阶段推进</th>
            <th>状态</th>
            <th>下一步</th>
            <th>截止</th>
            <th>渠道</th>
            <th>薪资</th>
            <th>优先</th>
            {trashMode && <th>操作</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((a) => {
            const dueDay = a.next_action_due_at ?? a.deadline
            const overdue =
              a.next_action_due_at != null &&
              isDayBeforeToday(a.next_action_due_at) &&
              !['accepted', 'rejected', 'withdrawn', 'closed'].includes(a.status)
            return (
              <tr key={a.id} className="tbl-row" onClick={() => onOpen(a.id)}>
                <td onClick={(e) => e.stopPropagation()}>
                  <input
                    type="checkbox"
                    aria-label={`选择 ${a.company_name} ${a.position}`}
                    checked={selected.has(a.id)}
                    onChange={() => onToggle(a.id)}
                  />
                </td>
                <td>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 9, minWidth: 0 }}>
                    <CompanyMark name={a.company_name} seed={a.id} />
                    <span style={{ minWidth: 0 }}>
                      <span className="ellipsis" style={{ display: 'block', fontSize: 14, fontWeight: 500 }}>
                        {a.company_name}
                      </span>
                      <span
                        className="ellipsis"
                        style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)' }}
                      >
                        {a.position}
                        {a.location ? ` · ${a.location}` : ''}
                      </span>
                    </span>
                  </span>
                </td>
                <td>
                  <StageRail status={a.status} pips={FLOW_PIPS} dates={a.stage_history} />
                </td>
                <td>
                  <StatusChip status={a.status} />
                </td>
                <td className="ellipsis" style={{ fontSize: 13 }}>
                  {a.next_action || <span style={{ color: 'var(--text-muted)' }}>—</span>}
                </td>
                <td>
                  <Num color={overdue ? 'var(--danger)' : 'var(--text-muted)'}>
                    {overdue ? `逾期 ${fmtDay(dueDay)}` : fmtDay(dueDay)}
                  </Num>
                </td>
                <td style={{ fontSize: 12, color: 'var(--text-muted)' }}>{a.channel || '—'}</td>
                <td>
                  <Num>
                    {a.salary_max != null ? `${a.salary_min ?? '—'}–${a.salary_max} ${a.salary_currency}` : '—'}
                  </Num>
                </td>
                <td
                  style={{
                    fontSize: 12,
                    color: a.priority === 'high' ? 'var(--text)' : 'var(--text-muted)',
                    fontWeight: a.priority === 'high' ? 500 : 400,
                  }}
                >
                  {priorityLabel(a.priority)}
                </td>
                {trashMode && (
                  <td onClick={(e) => e.stopPropagation()}>
                    <Button variant="ghost" size="sm" onClick={() => onRestore(a.id)}>
                      恢复
                    </Button>
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
      </table>
    </Card>
  )
}

function BoardView({
  groups,
  onOpen,
}: {
  groups: Array<{ bucket: BoardBucket; items: AppRow[] }>
  onOpen: (id: number) => void
}) {
  return (
    <div className="board">
      {groups.map(({ bucket, items }) => (
        <section key={bucket.title} className="board-col" aria-label={bucket.title}>
          <div className="board-col-head">
            <Dot color={bucket.dot} />
            <span>{bucket.title}</span>
            <span style={{ marginLeft: 'auto' }}>
              <Num color="var(--text-muted)">{items.length}</Num>
            </span>
          </div>
          {items.map((a) => (
            <button key={a.id} className="board-card" onClick={() => onOpen(a.id)}>
              <div className="ellipsis" style={{ fontSize: 13, fontWeight: 500 }}>
                {a.company_name}
              </div>
              <div className="ellipsis" style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>
                {a.position}
              </div>
              <div style={{ marginTop: 9 }}>
                <StageRail status={a.status} pips={FLOW_PIPS} dates={a.stage_history} thin />
              </div>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: 9 }}>
                <Num color="var(--text-muted)">{fmtDay(a.next_action_due_at ?? a.deadline)}</Num>
                <span style={{ fontSize: 10, color: 'var(--text-muted)', border: '1px solid var(--border)', padding: '0 5px' }}>
                  {a.channel || '—'}
                </span>
              </div>
            </button>
          ))}
        </section>
      ))}
    </div>
  )
}

function ListView({ rows, onOpen }: { rows: AppRow[]; onOpen: (id: number) => void }) {
  const groups = useMemo(() => {
    const high = rows.filter((r) => r.priority === 'high')
    const waiting = rows.filter((r) => r.priority !== 'high' && !['accepted', 'rejected', 'withdrawn', 'closed'].includes(r.status))
    const ended = rows.filter((r) => ['accepted', 'rejected', 'withdrawn', 'closed'].includes(r.status) && r.priority !== 'high')
    return [
      { title: '高优先级', dot: 'var(--accent)', items: high },
      { title: '进行中', dot: 'var(--info)', items: waiting },
      { title: '已结束', dot: 'var(--neutral)', items: ended },
    ].filter((g) => g.items.length > 0)
  }, [rows])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {groups.map((g) => (
        <Card key={g.title} padding={0}>
          <div className="panel-head">
            <Dot color={g.dot} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>{g.title}</span>
            <Num color="var(--text-muted)">{g.items.length}</Num>
          </div>
          {g.items.map((a) => (
            <button key={a.id} className="panel-row" onClick={() => onOpen(a.id)}>
              <span className="grow" style={{ display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 }}>
                <span style={{ fontSize: 14, fontWeight: 500 }}>{a.company_name}</span>
                <span className="ellipsis" style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {a.position}
                  {a.location ? ` · ${a.location}` : ''}
                </span>
              </span>
              <span className="ellipsis" style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 220 }}>
                {a.next_action}
              </span>
              <Num color="var(--text-muted)">{fmtDay(a.next_action_due_at ?? a.deadline)}</Num>
            </button>
          ))}
        </Card>
      ))}
    </div>
  )
}

function CreateDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const qc = useQueryClient()
  const [company, setCompany] = useState('')
  const [position, setPosition] = useState('')
  const [location, setLocation] = useState('')
  const [url, setUrl] = useState('')
  // 补录场景：新增时就能声明「已经投了」并填写真实投递时间——时间线显示的
  // 是投递那天，而不是今天录入的日期。
  const [status, setStatus] = useState('saved')
  const [submittedAt, setSubmittedAt] = useState('')
  const [err, setErr] = useState('')
  const [parsing, setParsing] = useState(false)

  // 粘贴 JD 链接 → 自动预填公司/岗位/城市（需求 #3）。解析由服务端发起
  // （已做 SSRF 防护），失败返回 200 + 空字段 → 前端退回手填。
  const prefill = useMutation({
    mutationFn: async () => {
      const u = url.trim()
      if (!u) return
      setParsing(true)
      setErr('')
      try {
        const r = await api.post<{
          company_name: string
          position: string
          location: string
        }>('/api/v1/applications/parse-url', { url: u })
        if (r.company_name) setCompany((c) => c || r.company_name)
        if (r.position) setPosition((c) => c || r.position)
        if (r.location) setLocation((c) => c || r.location)
        if (!r.company_name && !r.position && !r.location) setErr('未能从链接识别出信息，可继续手填')
      } catch (e) {
        setErr(e instanceof ApiError ? e.message : '解析失败，可继续手填')
      } finally {
        setParsing(false)
      }
    },
  })

  const mut = useMutation({
    mutationFn: (b: {
      company_name: string
      position: string
      location?: string
      job_url?: string
      status?: string
      submitted_at?: string
    }) => api.post('/api/v1/applications', b),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      onCreated()
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '创建失败'),
  })

  const canCreate = !mut.isPending && !parsing && company.trim() && position.trim()

  const submitCreate = () => {
    // datetime-local 是无时区挂墙时间：按用户配置时区换算成 UTC 瞬间（与
    // 更新进度弹窗同一规则），避免都柏林浏览器把「昨天 14:00」存成别的日子。
    const zone = effectiveZone() ?? Intl.DateTimeFormat().resolvedOptions().timeZone
    const toInstant = (v: string) => {
      const ms = localDateTimeToInstant(v, zone)
      return ms == null ? undefined : new Date(ms).toISOString()
    }
    mut.mutate({
      company_name: company.trim(),
      position: position.trim(),
      location: location.trim() || undefined,
      job_url: url.trim(),
      status,
      ...(status === 'applied' && submittedAt ? { submitted_at: toInstant(submittedAt) } : {}),
    })
  }

  return (
    <Modal
      title="新增岗位"
      onClose={onClose}
      width={520}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={!canCreate}
            onClick={submitCreate}
          >
            {mut.isPending ? <Spinner size={14} /> : '创建'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div className="field-grid">
        <Input
          id="cf-company"
          label="公司 *"
          value={company}
          onChange={(e) => setCompany(e.target.value)}
          placeholder="公司名称"
          autoFocus
        />
        <Input
          id="cf-pos"
          label="岗位 *"
          value={position}
          onChange={(e) => setPosition(e.target.value)}
          placeholder="岗位名称"
        />
        <Input
          id="cf-loc"
          label="城市"
          value={location}
          onChange={(e) => setLocation(e.target.value)}
          placeholder="解析预填或手填"
        />
        <div className="full">
          <Input
            id="cf-url"
            label="JD 链接"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="粘贴 JD 链接可自动预填公司、岗位与城市"
          />
          {url.trim() && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={parsing}
              onClick={() => prefill.mutate()}
              style={{ marginTop: 6 }}
            >
              {parsing ? <Spinner size={14} /> : '解析预填'}
            </Button>
          )}
        </div>
        <div>
          <Select
            label="当前状态"
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            options={[
              { value: 'saved', label: '待投递' },
              { value: 'preparing', label: '准备材料' },
              { value: 'applied', label: '已投递' },
            ]}
            aria-label="当前状态"
          />
        </div>
        {status === 'applied' && (
          <div>
            <Input
              id="cf-submitted"
              label="投递时间"
              type="datetime-local"
              value={submittedAt}
              onChange={(e) => setSubmittedAt(e.target.value)}
              hint="什么时候投出的；留空按今天算。补录过往投递填真实时间，时间线会显示那天"
            />
          </div>
        )}
      </div>
      <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 'var(--space-4)' }}>
        保存后仍可继续编辑完整信息、上传附件并更新进度。
      </p>
    </Modal>
  )
}
