/**
 * 列表页该画转圈、错误还是内容。
 *
 * React Query v5 的 isLoading === isPending && isFetching。首次请求失败后
 * 重试被挂起（后台标签 / 离线）时 status 仍是 pending、fetchStatus 是 paused，
 * 于是 isLoading 和 isError 都是 false，页面会把「还在等」画成空列表。
 * 用 isPending（有没有数据）而不是 isLoading（是不是正在打网络）。
 */
export type QueryListState = 'loading' | 'error' | 'ready'

export function queryListState(q: { isPending: boolean; isError: boolean }): QueryListState {
  if (q.isPending) return 'loading'
  if (q.isError) return 'error'
  return 'ready'
}
