import { http, unwrap } from '@/lib/http-client'
import type { TopicPool, PaginatedResponse } from '@/types'

export const topicPoolApi = {
  list: (channelId: string, params?: { status?: string; offset?: number; limit?: number }) =>
    unwrap<PaginatedResponse<TopicPool>>(http.get(`/channels/${channelId}/topics`, { params })),

  create: (channelId: string, data: { topics: string[] }) =>
    unwrap<{ items: TopicPool[]; count: number }>(http.post(`/channels/${channelId}/topics`, data)),

  delete: (channelId: string, id: number) =>
    unwrap<void>(http.delete(`/channels/${channelId}/topics/${id}`)),

  reset: (channelId: string, id: number) =>
    unwrap<void>(http.patch(`/channels/${channelId}/topics/${id}/reset`)),
}
