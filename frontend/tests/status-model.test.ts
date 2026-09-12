// 单一白名单（方案 §6.5 + 迁移 00006）：hydrateStatusModel /
// hydrateMilestoneKinds 安装服务端 GET /api/v1/meta/status-model 的 payload 后，
// 阶段字典、终态集合与「添加事件」的类型清单全部以服务端为准——前端静态表只剩
// 离线兜底职责。
// Isolated file: hydration mutates module state, so it must not leak into the
// fallback-mirror guard tests in unit.test.ts.
import { describe, expect, it } from 'vitest'
import { hydrateStatusModel, statusMeta, substatusOptions, ENDED } from '../src/lib/status'
import {
  hydrateMilestoneKinds,
  milestoneDefaultLabel,
  milestoneKindsByGroup,
  statusEffectForKind,
  type MilestoneKind,
} from '../src/lib/milestones'

describe('status-model hydration (§6.5 single whitelist)', () => {
  it('replaces labels / substatuses / terminal set from the server payload', () => {
    // Arrange
    const model = {
      stages: [
        { key: 'saved', label: '待投递', category: 'preparing', terminal: false, substatus: [] },
        {
          key: 'assessment',
          label: 'OA / 作业（改）',
          category: 'in_progress',
          terminal: false,
          substatus: [{ key: 'preparing', label: '准备 OA（改）' }],
        },
        { key: 'rejected', label: '被拒绝', category: 'ended', terminal: true, substatus: [] },
      ],
      targets: {},
    }

    // Act
    hydrateStatusModel(model)

    // Assert
    expect(statusMeta('assessment').label).toBe('OA / 作业（改）')
    expect(substatusOptions('assessment')).toEqual([{ key: 'preparing', label: '准备 OA（改）' }])
    expect(ENDED.has('rejected')).toBe(true)
    expect(ENDED.has('withdrawn')).toBe(false) // server payload is the whole truth
    expect(statusMeta('assessment').icon).toBe('🧪') // presentation stays local
  })

  it('replaces the milestone kind table from the server payload', () => {
    // Arrange: a server that renamed a kind and invented one the bundle never had.
    const kinds: MilestoneKind[] = [
      { key: 'oa', label: '在线测评（改）', status_effect: 'assessment', group: 'flow' },
      { key: 'coffee', label: '咖啡聊', status_effect: '', group: 'other' },
    ]

    // Act
    hydrateMilestoneKinds(kinds)

    // Assert
    expect(milestoneDefaultLabel('oa')).toBe('在线测评（改）')
    expect(statusEffectForKind('oa')).toBe('assessment')
    expect(statusEffectForKind('coffee')).toBe('')
    expect(milestoneKindsByGroup('flow').map((k) => k.key)).toEqual(['oa'])
    // 服务端清单没有的类型仍然可存，只是没有默认名、也不改阶段。
    expect(milestoneDefaultLabel('interview')).toBe('自定义节点')
    expect(statusEffectForKind('interview')).toBe('')
  })

  it('keeps the bundled fallback when the server omits the kind list', () => {
    // 旧后端不返回 milestone_kinds：必须保留兜底表，而不是把选择器清空。
    const before = milestoneKindsByGroup('other').map((k) => k.key)
    hydrateMilestoneKinds(undefined)
    expect(milestoneKindsByGroup('other').map((k) => k.key)).toEqual(before)
  })
})
