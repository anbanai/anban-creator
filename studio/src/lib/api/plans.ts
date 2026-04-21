import { http, unwrap } from '@/lib/http-client'
import type { Plan, CreatePlanRequest, UpdatePlanRequest, PaginatedResponse } from '@/types'

export const plansApi = {
  create: (data: CreatePlanRequest) =>
    unwrap<Plan>(http.post('/plans', data)),

  list: (params?: { offset?: number; limit?: number; channel_id?: string }) =>
    unwrap<PaginatedResponse<Plan>>(http.get('/plans', { params })),

  get: (id: string) =>
    unwrap<Plan>(http.get(`/plans/${id}`)),

  update: (id: string, data: UpdatePlanRequest) =>
    unwrap<Plan>(http.put(`/plans/${id}`, data)),

  delete: (id: string) =>
    unwrap<void>(http.delete(`/plans/${id}`)),

  pause: (id: string) =>
    unwrap<void>(http.post(`/plans/${id}/pause`)),

  resume: (id: string) =>
    unwrap<void>(http.post(`/plans/${id}/resume`)),
}
