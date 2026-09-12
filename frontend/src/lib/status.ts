// Status metadata shared across views (keys are the API contract, §2.2).
// Labels, badge tones and dot colors follow the tech-utility palette.
//
// 方案 §6.5: the whitelist (stage keys, labels, legal substatuses, terminal
// set, transition targets) is OWNED BY THE SERVER and hydrated at bootstrap
// from GET /api/v1/meta/status-model via hydrateStatusModel. The static tables
// below are the offline / first-paint fallback only — presentation attributes
// (icon / tone / dot) are local by design and keyed by stage key.
import type { Tone } from '../ds'
import type { MilestoneKind } from './milestones'

export interface StatusMeta {
  key: string
  label: string
  icon: string
  category: 'preparing' | 'in_progress' | 'decision' | 'ended'
  /** Badge tone from the design system palette. */
  tone: Tone
  /** Solid color for dots, stage trails and progress pips. */
  dot: string
}

const PRESENTATION: Record<string, { icon: string; category: StatusMeta['category']; tone: Tone; dot: string }> = {
  saved: { icon: '🗂️', category: 'preparing', tone: 'neutral', dot: 'var(--neutral)' },
  preparing: { icon: '📝', category: 'preparing', tone: 'neutral', dot: 'var(--text-muted)' },
  applied: { icon: '📤', category: 'in_progress', tone: 'info', dot: 'var(--info)' },
  screening: { icon: '📞', category: 'in_progress', tone: 'info', dot: 'var(--info)' },
  assessment: { icon: '🧪', category: 'in_progress', tone: 'warning', dot: 'var(--warning)' },
  interviewing: { icon: '🎤', category: 'in_progress', tone: 'accent', dot: 'var(--accent)' },
  offer: { icon: '🎉', category: 'decision', tone: 'positive', dot: 'var(--positive)' },
  accepted: { icon: '✅', category: 'ended', tone: 'positive', dot: 'var(--positive)' },
  rejected: { icon: '🚫', category: 'ended', tone: 'danger', dot: 'var(--danger)' },
  withdrawn: { icon: '↩️', category: 'ended', tone: 'neutral', dot: 'var(--neutral)' },
  closed: { icon: '🔒', category: 'ended', tone: 'neutral', dot: 'var(--neutral)' },
}

export const FALLBACK_LABELS: Record<string, string> = {
  saved: '待投递',
  preparing: '准备材料',
  applied: '已投递',
  screening: '初筛沟通',
  assessment: 'OA / 作业',
  interviewing: '面试',
  offer: '收到 Offer',
  accepted: '已接受',
  rejected: '被拒绝',
  withdrawn: '已撤回',
  closed: '岗位关闭',
}

export let STATUSES: StatusMeta[] = Object.entries(FALLBACK_LABELS).map(([key, label]) => ({
  key,
  label,
  ...PRESENTATION[key],
}))

let byKey = new Map(STATUSES.map((s) => [s.key, s]))

// 子状态字典（方案 §3.1/§6.5）：hydrateStatusModel 用服务端
// GET /api/v1/meta/status-model 的 payload 整体替换；这份静态表只是离线 /
// 首屏兜底。同一个键在不同大阶段含义不同，所以按阶段分组，
// 绝不用全局 map 查标签。
export interface SubstatusMeta {
  key: string
  label: string
}

const FALLBACK_SUBSTATUSES: Record<string, SubstatusMeta[]> = {
  preparing: [{ key: 'ready', label: '材料就绪 · 待投递' }],
  screening: [
    { key: 'awaiting_schedule', label: '待安排初筛' },
    { key: 'preparing', label: '准备初筛' },
    { key: 'completed', label: '已完成初筛 · 等反馈' },
  ],
  assessment: [
    { key: 'preparing', label: '准备 OA' },
    { key: 'completed', label: '已完成 OA · 等结果' },
    { key: 'passed', label: 'OA 已通过 · 等下一步' },
  ],
  interviewing: [
    { key: 'awaiting_schedule', label: '待安排面试' },
    { key: 'preparing', label: '准备面试' },
    { key: 'completed', label: '已完成面试 · 等反馈' },
  ],
  offer: [
    { key: 'reviewing', label: '待评估 Offer' },
    { key: 'negotiating', label: '协商 Offer' },
    { key: 'ready_to_accept', label: '待确认接受' },
  ],
}

export let SUBSTATUSES: Record<string, SubstatusMeta[]> = FALLBACK_SUBSTATUSES

/** Server payload of GET /api/v1/meta/status-model (the single whitelist). */
export interface ServerStatusModel {
  stages: Array<{
    key: string
    label: string
    category: string
    terminal: boolean
    substatus: SubstatusMeta[]
  }>
  targets: Record<string, Array<{ status: string }>>
  /**
   * 「添加事件」选择器与参考流程图用的事件类型清单（迁移 00006）。旧后端不返回
   * 这个字段，此时前端沿用 lib/milestones.ts 里的兜底表。
   */
  milestone_kinds?: MilestoneKind[]
}

/**
 * Replace the local whitelist with the server's status model (方案 §6.5).
 * Presentation attributes (icon / tone / dot) stay local, keyed by stage key.
 * Must run before the app tree renders so no component caches stale tables.
 */
