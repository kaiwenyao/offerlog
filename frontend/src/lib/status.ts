// Status metadata shared across views (keys are the API contract, §2.2).
// Labels, badge tones and dot colors follow the tech-utility palette.
import type { Tone } from '../ds'

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

export const STATUSES: StatusMeta[] = [
  { key: 'saved', label: '待投递', icon: '🗂️', category: 'preparing', tone: 'neutral', dot: 'var(--neutral)' },
  { key: 'preparing', label: '准备材料', icon: '📝', category: 'preparing', tone: 'neutral', dot: 'var(--text-muted)' },
  { key: 'applied', label: '已投递', icon: '📤', category: 'in_progress', tone: 'info', dot: 'var(--info)' },
  { key: 'screening', label: '初筛沟通', icon: '📞', category: 'in_progress', tone: 'info', dot: 'var(--info)' },
  { key: 'assessment', label: '笔试作业', icon: '🧪', category: 'in_progress', tone: 'warning', dot: 'var(--warning)' },
  { key: 'interviewing', label: '面试中', icon: '🎤', category: 'in_progress', tone: 'accent', dot: 'var(--accent)' },
  { key: 'offer', label: '收到 Offer', icon: '🎉', category: 'decision', tone: 'positive', dot: 'var(--positive)' },
  { key: 'accepted', label: '已接受', icon: '✅', category: 'ended', tone: 'positive', dot: 'var(--positive)' },
  { key: 'rejected', label: '被拒绝', icon: '🚫', category: 'ended', tone: 'danger', dot: 'var(--danger)' },
  { key: 'withdrawn', label: '已撤回', icon: '↩️', category: 'ended', tone: 'neutral', dot: 'var(--neutral)' },
  { key: 'closed', label: '岗位关闭', icon: '🔒', category: 'ended', tone: 'neutral', dot: 'var(--neutral)' },
]

const byKey = new Map(STATUSES.map((s) => [s.key, s]))

// 子状态字典（方案 §3.1/§6.5）。后端 backend/internal/applications/domain/substatus.go
// 是唯一白名单，会通过 GET /api/v1/meta/status-model 下发；这里保留一份用于首屏
// 与离线渲染的镜像。同一个键在不同大阶段含义不同，所以按阶段分组，
// 绝不用全局 map 查标签。
export interface SubstatusMeta {
  key: string
  label: string
}

export const SUBSTATUSES: Record<string, SubstatusMeta[]> = {
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

// 主流程推进顺序（用于“下一步建议”与阶段推进条）
export const FLOW_ORDER = [
  'saved',
  'preparing',
  'applied',
  'screening',
  'assessment',
  'interviewing',
  'offer',
  'accepted',
]

/** The 7 pips rendered in the table's 阶段推进 column (design: r.d1…r.d7). */
export const FLOW_PIPS = ['saved', 'preparing', 'applied', 'screening', 'assessment', 'interviewing', 'offer']

export const ENDED = new Set(['accepted', 'rejected', 'withdrawn', 'closed'])

export const NEXT_STEP_SUGGESTION: Record<string, string> = {
  saved: '填写岗位细节并设定截止时间',
  preparing: '上传简历 / 求职信',
  applied: '记录渠道与投递时间，准备跟进',
  screening: '记录沟通要点与下一步',
  assessment: '安排笔试 / 作业时间',
  interviewing: '安排下一轮面试',
  offer: '记录条件与答复期限',
}
