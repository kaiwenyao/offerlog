import { useState } from 'react'
import {
  milestoneKindsByGroup,
  milestoneIcon,
  milestoneDotColor,
  type MilestoneKind,
} from '../lib/milestones'

/**
 * 参考流程图。
 *
 * 这是抽屉里「阶段轨迹」工序线的继任者，但它表达的是完全相反的意思：工序线画的是
 * 一条**每个岗位都必须走**的流水线，走不到的格子留下「未经历」的空洞；参考流程图
 * 画的是一条**建议路线**，每一步都可以跳过——不是每个岗位都有 OA 或初筛。
 *
 * 它同时是最快的记录入口：点任何一个节点就打开「添加事件」并预填好类型。已经记过
 * 的节点实心高亮，没记过的保持空心——空心表示「还没发生 / 用不上」，不是缺失，
 * 所以这里不写「未经历」。
 */
export interface FlowGuideProps {
  /** 已经在这个岗位上记录过的阶段（来自时间线上的事件与节点）。 */
  reached: Set<string>
  /** 点击某个节点：用这个事件类型打开「添加事件」。 */
  onPick: (kind: string) => void
  /** 折叠态的初始值；默认展开，第一次用的人需要看见这条线。 */
  defaultOpen?: boolean
}

/** 主线上常常用不上的步骤——画成虚线边框，并在 title 里说明。 */
const OPTIONAL_STEPS = new Set(['prepare', 'screen', 'oa'])

function NodeButton({
  kind,
  reached,
  onPick,
}: {
  kind: MilestoneKind
  reached: boolean
  onPick: (kind: string) => void
}) {
  const color = milestoneDotColor(kind.key)
  const optional = OPTIONAL_STEPS.has(kind.key)
  const title = optional ? `${kind.label}（这一步可跳过）· 点击记录` : `${kind.label} · 点击记录`
  return (
    <button
      type="button"
      onClick={() => onPick(kind.key)}
      title={title}
      aria-label={title}
      data-kind={kind.key}
      className="flow-node"
      style={{
        background: reached ? color : 'transparent',
        color: reached ? '#fff' : 'var(--text-muted)',
        borderColor: reached ? color : 'var(--border)',
        borderStyle: optional && !reached ? 'dashed' : 'solid',
      }}
    >
      <span aria-hidden>{milestoneIcon(kind.key)}</span>
      <span>{kind.label}</span>
    </button>
  )
}

function Arrow() {
  return (
    <span aria-hidden style={{ color: 'var(--neutral-500)', fontSize: 12, lineHeight: 1 }}>
      →
    </span>
  )
}

export function FlowGuide({ reached, onPick, defaultOpen = true }: FlowGuideProps) {
  const [open, setOpen] = useState(defaultOpen)
  const flow = milestoneKindsByGroup('flow')
  const endings = milestoneKindsByGroup('end')
  const others = milestoneKindsByGroup('other')
  const isReached = (k: MilestoneKind) => k.status_effect !== '' && reached.has(k.status_effect)

  return (
    <div className="flow-guide">
      <button
        type="button"
        className="flow-guide-toggle"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <span aria-hidden>{open ? '▾' : '▸'}</span> 参考流程
        <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>
          （点节点快速记录，每一步都可跳过）
        </span>
      </button>

      {open && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)', marginTop: 8 }}>
          <div className="flow-row">
            {flow.map((k, i) => (
              <span key={k.key} style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                {i > 0 && <Arrow />}
                <NodeButton kind={k} reached={isReached(k)} onPick={onPick} />
              </span>
            ))}
          </div>

          <div className="flow-row">
            <span style={{ font: 'var(--type-caption)', color: 'var(--text-muted)' }}>随时可以：</span>
            {[...endings, ...others].map((k) => (
              <NodeButton key={k.key} kind={k} reached={isReached(k)} onPick={onPick} />
            ))}
          </div>

          <p style={{ margin: 0, font: 'var(--type-caption)', color: 'var(--text-muted)', lineHeight: 1.5 }}>
            虚线框的步骤常常用不上——没有 OA 或初筛的岗位，直接记下一步就行。
            时间线按你填的发生时间自动排序，当前状态取最后一个事件。
          </p>
        </div>
      )}
    </div>
  )
}
