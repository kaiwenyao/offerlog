import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, fmtDate, ApiError } from '../../lib/api'
import type { ActionItem, AppRow } from '../../lib/types'
import { EmptyHint, Spinner } from '../../components/ui'
import { StatusChip } from '../../components/ui'
import { useNavigate } from 'react-router-dom'
import { useState } from 'react'

interface TodayData {
  items: Array<
    ActionItem & { company_name?: string; position?: string; status?: string }
  >
}

function groupActions(actions: TodayData['items']) {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const weekEnd = today + 7 * 86400000
  const groups: { title: string; items: TodayData['items'] }[] = [
    { title: '已逾期', items: [] },
    { title: '今天', items: [] },
    { title: '未来 7 天', items: [] },
    { title: '更晚', items: [] },
  ]
  for (const a of actions) {
    const dueIso = a.due_ts ?? a.due_date
    const due = dueIso ? new Date(dueIso).getTime() : Infinity
    if (due === Infinity) groups[3].items.push(a)
    else if (due < today) groups[0].items.push(a)
    else if (due < today + 86400000) groups[1].items.push(a)
    else if (due <= weekEnd) groups[2].items.push(a)
    else groups[3].items.push(a)
  }
  return groups.filter((g) => g.items.length > 0)
}

export function TodayPage() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [toast, setToast] = useState('')
  const todayQ = useQuery({
    queryKey: ['actions', 'open'],
    queryFn: () => api.get<{ items: Array<ActionItem & { company_name?: string }> }>('/api/v1/actions?open=1'),
  })
  // Use the dashboard query: applications with next_action set
  const appsQ = useQuery({
    queryKey: ['apps', 'list', { page: 1, size: 200 }],
    queryFn: () => api.get<{ items: AppRow[] }>('/api/v1/applications?page=1&page_size=200'),
  })

  const doneMut = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/actions/${id}/done`, { done: true }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['today'] })
      qc.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (e) => setToast(e instanceof ApiError ? e.message : '操作失败'),
  })

  if (appsQ.isLoading || todayQ.isLoading) return <Spinner />
  const apps = (appsQ.data?.items ?? []).filter((a) => a.next_action && !a.archived)
  const toShow = apps.filter((a) => {
    const due = a.next_action_due_at ? new Date(a.next_action_due_at).getTime() : Infinity
    return due < Date.now() + 8 * 86400000
  })
  const { items: actions } = todayQ.data ?? { items: [] }
  const actionList = actions ?? []
  const merged: TodayData['items'] = [
    ...toShow.map((a) => ({
      id: -a.id,
      application_id: a.id,
      title: a.next_action,
      due_date: a.next_action_due_at,
      due_ts: null,
      done_at: null,
      remind_me: false,
      created_at: a.created_at,
      company_name: a.company_name,
      position: a.position,
      status: a.status,
    })),
    ...actionList.map((a) => ({ ...a })),
  ]
  const groups = groupActions(merged)
  const todayStamp = new Date().toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    weekday: 'long',
  })
  // 逾期 = due strictly before today; due-today stays calm (it is the 今天 group)
  const overdueBefore = new Date().setHours(0, 0, 0, 0)

  return (
    <div>
      <div className="page-head">
        <div>
          <div className="eyebrow">Today</div>
          <h1 className="display">今日待办</h1>
          <p className="page-sub small">逾期、今天与未来 7 天的下一步行动。完成后一键标记，或进入详情改期。</p>
        </div>
        <span className="stamp" aria-label="今天日期">
          {todayStamp}
        </span>
      </div>
      {toast && <p role="alert" className="err">{toast}</p>}
      {groups.length === 0 && (
        <EmptyHint>
          <p style={{ marginTop: 0 }}>今天没有待办 🎉</p>
          <p className="small">在数据库中给岗位填写“下一步行动”与截止时间，就会出现在这里。</p>
          <button className="btn btn-primary mt8" onClick={() => nav('/database')}>去添加岗位</button>
        </EmptyHint>
      )}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        {groups.map((g) => (
          <section
            key={g.title}
            className={'card today-group' + (g.title === '已逾期' ? ' is-overdue' : '')}
          >
            <h2 className="group-label panel-title" style={{ fontSize: 14 }}>
              {g.title === '已逾期' && <span aria-hidden style={{ color: 'var(--seal)' }}>●</span>}
              {g.title} <span className="count-badge num">{g.items.length}</span>
            </h2>
            <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {g.items.map((a) => (
                <li key={a.id} className="today-item">
                  <div className="grow">
                    <div className="row" style={{ gap: 8 }}>
                      {a.company_name && (
                        <button
                          className="linklike"
                          onClick={() => a.application_id && nav(`/apps/${a.application_id}`)}
                        >
                          {a.company_name}
                          {a.position ? ` · ${a.position}` : ''}
                        </button>
                      )}
                      {a.status && <StatusChip status={a.status} />}
                    </div>
                    <div style={{ marginTop: 3 }}>{a.title}</div>
                    <div className="small" style={{ marginTop: 3 }}>
                      {(a.due_ts ?? a.due_date) && new Date(a.due_ts ?? a.due_date!).getTime() < overdueBefore ? (
                        <span className="stamp is-overdue">逾期 · {fmtDate(a.due_ts ?? a.due_date)}</span>
                      ) : (
                        <span className="stamp">截止 {fmtDate(a.due_ts ?? a.due_date)}</span>
                      )}
                    </div>
                  </div>
                  {a.id < 0 ? (
                    <button
                      className="btn btn-ghost btn-small"
                      onClick={() => a.application_id && nav(`/apps/${a.application_id}`)}
                    >
                      更新
                    </button>
                  ) : (
                    <button
                      className="btn btn-primary btn-small"
                      disabled={doneMut.isPending}
                      onClick={() => doneMut.mutate(a.id)}
                    >
                      完成
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </div>
  )
}
