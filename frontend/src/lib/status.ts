// Status metadata shared across views (keys are the API contract, §2.2).
export interface StatusMeta {
  key: string
  label: string
  icon: string
  category: 'preparing' | 'in_progress' | 'decision' | 'ended'
  color: string // css class suffix
}

export const STATUSES: StatusMeta[] = [
  { key: 'saved', label: '待投递', icon: '🗂️', category: 'preparing', color: 'st-saved' },
  { key: 'preparing', label: '准备材料', icon: '📝', category: 'preparing', color: 'st-preparing' },
  { key: 'applied', label: '已投递', icon: '📤', category: 'in_progress', color: 'st-applied' },
  { key: 'screening', label: '初筛/沟通', icon: '📞', category: 'in_progress', color: 'st-screening' },
  { key: 'assessment', label: '笔试/作业', icon: '🧪', category: 'in_progress', color: 'st-assessment' },
  { key: 'interviewing', label: '面试中', icon: '🎤', category: 'in_progress', color: 'st-interviewing' },
  { key: 'offer', label: '收到 Offer', icon: '🎉', category: 'decision', color: 'st-offer' },
  { key: 'accepted', label: '已接受', icon: '✅', category: 'ended', color: 'st-accepted' },
  { key: 'rejected', label: '被拒绝', icon: '🚫', category: 'ended', color: 'st-rejected' },
  { key: 'withdrawn', label: '已撤回', icon: '↩️', category: 'ended', color: 'st-withdrawn' },
  { key: 'closed', label: '岗位关闭', icon: '🔒', category: 'ended', color: 'st-closed' },
]

const byKey = new Map(STATUSES.map((s) => [s.key, s]))

export function statusMeta(key: string): StatusMeta {
  return byKey.get(key) ?? { key, label: key, icon: '❓', category: 'preparing', color: 'st-saved' }
}

export const PRIORITIES: Record<string, { label: string; cls: string }> = {
  high: { label: '高', cls: 'priority-high' },
  medium: { label: '中', cls: 'priority-med' },
  low: { label: '低', cls: 'priority-low' },
}

export const CHANNEL_OPTIONS = ['LinkedIn', 'Referral', '官网', '猎头', '内推社区', '其他']
export const REMOTE_OPTIONS = ['远程', '混合', '到岗', '']
export const EMPLOYMENT_OPTIONS = ['全职', '实习', '合同', '兼职']
export const SALARY_CURRENCIES = ['EUR', 'USD', 'GBP', 'CNY', '其他']

// 主流程推进顺序（用于“下一步建议”）
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
