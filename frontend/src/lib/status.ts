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
