import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, fmtDateTime } from '../../lib/api'
import type { Notification } from '../../lib/types'
import { Button, Card, Tabs } from '../../ds'
import { ErrorText, Num, PageSpinner, Spinner } from '../../components/ui'

const KIND_LABEL: Record<string, string> = {
  overdue: '逾期待办',
  interview: '面试提醒',
  stale: '跟进提醒',
  weekly: '周报',
}

function kindTone(kind: string): string {
  switch (kind) {
    case 'overdue':
      return 'var(--danger)'
    case 'interview':
      return 'var(--accent)'
    case 'stale':
      return 'var(--warning)'
    default:
      return 'var(--info)'
  }
}

export function NotificationsPage() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [scope, setScope] = useState<'open' | 'all'>('open')

  const q = useQuery({
    queryKey: ['notifications', scope],
    queryFn: () => api.get<{ items: Notification[] }>(`/api/v1/notifications${scope === 'open' ? '?open=1' : ''}`),
    refetchInterval: 60_000,
  })

  const readAll = useMutation({
    mutationFn: () => api.post('/api/v1/notifications/read-all'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notifications'] }),
  })
  const markRead = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/notifications/${id}/read`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notifications'] }),
  })
  const dismiss = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/notifications/${id}/dismiss`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notifications'] }),
  })

  const items = q.data?.items ?? []

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <Tabs
          items={[
            { value: 'open', label: '未读' },
            { value: 'all', label: '全部' },
          ]}
          value={scope}
          onChange={(v) => setScope(v as 'open' | 'all')}
          ariaLabel="通知范围"
        />
        {scope === 'open' && items.length > 0 && (
          <Button variant="secondary" size="sm" disabled={readAll.isPending} onClick={() => readAll.mutate()}>
            {readAll.isPending ? <Spinner size={14} /> : '全部标为已读'}
          </Button>
        )}
      </div>

      {q.isLoading ? (
        <PageSpinner />
      ) : q.isError ? (
        <Card padding="18px">
          <ErrorText>通知加载失败</ErrorText>
          <Button variant="secondary" size="sm" onClick={() => q.refetch()} style={{ marginTop: 10 }}>
            重试
          </Button>
        </Card>
      ) : items.length === 0 ? (
        <Card padding="18px">
          <p style={{ margin: 0 }}>{scope === 'open' ? '没有未读通知 🎉' : '还没有通知'}</p>
          <p style={{ margin: '6px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
            逾期待办、明天面试和投递后未回复会自动生成提醒；可在「设置 → 提醒」里调整。
          </p>
        </Card>
      ) : (
        <Card padding={0} style={{ overflow: 'hidden' }}>
          {items.map((n) => (
            <div key={n.id} className="panel-row" style={{ alignItems: 'flex-start' }}>
              <span aria-hidden style={{ width: 8, height: 8, borderRadius: '50%', marginTop: 6, background: kindTone(n.kind), flex: '0 0 auto' }} />
              <span className="grow" style={{ minWidth: 0 }}>
                <span style={{ display: 'block', fontSize: 14, fontWeight: 500 }}>
                  {n.title}
                  <span style={{ color: 'var(--text-muted)', fontSize: 11, fontWeight: 400 }}>
                    · {KIND_LABEL[n.kind] ?? n.kind} · {fmtDateTime(n.created_at)}
                  </span>
                </span>
                <span style={{ display: 'block', fontSize: 13, color: 'var(--text-muted)', marginTop: 2, lineHeight: 1.5 }}>
                  {n.body}
                </span>
              </span>
              <span style={{ display: 'flex', gap: 6, flex: '0 0 auto' }}>
                {n.application_id && (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => {
                      markRead.mutate(n.id)
                      nav(`/apps/${n.application_id}`)
                    }}
                  >
                    查看岗位
                  </Button>
                )}
                {scope === 'open' && (
                  <Button variant="ghost" size="sm" onClick={() => dismiss.mutate(n.id)}>
                    忽略
                  </Button>
                )}
              </span>
            </div>
          ))}
        </Card>
      )}
    </section>
  )
}
