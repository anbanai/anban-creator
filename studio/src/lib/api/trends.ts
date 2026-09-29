import { http, unwrap } from '@/lib/http-client'
import type { TrendQueryResult } from '@/types'

export function normalizeTrendQueryResult(result: TrendQueryResult | null | undefined): TrendQueryResult {
  return {
    requested_at: result?.requested_at ?? '',
    ttl_seconds: result?.ttl_seconds ?? 0,
    items: Array.isArray(result?.items)
      ? result.items.filter(Boolean).map((group) => ({ ...group, items: Array.isArray(group.items) ? group.items : [] }))
      : [],
  }
}

export const trendsApi = {
  list: (params?: { platforms?: string; limit?: number }) =>
    unwrap<TrendQueryResult>(http.get('/trends', { params })).then(normalizeTrendQueryResult),
  refresh: (data: { platforms?: string[]; limit?: number }) =>
    unwrap<TrendQueryResult>(http.post('/trends/refresh', data)).then(normalizeTrendQueryResult),
}
