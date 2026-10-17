import { describe, expect, it } from 'vitest'
import { ALL_CATEGORIES, filterSnippets, matchSnippet, snippetCategories } from '../src/features/snippets/filter'
import type { Snippet } from '../src/lib/types'

const sn = (id: number, title: string, category: string, content = ''): Snippet => ({
  id, title, category, content, created_at: '', updated_at: '',
})

const items = [
  sn(1, '字节实习', '实习经历', 'Built recall pipeline'),
  sn(2, '腾讯实习', '实习经历', '负责广告投放'),
  sn(3, 'Why us', ''),
]

describe('snippets filter', () => {
  it('lists distinct categories with counts in server order', () => {
    expect(snippetCategories(items)).toEqual([
      { category: '实习经历', count: 2 },
      { category: '', count: 1 },
    ])
  })

  it('matches every term across title, category and content (case-insensitive)', () => {
    expect(matchSnippet(items[0], 'RECALL 字节')).toBe(true)
    expect(matchSnippet(items[0], 'recall 腾讯')).toBe(false)
    expect(matchSnippet(items[1], '实习经历')).toBe(true)
    expect(matchSnippet(items[2], '   ')).toBe(true)
  })

  it('filters by category and query together', () => {
    expect(filterSnippets(items, ALL_CATEGORIES, '').map((s) => s.id)).toEqual([1, 2, 3])
    expect(filterSnippets(items, '实习经历', '').map((s) => s.id)).toEqual([1, 2])
    expect(filterSnippets(items, '', '').map((s) => s.id)).toEqual([3])
    expect(filterSnippets(items, '实习经历', '广告').map((s) => s.id)).toEqual([2])
  })
})
