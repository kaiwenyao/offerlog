// CSRF token 存在 localStorage；禁用站点存储时 getItem/setItem 会抛 SecurityError，
// 模块求值阶段的裸读会白屏。读写都必须吞掉这个异常。
import { afterEach, describe, expect, it, vi } from 'vitest'
import { readCsrfStorage, setCsrf, writeCsrfStorage } from '../src/lib/api'

const CSRF_KEY = 'offerlog.csrf'

describe('csrf storage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    try {
      localStorage.removeItem(CSRF_KEY)
    } catch {
      /* stub already gone */
    }
  })

  it('round-trips a token through localStorage', () => {
    writeCsrfStorage('tok-1')
    expect(readCsrfStorage()).toBe('tok-1')
    writeCsrfStorage(null)
    expect(readCsrfStorage()).toBeNull()
  })

  it('does not throw when localStorage is blocked', () => {
    const boom = {
      getItem() {
        throw new DOMException('denied', 'SecurityError')
      },
      setItem() {
        throw new DOMException('denied', 'SecurityError')
      },
      removeItem() {
        throw new DOMException('denied', 'SecurityError')
      },
    }
    vi.stubGlobal('localStorage', boom)
    expect(readCsrfStorage()).toBeNull()
    expect(() => writeCsrfStorage('tok')).not.toThrow()
    expect(() => setCsrf('tok')).not.toThrow()
    expect(() => setCsrf(null)).not.toThrow()
  })
})
