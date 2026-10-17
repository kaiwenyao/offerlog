import type { Snippet } from '../../lib/types'

/** 「全部」筛选的哨兵值；真实分类是自由文本，"" 表示未分类。 */
export const ALL_CATEGORIES = '__all__'

/** Distinct categories with counts, in first-seen order (the server already
 *  sorts by category with 未分类 last). */
export function snippetCategories(items: Snippet[]): Array<{ category: string; count: number }> {
  const counts = new Map<string, number>()
  for (const s of items) counts.set(s.category, (counts.get(s.category) ?? 0) + 1)
  return [...counts].map(([category, count]) => ({ category, count }))
}

/** Whitespace-separated terms, every one must appear (case-insensitive) in the
 *  title, category or content — the same AND semantics as the ⌘K search. */
export function matchSnippet(s: Snippet, query: string): boolean {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return true
  const hay = `${s.title}\n${s.category}\n${s.content}`.toLowerCase()
  return terms.every((t) => hay.includes(t))
}

export function filterSnippets(items: Snippet[], category: string, query: string): Snippet[] {
  return items.filter((s) => (category === ALL_CATEGORIES || s.category === category) && matchSnippet(s, query))
}

/** Copy to the clipboard; resolves false when the browser refuses so the
 *  caller can say so instead of lying. navigator.clipboard only exists on
 *  secure origins (an http:// self-hosted deployment has none), so fall back to
 *  the legacy execCommand path with an off-screen textarea. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return legacyCopy(text)
  }
}

function legacyCopy(text: string): boolean {
  const active = document.activeElement as HTMLElement | null
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.position = 'fixed'
  ta.style.top = '-1000px'
  document.body.appendChild(ta)
  ta.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  ta.remove()
  // 归还焦点：⌘K 面板里复制完还要靠输入框接收 Esc。
  active?.focus()
  return ok
}
