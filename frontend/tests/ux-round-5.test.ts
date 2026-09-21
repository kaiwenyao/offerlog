// 这一轮修的是数据库页上五个「界面和地址/缓存对不上」的 bug。能提成纯函数的
// 都在这里钉住语义；弹窗里的 409 刷新、侧栏计数失效发生在组件路径上。
import { describe, expect, it } from 'vitest'
import { formatLabel, FORMATS } from '../src/features/database/forms'
import {
  ARCHIVED_VIEW,
  databaseDrawerPath,
  hrefWithoutSearch,
  isDrawerOnlyNavigation,
  layoutForView,
  trashModeFromParams,
  viewIdFromParams,
} from '../src/features/database/views'

describe('formatLabel: 面试形式显示中文', () => {
  // 原始 bug：详情和首页直接把 i.format 渲染出来，于是「一面 · video」。
  // 表单的 FORMATS 映射表一直在，只是没用到。
  it('maps every option the form offers', () => {
    expect(formatLabel('phone')).toBe('电话')
    expect(formatLabel('video')).toBe('视频')
    expect(formatLabel('onsite')).toBe('到面')
    expect(formatLabel('takehome')).toBe('作业')
    expect(FORMATS.map((f) => f.value).sort()).toEqual(['onsite', 'phone', 'takehome', 'video'])
  })

  it('empty / missing format renders as empty (caller supplies 待定)', () => {
    expect(formatLabel('')).toBe('')
    expect(formatLabel(null)).toBe('')
    expect(formatLabel(undefined)).toBe('')
  })

  it('unknown values pass through instead of disappearing', () => {
    expect(formatLabel('other')).toBe('other')
  })
})

describe('viewIdFromParams: 裸 /database 就是全部机会', () => {
  // 原始 bug：q === null 时不重置 viewId，点搜索 chip 的 × 跳到 /database
  // 后界面还停在「已归档」，F5 同一条链接却变成 11 条「全部机会」。
  it('missing view param is 全部机会 (-1)', () => {
    expect(viewIdFromParams(new URLSearchParams())).toBe(-1)
    expect(viewIdFromParams(new URLSearchParams('q=字节'))).toBe(-1)
  })

  it('reads the view id from the query string', () => {
    expect(viewIdFromParams(new URLSearchParams(`view=${ARCHIVED_VIEW}`))).toBe(ARCHIVED_VIEW)
    expect(viewIdFromParams(new URLSearchParams('view=-1'))).toBe(-1)
    expect(viewIdFromParams(new URLSearchParams('view=-3&q=腾讯'))).toBe(-3)
  })

  it('garbage view values fall back to 全部机会, not NaN', () => {
    expect(viewIdFromParams(new URLSearchParams('view='))).toBe(-1)
    expect(viewIdFromParams(new URLSearchParams('view=nope'))).toBe(-1)
  })
})

describe('trashModeFromParams: 回收站跟地址栏走', () => {
  it('reads trash=1 / trash=true and ignores other values', () => {
    expect(trashModeFromParams(new URLSearchParams())).toBe(false)
    expect(trashModeFromParams(new URLSearchParams('view=-1'))).toBe(false)
    expect(trashModeFromParams(new URLSearchParams('trash=1'))).toBe(true)
    expect(trashModeFromParams(new URLSearchParams('trash=true'))).toBe(true)
    expect(trashModeFromParams(new URLSearchParams('trash=0'))).toBe(false)
    expect(trashModeFromParams(new URLSearchParams('view=-3&layout=board&trash=1'))).toBe(true)
  })
})

describe('layoutForView: 回收站强制表格', () => {
  it('locks board/list to table inside the recycle bin', () => {
    expect(layoutForView('board', true)).toBe('table')
    expect(layoutForView('list', true)).toBe('table')
    expect(layoutForView('table', true)).toBe('table')
  })

  it('leaves the chosen layout alone outside the recycle bin', () => {
    expect(layoutForView('board', false)).toBe('board')
    expect(layoutForView('list', false)).toBe('list')
    expect(layoutForView('table', false)).toBe('table')
  })
})

describe('hrefWithoutSearch: 搜索 chip 的 × 只拿掉 q', () => {
  it('keeps the active view so URL and chips still agree', () => {
    expect(hrefWithoutSearch(`view=${ARCHIVED_VIEW}&q=字节`)).toBe(`/database?view=${ARCHIVED_VIEW}`)
    expect(hrefWithoutSearch('?view=-3&layout=board&q=腾讯')).toBe('/database?view=-3&layout=board')
  })

  it('bare search falls back to /database', () => {
    expect(hrefWithoutSearch('q=字节')).toBe('/database')
    expect(hrefWithoutSearch('')).toBe('/database')
  })

  it('keeps trash so cancelling search does not eject the recycle bin', () => {
    expect(hrefWithoutSearch('trash=1&q=字节')).toBe('/database?trash=1')
    expect(hrefWithoutSearch('view=-1&layout=table&trash=1&q=腾讯')).toBe('/database?view=-1&layout=table&trash=1')
  })
})

describe('databaseDrawerPath: 抽屉跟 URL 走', () => {
  // 原始 bug：selApp 只用 routeApp 做初始值，关闭不导航，于是 /database/1900
  // 关掉后再点腾讯，地址栏还是 1900，刷新/分享打开的还是小红书。
  it('opens on /database/:id and closes back to /database', () => {
    expect(databaseDrawerPath(1900, '')).toEqual({ pathname: '/database/1900', search: '' })
    expect(databaseDrawerPath(null, '')).toEqual({ pathname: '/database', search: '' })
  })

  it('keeps the current view / layout / q when opening or closing', () => {
    const search = `?view=${ARCHIVED_VIEW}`
    expect(databaseDrawerPath(42, search)).toEqual({ pathname: '/database/42', search })
    expect(databaseDrawerPath(null, search)).toEqual({ pathname: '/database', search })
  })
})

describe('isDrawerOnlyNavigation: 开抽屉不该重置列表状态', () => {
  it('is true when only the app id in the path changes', () => {
    const q = '?view=-103'
    expect(isDrawerOnlyNavigation('/database', '/database/1900', q, q)).toBe(true)
    expect(isDrawerOnlyNavigation('/database/1900', '/database', q, q)).toBe(true)
    expect(isDrawerOnlyNavigation('/database/1900', '/database/42', q, q)).toBe(true)
  })

  it('is false when the query string changes (search / view / layout)', () => {
    expect(isDrawerOnlyNavigation('/database/1900', '/database', '?view=-103', '?q=腾讯')).toBe(false)
    expect(isDrawerOnlyNavigation('/database', '/database', '', '?q=字节')).toBe(false)
  })

  it('is false for unrelated routes', () => {
    expect(isDrawerOnlyNavigation('/database', '/calendar', '', '')).toBe(false)
    expect(isDrawerOnlyNavigation('/apps/1', '/database', '', '')).toBe(false)
  })
})
