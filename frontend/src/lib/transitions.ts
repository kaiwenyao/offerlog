// Client-side fallback of the backend state machine.
//
// ⚠️ SOURCE OF TRUTH IS THE BACKEND: the rule mirror below only runs until
// hydrateTransitions() installs the server-owned target map from
// GET /api/v1/meta/status-model at bootstrap (方案 §6.5). The server
// re-validates every request and answers `invalid_transition` /
// `invalid_substatus` / `missing_reason`, so anything offered here that the
// server does not allow is a dead option the user can pick and get a 400 for.
// Keep this mirror in sync with backend/internal/applications/domain
// (allowedTarget / ReasonRequired / BucketFor / ValidSubstatus) for offline
// rendering, and update the guard test in frontend/tests/unit.test.ts.
//
// 方案 §4.1 relaxed the rules from a hand-written edge list to a rule:
//   • any non-terminal → any non-terminal (forward, skip or back),
//   • terminal → any non-terminal (reopen, needs a reason),
//   • → 已接受 only from Offer (never fabricate an acceptance),
//   • 已接受 → 已撤回 (毁约) is the one legal terminal → terminal move,
//   • terminal → terminal otherwise must go through 更正, not 流转.
import { ENDED, FLOW_ORDER, STATUSES, statusMeta, substatusOptions, type StatusMeta } from './status'
import type { ListboxGroup } from '../ds'

/** Every stage key, in dictionary order (rebuilt when the server model hydrates). */
export let ALL_STATUS_KEYS: string[] = STATUSES.map((s) => s.key)

export const isTerminal = (k: string): boolean => ENDED.has(k)

/**
 * Mirrors domain.allowedTarget exactly — for the OFFLINE fallback. At runtime
 * the server-owned target map hydrated from GET /api/v1/meta/status-model
 * takes over (方案 §6.5), so backend drift changes what the UI offers without
 * a frontend release. `same stage` returns true because a substatus / focus
 * round change inside one stage is a real transition; whether it is a *change*
 * at all is decided separately (same_status).
 */
export function canTransition(from: string, to: string): boolean {
  if (serverTargets) return (serverTargets[from] ?? []).includes(to)
  if (!ALL_STATUS_KEYS.includes(from) || !ALL_STATUS_KEYS.includes(to)) return false
  if (from === to) return true // 子状态 / 关注轮次变更
  if (to === 'accepted') return from === 'offer'
  if (from === 'accepted' && to === 'withdrawn') return true
  if (isTerminal(from) && isTerminal(to)) return false
  return true
}

// serverTargets is the hydrated whitelist (status -> reachable target keys),
// null until hydrateTransitions runs at bootstrap.
let serverTargets: Record<string, string[]> | null = null

/**
 * Install the server-owned transition map (方案 §6.5). From here on
 * canTransition / allowedTargets answer from the backend's TargetCombos
 * instead of the offline rule mirror below.
 */
export function hydrateTransitions(targets: Record<string, Array<{ status: string }>>): void {
  serverTargets = Object.fromEntries(Object.entries(targets).map(([k, v]) => [k, v.map((t) => t.status)]))
  ALL_STATUS_KEYS = STATUSES.map((s) => s.key)
  TRANSITIONS = Object.fromEntries(ALL_STATUS_KEYS.map((from) => [from, allowedTargets(from).map((s) => s.key)]))
}

/**
 * The reachable targets from `from`, in dictionary order. When the server
 * model is hydrated this is exactly `targets[from]` from
 * GET /api/v1/meta/status-model; offline it falls back to the rule mirror.
 * A stage with no subdivision cannot target itself (nothing to change).
 */
export function allowedTargets(from: string): StatusMeta[] {
  const pool =
    serverTargets && serverTargets[from]
      ? STATUSES.filter((s) => serverTargets![from].includes(s.key))
      : STATUSES.filter(
          (s) => canTransition(from, s.key) && !(s.key === from && substatusOptions(from).length === 0),
        )
  return pool.filter((s) => !(s.key === from && substatusOptions(from).length === 0))
}

/** Reachable status keys from `from` (shape used by the guard test). */
export let TRANSITIONS: Record<string, string[]> = Object.fromEntries(
  ALL_STATUS_KEYS.map((from) => [from, allowedTargets(from).map((s) => s.key)]),
)

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

/** Mirrors domain.BucketFor — decides the picker group, never permission. */
export function bucketFor(from: string, to: string): 'advance' | 'backward' | 'reopen' | 'end' {
  if (isTerminal(to)) return 'end'
  if (isTerminal(from)) return 'reopen'
  return rank(to) > rank(from) ? 'advance' : 'backward'
}

