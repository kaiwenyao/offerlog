import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, localDateTimeToInstant } from '../../lib/api'
import { effectiveZone } from '../../lib/tz'
import { STATUSES } from '../../lib/status'
import { Button, Input, Select } from '../../ds'
import { ErrorText, Modal, Spinner, StatusChip } from '../../components/ui'

const ENDED_KEYS = ['accepted', 'rejected', 'withdrawn', 'closed']
const RECRUITING_KEYS = ['applied', 'screening', 'assessment', 'interviewing']

const TRANSITIONS: Record<string, string[]> = {
  saved: ['preparing', 'applied', 'withdrawn', 'closed'],
  preparing: ['applied', 'screening', 'withdrawn', 'closed', 'saved'],
  applied: ['screening', 'assessment', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
  screening: ['assessment', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
  assessment: ['screening', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
  interviewing: ['screening', 'offer', 'rejected', 'withdrawn', 'closed'],
  offer: ['accepted', 'rejected', 'withdrawn', 'closed'],
  accepted: ['offer', 'rejected', 'withdrawn'],
  rejected: ['saved', 'preparing', 'applied', 'screening', 'interviewing', 'offer'],
  withdrawn: ['saved', 'preparing', 'applied', 'screening', 'interviewing', 'offer'],
  closed: ['saved', 'preparing', 'applied', 'screening', 'interviewing', 'offer'],
}

export function allowedTargets(from: string) {
  const keys = TRANSITIONS[from] ?? []
  return STATUSES.filter((s) => keys.includes(s.key))
}

export function TransitionModal({
  appId,
  currentStatus,
  version,
  onClose,
}: {
  appId: number
  currentStatus: string
  version: number
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [to, setTo] = useState('')
  const [occurredAt, setOccurredAt] = useState('')
  const [submittedAt, setSubmittedAt] = useState('')
  const [reason, setReason] = useState('')
  const [note, setNote] = useState('')
  const [err, setErr] = useState('')

  const mut = useMutation({
    mutationFn: () => {
      // occurred_at / submitted_at come from datetime-local inputs: naive
      // wall-clock strings with no zone. Interpret them in the USER's zone
      // (same rule as the interview scheduler) so a Dublin browser + Shanghai
      // user entering 09:00 does not store 09:00 Dublin = 17:00 Shanghai.
      const zone = effectiveZone() ?? Intl.DateTimeFormat().resolvedOptions().timeZone
      const toInstant = (v: string) => {
        const ms = localDateTimeToInstant(v, zone)
        return ms == null ? null : new Date(ms).toISOString()
      }
      return api.post(`/api/v1/applications/${appId}/transitions`, {
        to_status: to,
        version,
        occurred_at: occurredAt ? toInstant(occurredAt) : null,
        reason,
        note,
        submitted_at: submittedAt ? toInstant(submittedAt) : null,
        idempotency_key: `ui-${Date.now()}`,
      })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app', appId] })
      qc.invalidateQueries({ queryKey: ['events', appId] })
      qc.invalidateQueries({ queryKey: ['apps'] })
      onClose()
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : '更新失败'),
  })

  const needsReason =
    ENDED_KEYS.includes(to) || (ENDED_KEYS.includes(currentStatus) && to !== '' && !ENDED_KEYS.includes(to))
  const needsSubmitted = RECRUITING_KEYS.includes(to)

  return (
    <Modal
      title="更新进度"
      onClose={onClose}
      width={480}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button variant="primary" size="sm" disabled={!to || mut.isPending} onClick={() => mut.mutate()}>
            {mut.isPending ? <Spinner size={14} /> : '确认更新'}
          </Button>
        </>
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ font: 'var(--type-caption)', color: 'var(--text-muted)' }}>当前</span>
          <StatusChip status={currentStatus} />
        </div>

        <Select
          label="目标状态 *"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          options={allowedTargets(currentStatus).map((s) => ({ value: s.key, label: s.label }))}
        >
          <option value="">选择…</option>
        </Select>

        <Input
          label="发生时间（可选，默认现在）"
          type="datetime-local"
          value={occurredAt}
          onChange={(e) => setOccurredAt(e.target.value)}
        />

        {needsSubmitted && (
          <Input
            label="实际投递时间"
            type="datetime-local"
            value={submittedAt}
            onChange={(e) => setSubmittedAt(e.target.value)}
            hint="进入招聘阶段需要补充投递时间（或标记未经正式投递）"
          />
        )}

        {needsReason && (
          <Input label="原因 *" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="必填" />
        )}

        <Input label="说明（可选）" value={note} onChange={(e) => setNote(e.target.value)} />
      </div>
    </Modal>
  )
}
