import { describe, expect, it } from 'vitest'
import { normalizeTrendQueryResult } from './trends'

describe('normalizeTrendQueryResult', () => {
  it('converts null group items into an empty array', () => {
    const result = normalizeTrendQueryResult({
      items: [
        { platform: 'weibo', label: '微博', items: null as never, stale: true },
      ],
      requested_at: '',
      ttl_seconds: 0,
    })

    expect(result.items[0].items).toEqual([])
  })

  it('converts a null response into an empty result', () => {
    expect(normalizeTrendQueryResult(null)).toEqual({ items: [], requested_at: '', ttl_seconds: 0 })
  })
})