/** Mirrors domain.DeriveChangeType: what the timeline should call this move. */
export type ChangeType = 'advance' | 'rollback' | 'reopen'

export function changeTypeFor(from: string, to: string): ChangeType {
  if (isTerminal(from) && !isTerminal(to)) return 'reopen'
  if (rank(to) < rank(from)) return 'rollback'
  return 'advance'
}

export function changeTypeLabel(t: ChangeType | string): string {
  switch (t) {
    case 'rollback':
      return '回退'
    case 'reopen':
      return '重开'
    case 'correct':
      return '更正'
    default:
      return '推进'
  }
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
 * Split reachable targets into the groups a job seeker actually thinks in:
 * 推进 (further along the flow), 回退 / 更正 (back up the flow), 结束 (the
 * terminal outcomes). From a terminal status the forward bucket is the reopen
 * path, so it is relabelled.
 */
export function targetGroups(from: string): ListboxGroup[] {
  const fromEnded = isTerminal(from)
  const targets = allowedTargets(from)
  const fromRank = rank(from)

  if (fromEnded) {
    // 终态只能重开回非终态（需要原因），外加 已接受 → 已撤回 这一种毁约。
    const reopen = targets.filter((s) => !isTerminal(s.key))
    const inter = targets.filter((s) => isTerminal(s.key) && s.key !== from)
    return [
      { label: '重新开启', options: reopen.map(toOption) },
      { label: '毁约 / 更正', options: inter.map(toOption) },
    ].filter((g) => g.options.length > 0)
  }

  const accept = targets.filter((s) => s.key === 'accepted')
  const ending = targets.filter((s) => isTerminal(s.key) && s.key !== 'accepted')
  const pipeline = targets.filter((s) => !isTerminal(s.key))
  const forward = pipeline.filter((s) => rank(s.key) > fromRank)
  const backward = pipeline.filter((s) => rank(s.key) <= fromRank)

  return [
    // 接受 Offer 是向前的一步，放在「推进」而不是「结束」。
    { label: '推进', options: [...forward, ...accept].map(toOption) },
    { label: '回退 / 更正', options: backward.map(toOption) },
    { label: '结束', options: ending.map(toOption) },
  ].filter((g) => g.options.length > 0)
}

/**
 * The two or three targets worth a one-tap chip above the picker: the next
 * step in the main flow, plus 被拒绝 for records still in the pipeline.
 */
export function suggestedTargets(from: string): StatusMeta[] {
  const reachable = new Set(TRANSITIONS[from] ?? [])
  const fromRank = rank(from)
  const nextInFlow = FLOW_ORDER.filter((k) => rank(k) > fromRank && reachable.has(k)).slice(0, 2)
  const endings = isTerminal(from) ? [] : ['rejected'].filter((k) => reachable.has(k))
  return [...nextInFlow, ...endings].map(statusMeta)
}

/**
 * Targets the backend refuses without a reason (domain.go ReasonRequired):
 * the three managed endings, plus reopening an ended record back into the
 * pipeline. Note 已接受 is NOT here — accepting an offer needs no justification.
 */
export const REASON_REQUIRED_TARGETS = ['rejected', 'withdrawn', 'closed']

/** Mirrors domain.ReasonRequired exactly. */
export function needsReason(from: string, to: string): boolean {
  if (!to) return false
  return REASON_REQUIRED_TARGETS.includes(to) || (isTerminal(from) && !isTerminal(to))
}

/** Preset reasons offered as one-tap chips for the targets that require one. */
export const REASON_PRESETS: Record<string, string[]> = {
  rejected: ['简历未通过', '面试未通过', '岗位取消', '长期无回复'],
  withdrawn: ['接受了其他 Offer', '薪资不匹配', '地点不合适', '主动放弃'],
  closed: ['HR 通知岗位关闭', '长期无回复', '岗位已招满'],
}

/** Reasons for reopening a record that had already ended. */
export const REOPEN_REASON_PRESETS = ['招聘方重新联系', '之前记录有误', '岗位重新开放']

/**
 * 方案 §4.2: 回退与更正的分叉。默认「流程实际退回」——真实发生过的事保留历史，
 * 只有用户明确说「之前选错了」才走更正、让旧的错误事实失效。
 */
export type ChangeMode = 'flow' | 'correction'

export const CHANGE_MODE_LABEL: Record<ChangeMode, string> = {
  flow: '流程实际退回',
  correction: '之前选错了',
}

/** The change_type the transition API records for a given mode. */
export const API_CHANGE_TYPE: Record<ChangeMode, string> = {
  flow: 'auto', // 由后端 DeriveChangeType 按顺序推导 advance / rollback / reopen
  correction: 'correct',
}
