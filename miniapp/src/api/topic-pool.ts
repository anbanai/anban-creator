import { get, post, patch, del } from './request'
import type { PaginatedResponse, TopicPool } from '@/types'

export const topicPoolApi = {
  list: (channelId: string, params?: { status?: string; offset?: number; limit?: number }) =>
    get<PaginatedResponse<TopicPool>>(`/channels/${channelId}/topics`, params as Record<string, any>),

  create: (channelId: string, data: { topics: string[] }) =>
    post<{ items: TopicPool[]; count: number }>(`/channels/${channelId}/topics`, data),

  delete: (channelId: string, id: number) =>
    del<void>(`/channels/${channelId}/topics/${id}`),

  reset: (channelId: string, id: number) =>
    patch<void>(`/channels/${channelId}/topics/${id}/reset`),
}
