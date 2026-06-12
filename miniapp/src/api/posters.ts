import { get, post } from './request'
import type { PosterTask, CreatePosterRequest, PaginatedResponse } from '@/types'

export const postersApi = {
  create: (data: CreatePosterRequest) =>
    post<PosterTask>('/posters', data),

  get: (id: string) =>
    get<PosterTask>(`/posters/${id}`),

  list: (params?: { limit?: number; offset?: number }) =>
    get<PaginatedResponse<PosterTask>>('/posters', params as Record<string, any>),
}
