import { http, unwrap } from '@/lib/http-client'
import type { UsageStats } from '@/types'

export const usageApi = {
  stats: async (params?: { from?: string; to?: string; project_id?: string }): Promise<UsageStats> =>
    unwrap<UsageStats>(http.get('/usage/stats', { params })),
}
