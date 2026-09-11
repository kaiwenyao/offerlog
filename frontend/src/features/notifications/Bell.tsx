import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, fmtDateTime } from '../../lib/api'
import type { Notification } from '../../lib/types'
import { Button, Card } from '../../ds'
import { Icon } from '../../components/Icon'
import { ErrorText, Num, Spinner } from '../../components/ui'

const KIND_LABEL: Record<string, string> = {
  overdue: '逾期待办',
  interview: '面试提醒',
  assessment_due: 'OA 截止提醒',
  stale: '跟进提醒',
  weekly: '周报',
}

function kindTone(kind: string): string {
  switch (kind) {
    case 'overdue':
      return 'var(--danger)'
    case 'interview':
      return 'var(--accent)'
    case 'assessment_due':
      return 'var(--warning)'
    case 'stale':
      return 'var(--warning)'
    default:
      return 'var(--info)'
  }
}

/**
 * Open (unread) notifications. Shared by the header bell and the sidebar's
 * 通知中心 badge — one react-query entry, so both read the same cache.
 */
function useOpenNotifications() {
  return useQuery({
    queryKey: ['notifications', 'open'],
    queryFn: () => api.get<{ items: Notification[] }>('/api/v1/notifications?open=1'),
    staleTime: 15_000,
    // The app runs with refetchOnWindowFocus off; poll quietly so a reminder
    // generated server-side shows up without requiring a manual refresh.
    refetchInterval: 60_000,
  })
}

/** Unread count only — for the sidebar row. */
export function useUnreadCount(): number {
  return useOpenNotifications().data?.items.length ?? 0
}

export function NotificationsBell() {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const nav = useNavigate()

  // Esc closes the dropdown (keyboard accessibility).
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])

  const q = useOpenNotifications()

  const items = q.data?.items ?? []
  const unread = items.length

  const dismiss = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/notifications/${id}/dismiss`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notifications'] }),
    onError: (e: unknown) => console.error(e),
  })
  const read = useMutation({
    mutationFn: (id: number) => api.post(`/api/v1/notifications/${id}/read`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notifications'] }),
  })

  return (
    <span style={{ position: 'relative', display: 'inline-flex', marginLeft: 4 }}>
      <button
        type="button"
        aria-label={`通知（${unread} 条未读）`}
        aria-expanded={open}
        aria-haspopup="true"
        onClick={() => setOpen((v) => !v)}
        style={{
          position: 'relative',
          all: 'unset',
          boxSizing: 'border-box',
          cursor: 'pointer',
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 34,
          height: 34,
          border: '1px solid var(--border)',
          color: 'var(--text)',
        }}
      >
        <Icon name="bell" size={17} />
        {unread > 0 && (
          <span
            aria-hidden
            style={{
              position: 'absolute',
              top: -5,
              right: -5,
              minWidth: 16,
              height: 16,
              padding: '0 3px',
              background: 'var(--accent)',
              color: 'var(--text-on-accent)',
              fontSize: 10,
              display: 'grid',
              placeItems: 'center',
            }}
          >
            {unread > 9 ? '9+' : unread}
          </span>
        )}
      </button>

      {open && (
        <>
          <span aria-hidden style={{ position: 'fixed', inset: 0, zIndex: 90 }} onClick={() => setOpen(false)} />
          <Card
            variant="strong"
            padding={0}
            style={{ position: 'absolute', right: 0, top: 'calc(100% + 10px)', zIndex: 91, width: 380, maxWidth: '90vw', boxShadow: 'var(--shadow-pop)' }}
          >
            <div className="panel-head" style={{ padding: '10px 14px' }}>
              <b style={{ fontSize: 14 }}>通知</b>
              <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)' }}>
                {q.isLoading ? <Spinner size={13} /> : unread === 0 ? '没有未读' : `${unread} 条未读`}
              </span>
            </div>
            <div style={{ maxHeight: 380, overflowY: 'auto' }}>
              {q.isError ? (
                <div style={{ padding: 14 }}>
                  <ErrorText>通知加载失败</ErrorText>
                </div>
              ) : items.length === 0 ? (
                <div style={{ padding: '16px 14px', fontSize: 13, color: 'var(--text-muted)' }}>
                  没有未读通知。逾期待办、明天面试和投递后未回复会自动出现在这里。
                </div>
              ) : (
                items.map((n) => (
                  <div key={n.id} className="panel-row" style={{ alignItems: 'flex-start', padding: '10px 14px' }}>
                    <span
                      aria-hidden
                      style={{ width: 7, height: 7, marginTop: 6, background: kindTone(n.kind), flex: '0 0 auto' }}
                    />
                    <span className="grow" style={{ minWidth: 0 }}>
                      <span style={{ display: 'block', fontSize: 13, fontWeight: 500 }}>
                        {n.title}
                        <span style={{ marginLeft: 8, fontSize: 11, color: 'var(--text-muted)', fontWeight: 400 }}>
                          {KIND_LABEL[n.kind] ?? n.kind}
                        </span>
                      </span>
                      <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2, lineHeight: 1.45 }}>
                        {n.body}
                      </span>
                      <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>
                        {fmtDateTime(n.created_at)}
                      </span>
                    </span>
                    <span style={{ display: 'flex', flexDirection: 'column', gap: 4, flex: '0 0 auto' }}>
                      {n.application_id && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            read.mutate(n.id)
                            setOpen(false)
                            nav(`/apps/${n.application_id}`)
                          }}
                        >
                          查看
                        </Button>
                      )}
                      <Button variant="ghost" size="sm" onClick={() => dismiss.mutate(n.id)}>
                        忽略
                      </Button>
                    </span>
                  </div>
                ))
              )}
            </div>
            <div
              style={{
                borderTop: '1px solid var(--border)',
                padding: '8px 14px',
                display: 'flex',
                justifyContent: 'center',
              }}
            >
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setOpen(false)
                  nav('/notifications')
                }}
              >
                查看全部通知
              </Button>
            </div>
          </Card>
        </>
      )}
    </span>
  )
}
