import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, fmtBytes, fmtDate } from '../../lib/api'
import type { FileItem } from '../../lib/types'
import { EmptyHint, Spinner } from '../../components/ui'

export function FilesPage() {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const q = useQuery({
    queryKey: ['files'],
    queryFn: () => api.get<{ items: FileItem[] }>('/api/v1/files'),
  })
  const del = useMutation({
    mutationFn: (id: string) => api.del(`/api/v1/files/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['files'] }),
    onError: (e) => setErr(e instanceof ApiError ? e.message : '删除失败'),
  })
  const up = useMutation({
    mutationFn: async (f: File) => {
      const fd = new FormData()
      fd.append('file', f)
      fd.append('category', 'other')
      return api.post('/api/v1/files', fd, true)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['files'] })
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : '上传失败'),
  })
  return (
    <div>
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <h1 className="display" style={{ fontSize: 24 }}>文件库</h1>
        <label className="btn btn-primary" style={{ cursor: 'pointer' }}>
          {up.isPending ? <Spinner /> : '＋ 上传文件'}
          <input
            type="file"
            style={{ display: 'none' }}
            accept=".pdf,.docx,.txt,.png,.jpg,.jpeg"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) up.mutate(f)
              e.target.value = ''
            }}
          />
        </label>
      </div>
      <p className="small muted">私有存储：PDF/DOCX/TXT/PNG/JPEG，单文件 ≤ 20 MiB。下载需登录并校验归属。</p>
      {err && <p role="alert" className="err">{err}</p>}
      {q.isLoading ? (
        <Spinner />
      ) : (q.data?.items ?? []).length === 0 ? (
        <EmptyHint>
          <p style={{ marginTop: 0 }}>还没有文件</p>
          <p className="small">从岗位详情或这里上传简历等文件。</p>
        </EmptyHint>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }} className="mt16">
          {(q.data?.items ?? []).map((f) => (
            <div key={f.id} className="card row" style={{ padding: 12, justifyContent: 'space-between' }}>
              <div className="grow">
                <div style={{ fontWeight: 600 }}>{f.name}</div>
                <div className="small muted">
                  {fmtBytes(f.size_bytes)} · {f.category === 'resume' ? '简历' : '其他'}
                  {f.application_id ? ` · 关联岗位 #${f.application_id}` : ''} · {fmtDate(f.created_at)}
                </div>
                <div className="small muted num">SHA-256: {f.sha256.slice(0, 16)}…</div>
              </div>
              <div className="row">
                {f.status === 'ready' && (
                  <a className="btn btn-ghost btn-small" href={`/api/v1/files/${f.id}/download`} download>下载</a>
                )}
                <button className="btn btn-ghost btn-small" disabled={del.isPending} onClick={() => del.mutate(f.id)}>删除</button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