export function hydrateStatusModel(m: ServerStatusModel): void {
  const CATEGORIES: Array<StatusMeta['category']> = ['preparing', 'in_progress', 'decision', 'ended']
  STATUSES = m.stages.map((st) => {
    const p = PRESENTATION[st.key] ?? { icon: '❓', category: 'preparing' as const, tone: 'neutral' as Tone, dot: 'var(--neutral)' }
    return {
      key: st.key,
      label: st.label,
      icon: p.icon,
      category: CATEGORIES.includes(st.category as StatusMeta['category']) ? (st.category as StatusMeta['category']) : p.category,
      tone: p.tone,
      dot: p.dot,
    }
  })
  byKey = new Map(STATUSES.map((s) => [s.key, s]))
  SUBSTATUSES = Object.fromEntries(m.stages.map((st) => [st.key, st.substatus ?? []]))
  ENDED = new Set(m.stages.filter((st) => st.terminal).map((st) => st.key))
}

/** Legal substatuses for a stage ([] when the stage has no subdivision). */
export function substatusOptions(status: string): SubstatusMeta[] {
  return SUBSTATUSES[status] ?? []
}

/** True when (status, substatus) is a legal pair; "" is always 未细分. */
export function validSubstatus(status: string, substatus: string): boolean {
  if (!substatus) return statusMeta(status).key === status
  return substatusOptions(status).some((s) => s.key === substatus)
}

/**
 * The exact label the list / detail shows, mirroring domain.ComboLabelForKind:
 * the un-subdivided state is named too (「未细分」), so a legacy row is never
 * silently displayed as if the user had chosen 准备 OA.
 */
export function comboLabel(status: string, substatus?: string | null, assessmentKind?: string | null): string {
  const sub = substatus ?? ''
  if (status === 'assessment') {
    const kind = assessmentKind ?? ''
    const noun = kind === 'take_home' ? '作业' : 'OA'
    if (kind && kind !== 'online_test') {
      if (sub === 'preparing') return `准备${noun}`
      if (sub === 'completed') return `已提交${noun} · 等结果`
      if (sub === 'passed') return `${noun}已通过 · 等下一步`
    }
    if (sub === 'preparing') return '准备 OA'
    if (sub === 'completed') return '已完成 OA · 等结果'
    if (sub === 'passed') return 'OA 已通过 · 等下一步'
    return 'OA / 作业 · 进度未细分'
  }
  if (!sub) {
    switch (status) {
      case 'saved':
        return '待投递'
      case 'applied':
        return '已投递 · 等回复'
      case 'screening':
        return '初筛沟通 · 未细分'
      case 'interviewing':
        return '面试中 · 未细分'
      case 'offer':
        return '收到 Offer · 未细分'
      default:
        return statusMeta(status).label
    }
  }
  const opt = substatusOptions(status).find((s) => s.key === sub)
  return opt?.label ?? statusMeta(status).label
}

/** 测评类型（方案 §3.2）：作业显示「准备作业 / 已提交作业」。 */
export const ASSESSMENT_KINDS: Array<{ value: string; label: string }> = [
  { value: 'online_test', label: '在线测试 (OA)' },
  { value: 'take_home', label: 'Take-home 作业' },
  { value: 'other', label: '其他测评' },
]

export function statusMeta(key: string): StatusMeta {
  return (
    byKey.get(key) ?? { key, label: key, icon: '❓', category: 'preparing', tone: 'neutral', dot: 'var(--neutral)' }
  )
}

export const PRIORITIES: Record<string, { label: string; strong: boolean }> = {
  high: { label: '高', strong: true },
  medium: { label: '中', strong: false },
  low: { label: '低', strong: false },
}

export function priorityLabel(p: string): string {
  return PRIORITIES[p]?.label ?? '中'
}

export const CHANNEL_OPTIONS = ['LinkedIn', 'Referral', '官网', '猎头', '内推社区', '其他']
export const REMOTE_OPTIONS = ['远程', '混合', '到岗', '']
export const EMPLOYMENT_OPTIONS = ['全职', '实习', '合同', '兼职']
export const SALARY_CURRENCIES = ['EUR', 'USD', 'GBP', 'CNY', '其他']

// 大阶段的推荐顺序（方案 §4.1：「阶段顺序只作建议」），用于“下一步建议”、
// 阶段推进条与 推进/回退 的判定。
//
// 初筛沟通 排在 OA / 作业 **之后**：投递后自动收到 OA 是最常见的路子，
// HR 的电话沟通通常发生在笔试通过、安排面试之前。把它排在 已投递 和 OA 之间
// 会让最普通的「投递 → OA」在这条工序线上留出一个永远填不上的空洞。
export const FLOW_ORDER = [
  'saved',
  'preparing',
  'applied',
  'assessment',
  'screening',
  'interviewing',
  'offer',
  'accepted',
]

/** The 7 pips rendered in the table's 阶段推进 column (design: r.d1…r.d7). */
export const FLOW_PIPS = ['saved', 'preparing', 'applied', 'assessment', 'screening', 'interviewing', 'offer']

export let ENDED = new Set(STATUSES.filter((s) => s.category === 'ended').map((s) => s.key))

export const NEXT_STEP_SUGGESTION: Record<string, string> = {
  saved: '填写岗位细节并设定截止时间',
  preparing: '上传简历 / 求职信',
  applied: '记录渠道与投递时间，准备跟进',
  screening: '记录沟通要点与下一步',
  assessment: '安排笔试 / 作业时间',
  interviewing: '安排下一轮面试',
  offer: '记录条件与答复期限',
}
