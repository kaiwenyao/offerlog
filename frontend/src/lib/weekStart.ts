import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Preferences } from './types'
import { DEFAULT_WEEK_START, normalizeWeekStart } from '../features/calendar/grid'

/**
 * 「每周起始日」偏好（0 = 周日 … 6 = 周六）。
 *
 * 设置页把它存在服务端，后端的首页摘要一直按它算周窗口与「本周工序」的 7 列；
 * 日历页和数据库页的「本周面试」却写死了周一，于是把起始日改成周日之后，同一个
 * 「本周」在两个页面上差了一天。这个 hook 让它们读同一份偏好——与设置页共用
 * ['preferences'] 这一条缓存，不会多发请求。
 *
 * 偏好还没到手（首屏 / 请求失败）时退回周一，与后端默认值一致。
 */
export function useWeekStart(): number {
  const q = useQuery({
    queryKey: ['preferences'],
    queryFn: () => api.get<Preferences>('/api/v1/preferences'),
    staleTime: 5 * 60_000,
  })
  return q.data ? normalizeWeekStart(q.data.week_start) : DEFAULT_WEEK_START
}
