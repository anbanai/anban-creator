import { get } from './index'
import type { UsageStats } from '@/types'

export const usageApi = {
  stats: (params: { from: string; to: string; channel_id?: string }) =>
    get<UsageStats>('/usage/stats', params as Record<string, any>),
}
