import { http, unwrap } from '@/lib/http-client'
import type { TrendQueryResult } from '@/types'

export const trendsApi = {
  list: (params?: { platforms?: string; limit?: number }) =>
    unwrap<TrendQueryResult>(http.get('/trends', { params })),
  refresh: (data: { platforms?: string[]; limit?: number }) =>
    unwrap<TrendQueryResult>(http.post('/trends/refresh', data)),
}
