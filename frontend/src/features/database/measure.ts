// 用隐藏探针量出真实文字宽度（含 CJK、字重、字体回退），供列宽自适应使用。
// 探针只创建一次，结果按「字重|字号|文字」缓存，避免 60 行 × N 列反复触发布局。
import { estimateTextWidth, type TextMeasure } from './columns'

export interface TextMeasurer extends TextMeasure {
  /** 丢掉缓存（字体加载完成后需要重量一遍）。 */
  reset(): void
  /** 移除探针节点（组件卸载时调用）。 */
  dispose(): void
}

export function createTextMeasurer(): TextMeasurer {
  const cache = new Map<string, number>()
  let probe: HTMLSpanElement | null = null
  let family = ''

  const measure: TextMeasurer = (text, px, weight) => {
    if (!text) return 0
    const key = `${weight}|${px}|${text}`
    const hit = cache.get(key)
    if (hit != null) return hit
    if (typeof document === 'undefined' || !document.body) {
      const est = estimateTextWidth(text, px, weight)
      cache.set(key, est)
      return est
    }
    if (!probe) {
      const body = getComputedStyle(document.body)
      family = body.fontFamily || 'sans-serif'
      probe = document.createElement('span')
      probe.setAttribute('aria-hidden', 'true')
      probe.style.cssText =
        'position:absolute;top:-9999px;left:-9999px;visibility:hidden;white-space:nowrap;pointer-events:none'
      document.body.appendChild(probe)
    }
    probe.style.font = `${weight} ${px}px ${family}`
    probe.textContent = text
    const width = probe.getBoundingClientRect().width
    // jsdom（无布局）与字体加载前都会量出 0，此时用字符估算兜底。
    const out = width > 0 ? width : estimateTextWidth(text, px, weight)
    cache.set(key, out)
    return out
  }

  measure.reset = () => cache.clear()
  measure.dispose = () => {
    probe?.remove()
    probe = null
    cache.clear()
  }
  return measure
}
