// Client-side mirror of the backend state machine.
//
// ⚠️ SOURCE OF TRUTH IS THE BACKEND: backend/internal/applications/domain/domain.go
// (`allowedDirect`). Every edge here must exist there — the server re-validates
// and returns `invalid_transition`, so an extra edge is a dead option the user
// can pick and get a 400 for. Change one, change both, and update the guard
// test in frontend/tests/unit.test.ts.
import { ENDED, FLOW_ORDER, STATUSES, statusMeta, type StatusMeta } from './status'
import type { ListboxGroup } from '../ds'

export const TRANSITIONS: Record<string, string[]> = {
  // 准备期可以跳阶到任一招聘阶段：真实用户常常「面完了才想起来记录」或走内推
  // 直接约面。进入 in-progress 时后端仍要投递证据（补时间或勾未经正式投递）。
  saved: [
    'preparing',
    'applied',
    'screening',
    'assessment',
    'interviewing',
    'offer',
    'rejected',
    'withdrawn',
    'closed',
  ],
  preparing: ['applied', 'screening', 'assessment', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
  applied: ['screening', 'assessment', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
  screening: ['assessment', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
  assessment: ['screening', 'interviewing', 'offer', 'rejected', 'withdrawn', 'closed'],
  interviewing: ['screening', 'assessment', 'offer', 'rejected', 'withdrawn', 'closed'],
  offer: ['accepted', 'rejected', 'withdrawn', 'closed'],
  // 终态重开 / 毁约，后端一律要求填原因。
  accepted: ['offer', 'withdrawn'],
  rejected: ['saved', 'preparing', 'applied', 'screening', 'assessment', 'interviewing', 'offer'],
  withdrawn: ['saved', 'preparing', 'applied', 'screening', 'assessment', 'interviewing', 'offer'],
  closed: ['saved', 'preparing', 'applied', 'screening', 'assessment', 'interviewing', 'offer'],
}

/** Reachable targets from `from`, in the canonical dictionary order. */
export function allowedTargets(from: string): StatusMeta[] {
  const keys = TRANSITIONS[from] ?? []
  return STATUSES.filter((s) => keys.includes(s.key))
}

/** Statuses that still need submission evidence before they can be entered. */
export const RECRUITING_KEYS = ['applied', 'screening', 'assessment', 'interviewing']

/**
 * Where 「未经正式投递」 is a coherent claim (mirror of domain.go
 * SkipSubmissionStatuses). 已投递 is excluded on purpose: that status *is* the
 * assertion that a submission happened, so it always needs a real time.
 */
export const SKIP_SUBMISSION_TARGETS = ['screening', 'assessment', 'interviewing']

/** One-line "what picking this means" copy shown next to each target. */
const TARGET_HINT: Record<string, string> = {
  preparing: '开始改简历 / 写求职信',
  applied: '简历已投出',
  screening: 'HR 或猎头已联系',
  assessment: '收到笔试或作业',
  interviewing: '进入面试轮次',
  offer: '拿到录用意向',
  accepted: '接受这个 Offer',
  rejected: '对方明确拒绝',
  withdrawn: '我主动放弃',
  closed: '岗位撤销 / 关闭',
  saved: '退回收藏，重新开始',
}

function rank(key: string): number {
  const i = FLOW_ORDER.indexOf(key)
  return i < 0 ? Number.POSITIVE_INFINITY : i
}

function toOption(s: StatusMeta) {
  return {
    value: s.key,
    label: s.label,
    icon: s.icon,
    dot: s.dot,
    hint: TARGET_HINT[s.key],
  }
}

/**
 * Split reachable targets into the three buckets a job seeker actually thinks
 * in: 推进 (further along the flow), 回退 / 更正 (back up the flow), 结束
 * (the terminal outcomes). From a terminal status the forward bucket is the
 * reopen path, so it is relabelled.
 */
export function targetGroups(from: string): ListboxGroup[] {
  const fromEnded = ENDED.has(from)
  const targets = allowedTargets(from)
  const fromRank = rank(from)

  const ending = targets.filter((s) => ENDED.has(s.key))
  const pipeline = targets.filter((s) => !ENDED.has(s.key))
  const forward = pipeline.filter((s) => rank(s.key) > fromRank)
  const backward = pipeline.filter((s) => rank(s.key) <= fromRank)

  return [
    {
      label: fromEnded ? '重新开启' : '推进',
      options: (fromEnded ? pipeline : forward).map(toOption),
    },
    {
      label: '回退 / 更正',
      options: (fromEnded ? [] : backward).map(toOption),
    },
    { label: '结束', options: ending.map(toOption) },
  ].filter((g) => g.options.length > 0)
}

/**
 * The two or three targets worth a one-tap chip above the picker: the next
 * step in the main flow, plus the endings a user reaches most often from here.
 */
export function suggestedTargets(from: string): StatusMeta[] {
  const reachable = new Set(TRANSITIONS[from] ?? [])
  const fromRank = rank(from)
  const nextInFlow = FLOW_ORDER.filter((k) => rank(k) > fromRank && reachable.has(k)).slice(0, 2)
  const endings = ENDED.has(from) ? [] : ['rejected'].filter((k) => reachable.has(k))
  return [...nextInFlow, ...endings].map(statusMeta)
}

/**
 * Targets the backend refuses without a reason (domain.go ValidateTransition:
 * `to == rejected || withdrawn || closed`). Note 已接受 is NOT here — accepting
 * an offer needs no justification.
 */
export const REASON_REQUIRED_TARGETS = ['rejected', 'withdrawn', 'closed']

/**
 * Mirrors the backend rule exactly: a reason is required for the three managed
 * endings, and for reopening an ended record back into the pipeline
 * (`fromTerminal && !toTerminal`).
 */
export function needsReason(from: string, to: string): boolean {
  if (!to) return false
  return REASON_REQUIRED_TARGETS.includes(to) || (ENDED.has(from) && !ENDED.has(to))
}

/** Preset reasons offered as one-tap chips for the targets that require one. */
export const REASON_PRESETS: Record<string, string[]> = {
  rejected: ['简历未通过', '面试未通过', '岗位取消', '长期无回复'],
  withdrawn: ['接受了其他 Offer', '薪资不匹配', '地点不合适', '主动放弃'],
  closed: ['HR 通知岗位关闭', '长期无回复', '岗位已招满'],
}

/** Reasons for reopening a record that had already ended. */
export const REOPEN_REASON_PRESETS = ['招聘方重新联系', '之前记录有误', '岗位重新开放']
