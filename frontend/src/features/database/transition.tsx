import { useMemo, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../lib/api'
import { toInstantInUserZone, toLocalDateTimeInput } from '../../lib/tz'
import {
  ASSESSMENT_KINDS,
  comboLabel,
  ENDED,
  statusMeta,
  substatusOptions,
} from '../../lib/status'
import {
  CHANGE_MODE_LABEL,
  changeTypeFor,
  changeTypeLabel,
  needsReason as reasonRequired,
  RECRUITING_KEYS,
  REASON_PRESETS,
  REOPEN_REASON_PRESETS,
  SKIP_SUBMISSION_TARGETS,
  suggestedTargets,
  targetGroups,
  type ChangeMode,
} from '../../lib/transitions'
import type { AssessmentRound, Interview } from '../../lib/types'
import { Button, Card, Checkbox, Eyebrow, Listbox, Tag, Textarea, Input } from '../../ds'
import { ErrorText, Modal, Spinner, StatusChip } from '../../components/ui'
import { FORMATS, ROUNDS } from './forms'

export interface TransitionModalProps {
  appId: number
  currentStatus: string
  /** 大阶段内的当前具体进度（方案 §3）；"" = 未细分。 */
  currentSubstatus?: string
  version: number
  /** Null when the record has no recorded submission yet — drives the 投递时间 ask. */
  submittedAt: string | null
  /** Existing rounds, used to guess which round the user is about to schedule. */
  interviews: Interview[]
  /** 已记录的 OA / 作业轮次，用于「新增一轮」而不是覆盖上一轮（方案 §3.2）。 */
  assessments?: AssessmentRound[]
  onClose: () => void
}

/** Rounds are named 一面/二面/…; guess the next one from how many exist. */
function guessRound(count: number): string {
  return ROUNDS[Math.min(count, ROUNDS.length - 2)]
}

/** OA 轮次名：第一轮叫「OA」，之后「OA 2」「OA 3」… */
function guessAssessmentName(count: number): string {
  return count === 0 ? 'OA' : `OA ${count + 1}`
}

export function TransitionModal({
  appId,
  currentStatus,
  currentSubstatus = '',
  version,
  submittedAt,
  interviews,
  assessments = [],
  onClose,
}: TransitionModalProps) {
  const qc = useQueryClient()
  const [to, setTo] = useState('')
  const [tosub, setToSub] = useState('')
  // 方案 §4.2：默认「流程实际退回」（追加真实变更、保留历史），只有用户明确
  // 说「之前选错了」才走更正，让旧的错误事实失效。
  const [mode, setMode] = useState<ChangeMode>('flow')
  // Prefilled, not blank: the field used to say 「留空默认为现在」 and then
  // silently write the server clock, so a user who meant 前天 got today with no
  // sign anything had been chosen for them.
  const [occurredAt, setOccurredAt] = useState(() => toLocalDateTimeInput())
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

  // Inline OA / 作业轮次（方案 §3.2）：收到邀请时记 invited_at / due_at / planned_at，
  // 标记完成时记 completed_at；四次时间各自保存，互不覆盖。
  // 进入 OA 阶段默认顺手记一轮；同阶段只改子状态时默认不新建，避免每次点
  // 「已完成 OA」都多出一轮（方案 §3.2「收到另一份测评：新增 OA 轮次」）。
  const [oaNewRound, setOaNewRound] = useState(currentStatus !== 'assessment')
  const [oaKind, setOaKind] = useState('online_test')
  const [oaName, setOaName] = useState(() => guessAssessmentName(assessments.length))
  const [oaInvited, setOaInvited] = useState('')
  const [oaPlanned, setOaPlanned] = useState('')
  const [oaDue, setOaDue] = useState('')
  const [oaCompletedUnknown, setOaCompletedUnknown] = useState(false)
  const [oaLink, setOaLink] = useState('')

  const isReopen = ENDED.has(currentStatus)
  const groups = useMemo(() => targetGroups(currentStatus), [currentStatus])
  const suggestions = useMemo(() => suggestedTargets(currentStatus), [currentStatus])

  const needsReason = reasonRequired(currentStatus, to)
  // Only ask for a submission time when the record genuinely has none. A record
  // that was already submitted must not be re-asked on every later stage. In
  // correction mode the submission facts are irrelevant — the correction
  // replay never touches them — so the fields stay hidden instead of forcing
  // input that would be silently discarded (PR #23 review P1 #7).
  const needsSubmitted = mode === 'flow' && RECRUITING_KEYS.includes(to) && !submittedAt
  // 已投递 always needs a real time — offering the escape hatch there would
  // produce a row that reads as submitted but counts as unsubmitted everywhere.
  const canSkipSubmission = SKIP_SUBMISSION_TARGETS.includes(to)
  const isInterviewing = to === 'interviewing'
  const isAssessment = to === 'assessment'
  const recordAssessment = isAssessment && oaNewRound
  // 关注轮次：进入 OA / 面试阶段时，同一事务里建一条轮次并把阶段指向它。
  const createsRound = (isInterviewing && scheduled !== '') || recordAssessment
  // 阶段、子状态、关注轮次全都没变 → 后端会答 same_status，前端先挡住。
  const isNoop = to === currentStatus && tosub === currentSubstatus && !createsRound
  // Prefer the target's own presets (毁约 from 已接受 wants the 撤回 reasons,
  // not the reopen ones); fall back to reopen copy for a pipeline target.
  const reasonPresets = REASON_PRESETS[to] ?? (isReopen ? REOPEN_REASON_PRESETS : [])
  const subOptions = substatusOptions(to)

  const canSubmit =
    to !== '' &&
    !isNoop &&
    (!needsReason || reason.trim() !== '') &&
    (!needsSubmitted || noFormalSubmission || submitted !== '')

  /** 更正流程必须指明要修正的事件；自动选最近一条可更正的状态事件。 */
  const mut = useMutation({
    mutationFn: async () => {
      const occurred = occurredAt ? toInstantInUserZone(occurredAt) : null
      if (occurredAt && occurred?.iso == null) throw new ApiError('bad_occurred_at', '发生时间格式不正确', 400)
      const skipSubmission = canSkipSubmission && noFormalSubmission
      const submittedInstant = !skipSubmission && submitted ? toInstantInUserZone(submitted) : null
      if (submitted && !skipSubmission && submittedInstant?.iso == null) {
        throw new ApiError('bad_submitted_at', '投递时间格式不正确', 400)
      }

      if (mode === 'correction') {
        // 更正走独立接口：旧事件保留审计，有效进度由服务端重放重算。
        await api.post(`/api/v1/applications/${appId}/correct-current`, {
          to_status: to,
          to_substatus: tosub,
          reason,
          occurred_at: occurred?.iso ?? null,
          version,
        })
        statusCommitted.current = true
        return
      }

      await api.post(`/api/v1/applications/${appId}/transitions`, {
        to_status: to,
        to_substatus: tosub,
        version,
        occurred_at: occurred?.iso ?? null,
        reason,
        note,
        submitted_at: submittedInstant?.iso ?? null,
        no_formal_submission: skipSubmission,
        change_type: '',
        idempotency_key: `ui-${Date.now()}`,
        ...(recordAssessment
          ? {
              assessment: {
                kind: oaKind,
                name: oaName,
                progress: tosub === 'completed' || tosub === 'passed' ? 'completed' : 'preparing',
                result: tosub === 'passed' ? 'passed' : 'unknown',
                invited_at: oaInvited ? toInstantInUserZone(oaInvited).iso : null,
                planned_at: oaPlanned ? toInstantInUserZone(oaPlanned).iso : null,
                due_at: oaDue ? toInstantInUserZone(oaDue).iso : null,
                completed_at:
                  tosub === 'completed' || tosub === 'passed'
                    ? oaCompletedUnknown
                      ? null
                      : occurred?.iso ?? null
                    : null,
                completed_unknown: oaCompletedUnknown,
                link: oaLink,
              },
            }
          : {}),
        ...(isInterviewing && scheduled
          ? {
              interview: {
                round_name: roundName,
                format,
                scheduled_at: toInstantInUserZone(scheduled).iso,
                timezone: toInstantInUserZone(scheduled).zone,
                progress: tosub === 'completed' ? 'completed' : tosub === 'preparing' ? 'preparing' : 'awaiting_schedule',
              },
            }
          : {}),
      })
      statusCommitted.current = true
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
          `状态已更新为「${comboLabel(to, tosub)}」，但轮次写入失败：${apiMsg ?? '网络错误或服务无响应'}。可在「概览」标签重新安排。`,
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
    qc.invalidateQueries({ queryKey: ['assessments', appId] })
    qc.invalidateQueries({ queryKey: ['apps'] })
  }

  // 同一个大阶段允许只换子状态，所以标题区分「重开 / 更正」与普通更新。
  const title = isReopen ? '重开 / 更正' : '更新进度'
  const changeKind = to ? changeTypeFor(currentStatus, to) : null

  return (
    <Modal
      title={title}
      onClose={onClose}
      width={560}
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
              {mut.isPending ? <Spinner size={14} /> : mode === 'correction' ? '确认更正' : '确认更新'}
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
          <span style={{ font: 'var(--type-caption)' }}>{comboLabel(currentStatus, currentSubstatus)}</span>
        </div>

        {suggestions.length > 0 && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 'var(--space-2)',
            }}
          >
            <Eyebrow>下一步建议</Eyebrow>
            <div
              style={{
                display: 'flex',
                flexWrap: 'wrap',
                gap: 'var(--space-2)',
              }}
            >
              {suggestions.map((s) => (
                <Tag
                  key={s.key}
                  selected={to === s.key}
                  onClick={() => {
                    setTo(s.key)
                    setToSub('')
                  }}
                >
                  {s.icon} {s.label}
                </Tag>
              ))}
            </div>
          </div>
        )}

        <Listbox
          label="目标状态 *"
          value={to}
          onChange={(v) => {
            setTo(v)
            setToSub('')
          }}
          groups={groups}
          placeholder="选择…"
          triggerClassName="status-target-trigger"
        />

        {/* 方案 §3.1：进入有细分的阶段时，默认展开该阶段的子状态，让用户直接
            选「准备 OA」「已完成 OA · 等结果」而不是先选大阶段再猜。 */}
        {subOptions.length > 0 && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
            <Eyebrow>{statusMeta(to).label} · 具体进度</Eyebrow>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
              <Tag selected={tosub === ''} onClick={() => setToSub('')}>
                未细分
              </Tag>
              {subOptions.map((s) => (
                <Tag key={s.key} selected={tosub === s.key} onClick={() => setToSub(s.key)}>
                  {s.label}
                </Tag>
              ))}
            </div>
          </div>
        )}

        {/* 方案 §4.2：回退与更正的分叉。真实发生过的退回保留历史事实，
            「之前选错了」才作废那条错误记录并重算有效进度。 */}
        {to !== '' && !isNoop && (
          <Card variant="outline" padding="10px 12px">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
              <Eyebrow>这次变更属于</Eyebrow>
              <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
                <Tag selected={mode === 'flow'} onClick={() => setMode('flow')}>
                  {CHANGE_MODE_LABEL.flow}
                  {changeKind ? ` · ${changeTypeLabel(changeKind)}` : ''}
                </Tag>
                <Tag selected={mode === 'correction'} onClick={() => setMode('correction')}>
                  {CHANGE_MODE_LABEL.correction}
                </Tag>
              </div>
              <span style={{ font: 'var(--type-caption)', color: 'var(--text-muted)' }}>
                {mode === 'flow'
                  ? '这件事真实发生过（例如 HR 要求补材料）。保留已投递、面试等历史事实，只是当前进度往回走。'
                  : '之前那一步点错了。错误记录会保留审计标记，但不计入有效完成次数，当前进度由最后一条有效记录决定。'}
              </span>
            </div>
          </Card>
        )}

        <Input
          label="发生时间"
          type="datetime-local"
          value={occurredAt}
          onChange={(e) => setOccurredAt(e.target.value)}
          hint="这件事什么时候发生的 —— 时间线按它显示；已预填现在，可改成实际发生的时间"
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

        {/* OA / 作业：收到、计划、截止、完成四种时间各自保存（方案 §3.2）。 */}
        {isAssessment && mode === 'flow' && (
          <Card variant="outline" padding="10px 12px">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
              <Eyebrow>测评轮次</Eyebrow>
              <Checkbox
                label={
                  assessments.length === 0
                    ? '记录这一轮测评的收到 / 计划 / 截止时间'
                    : `新增一轮测评（已有 ${assessments.length} 轮记录，不会覆盖）`
                }
                checked={oaNewRound}
                onChange={setOaNewRound}
              />
              {!oaNewRound && (
                <span style={{ font: 'var(--type-caption)', color: 'var(--text-muted)' }}>
                  只改这一阶段的具体进度；该阶段已有轮次的进度会同步成同一事实。
                </span>
              )}
              {oaNewRound && (
                <>
              <div style={{ display: 'flex', gap: 'var(--space-3)' }}>
                <Listbox
                  label="类型"
                  value={oaKind}
                  onChange={setOaKind}
                  groups={[{ label: '', options: ASSESSMENT_KINDS }]}
                />
                <Input label="名称" value={oaName} onChange={(e) => setOaName(e.target.value)} />
              </div>
              <div style={{ display: 'flex', gap: 'var(--space-3)' }}>
                <Input
                  label="收到邀请"
                  type="datetime-local"
                  value={oaInvited}
                  onChange={(e) => setOaInvited(e.target.value)}
                />
                <Input
                  label="计划开做"
                  type="datetime-local"
                  value={oaPlanned}
                  onChange={(e) => setOaPlanned(e.target.value)}
                />
              </div>
              <div style={{ display: 'flex', gap: 'var(--space-3)', alignItems: 'flex-end' }}>
                <Input
                  label="截止时间"
                  type="datetime-local"
                  value={oaDue}
                  onChange={(e) => setOaDue(e.target.value)}
                  hint="完成后不再提醒这个截止时间"
                />
                <Input label="测试链接" value={oaLink} onChange={(e) => setOaLink(e.target.value)} />
              </div>
                </>
              )}
              {oaNewRound && (tosub === 'completed' || tosub === 'passed') && (
                <Checkbox
                  label="完成时间不详（只标「已完成」，不伪造精确时间）"
                  checked={oaCompletedUnknown}
                  onChange={setOaCompletedUnknown}
                />
              )}
            </div>
          </Card>
        )}

        {isInterviewing && mode === 'flow' && (
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

        {(needsReason || mode === 'correction') && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 'var(--space-2)',
            }}
          >
            {/* 方案 §4.2：更正的原因记在更正事件上；被改写的那步如果本身要求
                原因（例如从终态重开），留空时后端会用该事件当时记录的原因兜底。 */}
            {reasonPresets.length > 0 && mode === 'correction' && (
              <span style={{ font: 'var(--type-caption)', color: 'var(--text-muted)' }}>
                原因可选；留空时沿用被更正记录当时填写的原因。
              </span>
            )}
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
              label={needsReason ? '原因 *' : '原因（可选）'}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={2}
              placeholder={needsReason ? '必填' : '为什么改这条记录'}
            />
          </div>
        )}

        <Textarea label="说明（可选）" value={note} onChange={(e) => setNote(e.target.value)} rows={2} />
      </div>
    </Modal>
  )
}
