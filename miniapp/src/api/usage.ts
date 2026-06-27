import { get } from './request'
import type { UsageStats } from '@/types'

export const usageApi = {
  stats: (params: { from: string; to: string; project_id?: string }) =>
    get<UsageStats>('/usage/stats', params as Record<string, any>),
}
