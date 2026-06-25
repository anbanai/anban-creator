import { http, unwrap } from '@/lib/http-client'
import type { TopicPool, PaginatedResponse } from '@/types'

export const topicPoolApi = {
  list: (projectId: string, params?: { status?: string; offset?: number; limit?: number }) =>
    unwrap<PaginatedResponse<TopicPool>>(http.get(`/projects/${projectId}/topics`, { params })),

  create: (projectId: string, data: { topics: string[] }) =>
    unwrap<{ items: TopicPool[]; count: number }>(http.post(`/projects/${projectId}/topics`, data)),

  delete: (projectId: string, id: number) =>
    unwrap<void>(http.delete(`/projects/${projectId}/topics/${id}`)),

  reset: (projectId: string, id: number) =>
    unwrap<void>(http.patch(`/projects/${projectId}/topics/${id}/reset`)),
}
