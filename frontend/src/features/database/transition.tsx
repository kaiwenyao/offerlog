import { useMemo, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import { toInstantInUserZone } from '../../lib/tz'
import { ENDED, statusMeta } from '../../lib/status'
import {
  needsReason as reasonRequired,
  RECRUITING_KEYS,
  REASON_PRESETS,
  REOPEN_REASON_PRESETS,
  SKIP_SUBMISSION_TARGETS,
  suggestedTargets,
  targetGroups,
} from '../../lib/transitions'
import type { Interview } from '../../lib/types'
import { Button, Card, Checkbox, Eyebrow, Listbox, Tag, Textarea, Input } from '../../ds'
import { ErrorText, Modal, Spinner, StatusChip } from '../../components/ui'
import { FORMATS, ROUNDS } from './forms'

export interface TransitionModalProps {
  appId: number
  currentStatus: string
  version: number
  /** Null when the record has no recorded submission yet — drives the 投递时间 ask. */
  submittedAt: string | null
  /** Existing rounds, used to guess which round the user is about to schedule. */
  interviews: Interview[]
  onClose: () => void
}

/** Rounds are named 一面/二面/…; guess the next one from how many exist. */
function guessRound(count: number): string {
  return ROUNDS[Math.min(count, ROUNDS.length - 2)]
}

export function TransitionModal({
  appId,
  currentStatus,
  version,
  submittedAt,
  interviews,
  onClose,
}: TransitionModalProps) {
  const qc = useQueryClient()
  const [to, setTo] = useState('')
  const [occurredAt, setOccurredAt] = useState('')
  const [submitted, setSubmitted] = useState('')
  const [noFormalSubmission, setNoFormalSubmission] = useState(false)
  const [reason, setReason] = useState('')
  const [note, setNote] = useState('')
  const [err, setErr] = useState('')
  // The status change is durable well before the mutation settles, but the user
  // must not be able to dismiss the dialog in that window — they would never see
  // the partial-failure message. So the flag lives in a ref while the request is
  // in flight, and only becomes UI state once the second write has actually
  // failed. At that point the transition is NOT retryable (it would 409 /
  // same_status), so the footer collapses to a single 关闭.
  const statusCommitted = useRef(false)
  const [partialFailure, setPartialFailure] = useState(false)

  // Inline interview round (only when advancing to 面试中).
  const [roundName, setRoundName] = useState(() => guessRound(interviews.length))
  const [format, setFormat] = useState('video')
  const [scheduled, setScheduled] = useState('')

  const isReopen = ENDED.has(currentStatus)
  const groups = useMemo(() => targetGroups(currentStatus), [currentStatus])
  const suggestions = useMemo(() => suggestedTargets(currentStatus), [currentStatus])

  const needsReason = reasonRequired(currentStatus, to)
  // Only ask for a submission time when the record genuinely has none. A record
  // that was already submitted must not be re-asked on every later stage.
  const needsSubmitted = RECRUITING_KEYS.includes(to) && !submittedAt
  // 已投递 always needs a real time — offering the escape hatch there would
  // produce a row that reads as submitted but counts as unsubmitted everywhere.
  const canSkipSubmission = SKIP_SUBMISSION_TARGETS.includes(to)
  const isInterviewing = to === 'interviewing'
  // Prefer the target's own presets (毁约 from 已接受 wants the 撤回 reasons,
  // not the reopen ones); fall back to reopen copy for a pipeline target.
  const reasonPresets = REASON_PRESETS[to] ?? (isReopen ? REOPEN_REASON_PRESETS : [])

  const canSubmit =
    to !== '' && (!needsReason || reason.trim() !== '') && (!needsSubmitted || noFormalSubmission || submitted !== '')

  const mut = useMutation({
    mutationFn: async () => {
      const occurred = occurredAt ? toInstantInUserZone(occurredAt) : null
      if (occurredAt && occurred?.iso == null) throw new ApiError('bad_occurred_at', '发生时间格式不正确', 400)
      const skipSubmission = canSkipSubmission && noFormalSubmission
      const submittedInstant = !skipSubmission && submitted ? toInstantInUserZone(submitted) : null
      if (submitted && !skipSubmission && submittedInstant?.iso == null) {
        throw new ApiError('bad_submitted_at', '投递时间格式不正确', 400)
      }

      await api.post(`/api/v1/applications/${appId}/transitions`, {
        to_status: to,
        version,
        occurred_at: occurred?.iso ?? null,
        reason,
        note,
        submitted_at: submittedInstant?.iso ?? null,
        no_formal_submission: skipSubmission,
        idempotency_key: `ui-${Date.now()}`,
      })
      statusCommitted.current = true

      // Second, independent write. The status change is already durable, so a
      // failure here must be reported as a partial success — never swallowed,
      // never retried as one unit.
      if (isInterviewing && scheduled) {
        const { iso, zone } = toInstantInUserZone(scheduled)
        if (iso == null) {
          throw new ApiError('bad_scheduled_at', '面试时间格式不正确', 400)
        }
        await api.post(`/api/v1/applications/${appId}/interviews`, {
          round_name: roundName,
          format,
          scheduled_at: iso,
          timezone: zone,
        })
      }
    },
    onSuccess: () => {
      invalidate()
      onClose()
    },
    onError: (e: unknown) => {
      // A network-level failure has no server message; nesting the generic
      // 「更新失败」 inside 「…创建失败：」 reads as nonsense, so each branch
      // gets its own fallback.
      const apiMsg = e instanceof ApiError ? e.message : null
      if (statusCommitted.current) {
        // Refresh anyway: the status really did change.
        invalidate()
        setPartialFailure(true)
        setErr(
          `状态已更新为「${statusMeta(to).label}」，但面试轮次创建失败：${apiMsg ?? '网络错误或服务无响应'}。可在「概览」标签重新安排。`,
        )
      } else {
        setErr(apiMsg ?? '更新失败')
      }
    },
  })

  function invalidate() {
    qc.invalidateQueries({ queryKey: ['app', appId] })
    qc.invalidateQueries({ queryKey: ['events', appId] })
    qc.invalidateQueries({ queryKey: ['interviews', appId] })
    qc.invalidateQueries({ queryKey: ['apps'] })
  }

  const title = isReopen ? '重开 / 更正' : '更新进度'

  return (
    <Modal
      title={title}
      onClose={onClose}
      width={520}
      footer={
        partialFailure ? (
          <Button variant="primary" size="sm" onClick={onClose}>
            关闭
          </Button>
        ) : (
          <>
            {/* Disabled while in flight: the status write may already have
                landed, and dismissing now would hide a partial failure. */}
            <Button variant="ghost" size="sm" disabled={mut.isPending} onClick={onClose}>
              取消
            </Button>
            <Button variant="primary" size="sm" disabled={!canSubmit || mut.isPending} onClick={() => mut.mutate()}>
              {mut.isPending ? <Spinner size={14} /> : '确认更新'}
            </Button>
          </>
        )
      }
    >
      {err && <ErrorText>{err}</ErrorText>}
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-4)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ font: 'var(--type-caption)', color: 'var(--text-muted)' }}>当前</span>
          <StatusChip status={currentStatus} />
        </div>

        {suggestions.length > 0 && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 'var(--space-2)',
            }}
          >
            <Eyebrow>常用</Eyebrow>
            <div
              style={{
                display: 'flex',
                flexWrap: 'wrap',
                gap: 'var(--space-2)',
              }}
            >
              {suggestions.map((s) => (
                <Tag key={s.key} selected={to === s.key} onClick={() => setTo(s.key)}>
                  {s.icon} {s.label}
                </Tag>
              ))}
            </div>
          </div>
        )}

        <Listbox
          label="目标状态 *"
          value={to}
          onChange={setTo}
          groups={groups}
          placeholder="选择…"
          triggerClassName="status-target-trigger"
        />

        <Input
          label="发生时间"
          type="datetime-local"
          value={occurredAt}
          onChange={(e) => setOccurredAt(e.target.value)}
          hint="这件事什么时候发生的；留空默认为现在"
        />

        {needsSubmitted && (
          <Card variant="outline" padding="10px 12px">
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 'var(--space-3)',
              }}
            >
              {!(canSkipSubmission && noFormalSubmission) && (
                <Input
                  label="实际投递时间 *"
                  type="datetime-local"
                  value={submitted}
                  onChange={(e) => setSubmitted(e.target.value)}
                  hint="这份申请什么时候投出的；进入招聘阶段需要它来计算等待天数"
                />
              )}
              {canSkipSubmission && (
                <Checkbox
                  label="未经正式投递（内推 / 猎头直接约面）"
                  checked={noFormalSubmission}
                  onChange={setNoFormalSubmission}
                />
              )}
            </div>
          </Card>
        )}

        {isInterviewing && (
          <Card variant="outline" padding="10px 12px">
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 'var(--space-3)',
              }}
            >
              <Eyebrow>顺便安排这轮面试（可选）</Eyebrow>
              <div style={{ display: 'flex', gap: 'var(--space-3)' }}>
                <Listbox
                  label="轮次"
                  value={roundName}
                  onChange={setRoundName}
                  groups={[
                    {
                      label: '',
                      options: ROUNDS.map((r) => ({ value: r, label: r })),
                    },
                  ]}
                />
                <Listbox label="形式" value={format} onChange={setFormat} groups={[{ label: '', options: FORMATS }]} />
              </div>
              <Input
                label="面试时间"
                type="datetime-local"
                value={scheduled}
                onChange={(e) => setScheduled(e.target.value)}
                hint="留空则只改状态，不创建面试轮次"
              />
            </div>
          </Card>
        )}

        {needsReason && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 'var(--space-2)',
            }}
          >
            {reasonPresets.length > 0 && (
              <div
                style={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  gap: 'var(--space-2)',
                }}
              >
                {reasonPresets.map((r) => (
                  <Tag key={r} selected={reason === r} onClick={() => setReason(r)}>
                    {r}
                  </Tag>
                ))}
              </div>
            )}
            <Textarea
              label="原因 *"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={2}
              placeholder="必填"
            />
          </div>
        )}

        <Textarea label="说明（可选）" value={note} onChange={(e) => setNote(e.target.value)} rows={2} />
      </div>
    </Modal>
  )
}
