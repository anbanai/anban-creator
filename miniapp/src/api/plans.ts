import { get, post, put, del } from './request'
import type { Plan, CreatePlanRequest, UpdatePlanRequest, PaginatedResponse } from '@/types'

export interface ListPlansParams {
  project_id?: string
  limit?: number
  offset?: number
}

export const plansApi = {
  list: (params?: ListPlansParams) =>
    get<PaginatedResponse<Plan>>('/plans', params as Record<string, any>),

  get: (id: string) =>
    get<Plan>(`/plans/${id}`),

  create: (data: CreatePlanRequest) =>
    post<Plan>('/plans', data),

  update: (id: string, data: UpdatePlanRequest) =>
    put<Plan>(`/plans/${id}`, data),

  delete: (id: string) =>
    del<void>(`/plans/${id}`),

  pause: (id: string) =>
    post<void>(`/plans/${id}/pause`),

  resume: (id: string) =>
    post<void>(`/plans/${id}/resume`),
}
