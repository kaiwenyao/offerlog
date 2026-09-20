/**
 * 背景滚动锁，按「还有几层浮层开着」计数。
 *
 * 为什么不数 DOM：原来 Dialog 和抽屉各自数 `.modal-backdrop` 的个数来决定要不要
 * 解锁，但 React 卸载时**先移除 DOM、后跑 effect 清理**，所以清理函数里数到的
 * 永远少一层。抽屉里打开任意一个弹窗再关掉，那次清理就把抽屉自己加的锁一起摘了
 * ——弹窗关上之后，背后的长列表又能跟着滚轮滚。
 *
 * 计数放在模块作用域里：谁加锁谁负责调用返回的解锁函数，加几次就要解几次，最后
 * 一层解开时才真的放开 body。
 */
let depth = 0

/** 加一层滚动锁，返回解锁函数（重复调用只生效一次）。 */
export function lockScroll(): () => void {
  depth += 1
  if (depth === 1) document.body.classList.add('ol-modal-open')
  let released = false
  return () => {
    if (released) return
    released = true
    depth = Math.max(0, depth - 1)
    if (depth === 0) document.body.classList.remove('ol-modal-open')
  }
}
