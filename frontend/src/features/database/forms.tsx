import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import type { AppRow } from '../../lib/types'
import { NEXT_STEP_SUGGESTION } from '../../lib/status'
import { Button, Input, Select, Textarea } from '../../ds'
import { ErrorText, Modal, Spinner } from '../../components/ui'

const ROUNDS = ['一面', '二面', '三面', '终面', '技术面', 'HR 面', '其他']
const FORMATS = [
  { value: 'phone', label: '电话' },
  { value: 'video', label: '视频' },
  { value: 'onsite', label: '到面' },
  { value: 'takehome', label: '作业' },
]

export function InterviewForm({
  appId,
  onClose,
  onDone,
}: {
  appId: number
  onClose: () => void
  onDone: () => void
}) {
  const [roundName, setRoundName] = useState('一面')
  const [format, setFormat] = useState('video')
  const [scheduled, setScheduled] = useState('')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: () =>
      api.post(`/api/v1/applications/${appId}/interviews`, {
        round_name: roundName,
        format,
        scheduled_at: scheduled ? new Date(scheduled).toISOString() : null,
      }),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="安排面试"
      onClose={onClose}
      footer={
        <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
          {mut.isPending ? <Spinner size={14} /> : '保存'}
        </Button>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Select label="轮次" options={ROUNDS} value={roundName} onChange={(e) => setRoundName(e.target.value)} />
        <Select label="形式" options={FORMATS} value={format} onChange={(e) => setFormat(e.target.value)} />
        <Input
          label="时间"
          type="datetime-local"
          value={scheduled}
          onChange={(e) => setScheduled(e.target.value)}
        />
      </div>
    </Modal>
  )
}

export function ActionForm({ app, onClose, onDone }: { app: AppRow; onClose: () => void; onDone: () => void }) {
  const [title, setTitle] = useState(app.next_action || NEXT_STEP_SUGGESTION[app.status] || '')
  const [due, setDue] = useState(app.next_action_due_at ? app.next_action_due_at.slice(0, 10) : '')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: async () => {
      // Save onto the application row (the today dashboard reads next_action)
      // and create a standalone action for the checklist. Re-read the row first
      // so a concurrent edit elsewhere does not trigger a 409 conflict.
      const fresh = await api.get<AppRow>(`/api/v1/applications/${app.id}`)
      const dueIso = due ? new Date(`${due}T00:00:00`).toISOString() : null
      await api.patch(`/api/v1/applications/${app.id}`, {
        version: fresh.version,
        next_action: title || null,
        next_action_due_at: dueIso,
      })
      if (title) {
        await api.post(`/api/v1/applications/${app.id}/actions`, { title, due_date: dueIso })
      }
    },
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })

  return (
    <Modal
      title="设置下一步行动"
      onClose={onClose}
      footer={
        <Button variant="primary" size="sm" onClick={() => mut.mutate()} disabled={mut.isPending}>
          {mut.isPending ? <Spinner size={14} /> : '保存'}
        </Button>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Input label="行动内容" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="例如：准备二面" />
        <Input label="截止日期" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
      </div>
    </Modal>
  )
}

export function NoteForm({ appId, onClose, onDone }: { appId: number; onClose: () => void; onDone: () => void }) {
  const [content, setContent] = useState('')
  const [err, setErr] = useState('')
  const mut = useMutation({
    mutationFn: () => api.post(`/api/v1/applications/${appId}/notes`, { content_md: content }),
    onSuccess: onDone,
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '保存失败'),
  })
  return (
    <Modal
      title="记录备注"
      onClose={onClose}
      footer={
        <Button variant="primary" size="sm" disabled={!content.trim() || mut.isPending} onClick={() => mut.mutate()}>
          {mut.isPending ? <Spinner size={14} /> : '保存'}
        </Button>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <Textarea
        rows={6}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder="沟通要点、联系人、后续计划…"
      />
    </Modal>
  )
}
