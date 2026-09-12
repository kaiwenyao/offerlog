// 事件类型：用户在「＋ 添加事件」里挑的那一栏，也是参考流程图画出来的那条线。
//
// 迁移 00006 起，记录进度的唯一方式就是往时间线上加事件。用户只说「发生了什么」，
// 岗位阶段由后端推导——每个 kind 携带一个 status_effect，当前状态 = 时间线上最后
// 一个带 status_effect 的节点。前端这份表只负责渲染（名称、图标、颜色、分组）。
//
// 权威清单来自 GET /api/v1/meta/status-model 的 milestone_kinds（与后端
// applications/domain/milestone.go 同源）；下面的静态表只是离线 / 首屏兜底，
// 与 status.ts 里 FALLBACK_LABELS 的处理方式一致。

import { statusMeta } from './status'

export type MilestoneGroup = 'flow' | 'end' | 'other'

export interface MilestoneKind {
  key: string
  label: string
  /** 这一步把岗位带进哪个阶段；'' = 只记事，不改阶段。 */
  status_effect: string
  group: MilestoneGroup
}

/** 离线 / 首屏兜底。服务端返回后整体替换（见 hydrateMilestoneKinds）。 */
export const FALLBACK_MILESTONE_KINDS: MilestoneKind[] = [
  { key: 'save', label: '收藏岗位', status_effect: 'saved', group: 'flow' },
  { key: 'prepare', label: '准备材料', status_effect: 'preparing', group: 'flow' },
  { key: 'apply', label: '投递', status_effect: 'applied', group: 'flow' },
  { key: 'screen', label: '初筛', status_effect: 'screening', group: 'flow' },
  { key: 'oa', label: 'OA / 笔试', status_effect: 'assessment', group: 'flow' },
  { key: 'interview', label: '面试', status_effect: 'interviewing', group: 'flow' },
  { key: 'offer', label: '收到 Offer', status_effect: 'offer', group: 'flow' },
  { key: 'accept', label: '接受 Offer', status_effect: 'accepted', group: 'end' },
  { key: 'reject', label: '被拒绝', status_effect: 'rejected', group: 'end' },
  { key: 'withdraw', label: '撤回申请', status_effect: 'withdrawn', group: 'end' },
  { key: 'close', label: '岗位关闭', status_effect: 'closed', group: 'end' },
  { key: 'phone', label: '电话沟通', status_effect: '', group: 'other' },
  { key: 'custom', label: '自定义事件', status_effect: '', group: 'other' },
]

export let MILESTONE_KINDS: MilestoneKind[] = FALLBACK_MILESTONE_KINDS

let byKey = new Map(MILESTONE_KINDS.map((k) => [k.key, k]))

/**
 * Replace the local table with the server's list. Must run before the app tree
 * renders, so no component caches the offline mirror.
 */
export function hydrateMilestoneKinds(kinds: MilestoneKind[] | null | undefined): void {
  if (!kinds || kinds.length === 0) return
  MILESTONE_KINDS = kinds
  byKey = new Map(MILESTONE_KINDS.map((k) => [k.key, k]))
}

/** 建议清单里的类型才有已知含义；用户自创的 slug 照存不误，只是没有默认名。 */
export function isKnownKind(kind: string): boolean {
  return byKey.has(kind)
}

/** 类型 → 默认名称。未知类型回落到「自定义节点」（与后端同一措辞）。 */
export function milestoneDefaultLabel(kind: string): string {
  return byKey.get(kind)?.label ?? '自定义节点'
}

/** 类型 → 阶段效果；未知类型不改阶段。 */
export function statusEffectForKind(kind: string): string {
  return byKey.get(kind)?.status_effect ?? ''
}

export function milestoneKindsByGroup(group: MilestoneGroup): MilestoneKind[] {
  return MILESTONE_KINDS.filter((k) => k.group === group)
}

/** 下拉里分组的渲染顺序。 */
export const MILESTONE_KIND_GROUPS: MilestoneGroup[] = ['flow', 'end', 'other']

/**
 * 这个名称是否还是某个类型的默认名。用来判断「用户改过名字没有」：改过就保留，
 * 没改过就跟着类型走。
 */
export function isDefaultLabel(label: string): boolean {
  return MILESTONE_KINDS.some((k) => k.label === label)
}

/** 分组的中文小标题，用在事件类型下拉里。 */
export const MILESTONE_GROUP_LABEL: Record<MilestoneGroup, string> = {
  flow: '推进流程',
  end: '结束',
  other: '其他（不改变状态）',
}

/**
 * 节点的颜色直接沿用它所推进到的那个阶段的语义色，这样时间线上的圆点、顶部的
 * 状态芯片、表格里的工序线说的是同一件事。不改阶段的事件保持中性灰。
 */
export function milestoneDotColor(kind: string): string {
  const effect = statusEffectForKind(kind)
  return effect ? statusMeta(effect).dot : 'var(--neutral)'
}

/** 节点图标：有阶段效果的沿用阶段图标，其余用一个中性的「记一笔」。 */
export function milestoneIcon(kind: string): string {
  const effect = statusEffectForKind(kind)
  if (effect) return statusMeta(effect).icon
  return kind === 'phone' ? '☎️' : '📌'
}
