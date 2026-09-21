// React Query v5: isLoading === isPending && isFetching。首次失败后重试被挂起时
// 两者都是 false，列表会把「还在等」画成空状态。queryListState 用 isPending。
import { describe, expect, it } from 'vitest'
import { queryListState } from '../src/lib/queryState'

describe('queryListState', () => {
  it('treats pending + paused (isLoading=false, isError=false) as loading, not empty', () => {
    expect(queryListState({ isPending: true, isError: false })).toBe('loading')
  })

  it('shows the error branch after retries are exhausted', () => {
    expect(queryListState({ isPending: false, isError: true })).toBe('error')
  })

  it('is ready when the query has settled with data (or a real empty result)', () => {
    expect(queryListState({ isPending: false, isError: false })).toBe('ready')
  })
})
