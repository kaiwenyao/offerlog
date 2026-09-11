// 方案 §6.5 单一白名单：hydrateStatusModel / hydrateTransitions 安装服务端
// GET /api/v1/meta/status-model 的 payload 后，字典、终态集合与可达目标全部
// 以服务端为准 —— 前端静态表只剩离线兜底职责（PR #23 review P1 #5）。
// Isolated file: hydration mutates module state, so it must not leak into the
// fallback-mirror guard tests in unit.test.ts.
import { describe, expect, it } from 'vitest'
import { hydrateStatusModel, statusMeta, substatusOptions, ENDED } from '../src/lib/status'
import { allowedTargets, canTransition, hydrateTransitions } from '../src/lib/transitions'

describe('status-model hydration (§6.5 single whitelist)', () => {
  it('replaces labels / substatuses / terminal set from the server payload', () => {
    const model = {
      stages: [
        { key: 'saved', label: '待投递', category: 'preparing', terminal: false, substatus: [] },
        { key: 'assessment', label: 'OA / 作业（改）', category: 'in_progress', terminal: false, substatus: [{ key: 'preparing', label: '准备 OA（改）' }] },
        { key: 'rejected', label: '被拒绝', category: 'ended', terminal: true, substatus: [] },
      ],
      targets: {
        saved: [{ status: 'assessment' }, { status: 'rejected' }],
        assessment: [{ status: 'rejected' }],
        rejected: [],
      },
    }
    hydrateStatusModel(model)
    hydrateTransitions(model.targets)
    expect(statusMeta('assessment').label).toBe('OA / 作业（改）')
    expect(substatusOptions('assessment')).toEqual([{ key: 'preparing', label: '准备 OA（改）' }])
    expect(ENDED.has('rejected')).toBe(true)
    expect(ENDED.has('withdrawn')).toBe(false) // server payload is the whole truth
    expect(canTransition('saved', 'assessment')).toBe(true)
    expect(canTransition('saved', 'interviewing')).toBe(false) // server forbids it
    expect(allowedTargets('saved').map((s) => s.key)).toEqual(['assessment', 'rejected'])
    expect(statusMeta('assessment').icon).toBe('🧪') // presentation stays local
  })
})
