import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtBytes, fmtDate } from '../../lib/api'
import type { FileItem } from '../../lib/types'
import { Button, Card, LinkButton, Tag } from '../../ds'
import { Icon } from '../../components/Icon'
import { EmptyHint, ErrorText, Num, PageSpinner, Spinner } from '../../components/ui'
import { FileTile } from '../database/tabs'

const ACCEPTED_UPLOADS = '.pdf,.docx,.txt,.png,.jpg,.jpeg'
const QUOTA_BYTES = 5 * 1024 * 1024 * 1024

const CATEGORIES: Array<{ key: string; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'resume', label: '简历' },
  { key: 'cover_letter', label: '求职信' },
  { key: 'portfolio', label: '作品集' },
  { key: 'other', label: '其它' },
]

function categoryLabel(key: string): string {
  return CATEGORIES.find((c) => c.key === key)?.label ?? '其它'
}

export function FilesPage() {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const [filter, setFilter] = useState('all')

  const q = useQuery({ queryKey: ['files'], queryFn: () => api.get<{ items: FileItem[] }>('/api/v1/files') })

  const del = useMutation({
    mutationFn: (id: string) => api.del(`/api/v1/files/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '删除失败'),
  })

  const up = useMutation({
    mutationFn: (f: File) => {
      const fd = new FormData()
      fd.append('file', f)
      fd.append('category', 'other')
      return api.post('/api/v1/files', fd, true)
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '上传失败'),
  })

  const files = useMemo(() => q.data?.items ?? [], [q.data])
  const counts = useMemo(() => {
    const map: Record<string, number> = { all: files.length }
    for (const f of files) map[f.category] = (map[f.category] ?? 0) + 1
    return map
  }, [files])

  const used = files.reduce((sum, f) => sum + f.size_bytes, 0)
  const shown = filter === 'all' ? files : files.filter((f) => f.category === filter)

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        {CATEGORIES.map((c) => (
          <Tag key={c.key} selected={filter === c.key} onClick={() => setFilter(c.key)}>
            {c.label} {counts[c.key] ?? 0}
          </Tag>
        ))}
        <span style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, color: 'var(--text-muted)' }}>
          <span className="meter slim" style={{ width: 120, flex: '0 0 auto' }}>
            <span style={{ width: `${Math.min(100, (used / QUOTA_BYTES) * 100)}%`, background: 'var(--accent)' }} />
          </span>
          已用 <Num color="var(--text)">{fmtBytes(used)}</Num> / 5 GB
          <label style={{ display: 'inline-flex' }}>
            <Button variant="primary" size="sm" onClick={() => document.getElementById('files-upload')?.click()}>
              {up.isPending ? <Spinner size={14} /> : <Icon name="upload" size={14} />}＋ 上传文件
            </Button>
            <input
              id="files-upload"
              type="file"
              style={{ display: 'none' }}
              accept={ACCEPTED_UPLOADS}
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) up.mutate(f)
                e.target.value = ''
              }}
            />
          </label>
        </span>
      </div>

      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
        私有存储：PDF/DOCX/TXT/PNG/JPEG，单文件 ≤ 20 MiB。下载需登录并校验归属。
      </p>

      {err && <ErrorText>{err}</ErrorText>}

      {q.isLoading ? (
        <PageSpinner />
      ) : shown.length === 0 ? (
        <EmptyHint>
          <p style={{ margin: 0 }}>{files.length === 0 ? '还没有文件' : '这个分类下还没有文件'}</p>
          <p style={{ margin: 0, fontSize: 13 }}>从岗位详情或这里上传简历等文件。</p>
        </EmptyHint>
      ) : (
        <Card padding={0}>
          <div className="tbl-scroll">
            <table className="tbl" style={{ minWidth: 780 }}>
              <thead>
                <tr>
                  <th>文件</th>
                  <th style={{ width: 96 }}>类别</th>
                  <th style={{ width: 92 }}>大小</th>
                  <th style={{ width: 180 }}>关联岗位</th>
                  <th style={{ width: 108 }}>上传时间</th>
                  <th style={{ width: 130 }} />
                </tr>
              </thead>
              <tbody>
                {shown.map((f) => (
                  <tr key={f.id}>
                    <td>
                      <span style={{ display: 'flex', alignItems: 'center', gap: 9, minWidth: 0 }}>
                        <FileTile name={f.name} />
                        <span className="ellipsis" style={{ fontSize: 14 }}>
                          {f.name}
                        </span>
                      </span>
                    </td>
                    <td style={{ fontSize: 12, color: 'var(--text-muted)' }}>{categoryLabel(f.category)}</td>
                    <td>
                      <Num>{fmtBytes(f.size_bytes)}</Num>
                    </td>
                    <td className="ellipsis" style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                      {f.application_id ? `#${f.application_id}` : '—'}
                    </td>
                    <td>
                      <Num color="var(--text-muted)">{fmtDate(f.created_at)}</Num>
                    </td>
                    <td>
                      <span style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                        {f.status === 'ready' && (
                          <LinkButton variant="ghost" size="sm" href={`/api/v1/files/${f.id}/download`} download>
                            下载
                          </LinkButton>
                        )}
                        <Button variant="ghost" size="sm" disabled={del.isPending} onClick={() => del.mutate(f.id)}>
                          删除
                        </Button>
                      </span>
                    </td>
                  </tr>
                ))}
                </tbody>
              </table>
          </div>
        </Card>
      )}
    </section>
  )
}
