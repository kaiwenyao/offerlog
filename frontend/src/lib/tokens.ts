// Canvas 里读不到 CSS 变量：echarts 把颜色画到 canvas 上，`var(--accent-200)`
// 这种写法在那里不会解析，只会得到一串无效颜色。所以图表配置里的 token 名字要
// 在渲染时通过 getComputedStyle 换成实际色值。
//
// 也因此，图表只能引用 token、不能写死十六进制色——将来加暗色主题时，图表跟着
// token 一起变，不需要再改一遍图表代码。
export function resolve(color: string): string {
  if (!color.startsWith('var(')) return color
  if (typeof document === 'undefined') return color
  const v = getComputedStyle(document.documentElement).getPropertyValue(color.slice(4, -1)).trim()
  return v || color
}
