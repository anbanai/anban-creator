import { get, post, patch, del } from './request'
import type { PaginatedResponse, TopicPool } from '@/types'

export const topicPoolApi = {
  list: (projectId: string, params?: { status?: string; offset?: number; limit?: number }) =>
    get<PaginatedResponse<TopicPool>>(`/projects/${projectId}/topics`, params as Record<string, any>),

  create: (projectId: string, data: { topics: string[] }) =>
    post<{ items: TopicPool[]; count: number }>(`/projects/${projectId}/topics`, data),

  delete: (projectId: string, id: number) =>
    del<void>(`/projects/${projectId}/topics/${id}`),

  reset: (projectId: string, id: number) =>
    patch<void>(`/projects/${projectId}/topics/${id}/reset`),
}
