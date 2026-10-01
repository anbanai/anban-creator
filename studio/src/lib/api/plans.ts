import { http, unwrap } from '@/lib/http-client'
import type { Plan, PlanEntry, CreatePlanEntryRequest, UpdatePlanEntryRequest, CreatePlanRequest, UpdatePlanRequest, PaginatedResponse, ScheduleRecommendation } from '@/types'

export const plansApi = {
  scheduleRecommendation: () =>
    unwrap<ScheduleRecommendation>(http.get('/plans/schedule-recommendation')),
  create: (data: CreatePlanRequest) =>
    unwrap<Plan>(http.post('/plans', data)),

  list: (params?: { offset?: number; limit?: number; project_id?: string }) =>
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

  listEntries: (id: string) =>
    unwrap<PlanEntry[]>(http.get(`/plans/${id}/entries`)),

  createEntry: (id: string, data: CreatePlanEntryRequest) =>
    unwrap<PlanEntry>(http.post(`/plans/${id}/entries`, data)),

  updateEntry: (planId: string, entryId: string, data: UpdatePlanEntryRequest) =>
    unwrap<PlanEntry>(http.put(`/plans/${planId}/entries/${entryId}`, data)),

  deleteEntry: (planId: string, entryId: string) =>
    unwrap<void>(http.delete(`/plans/${planId}/entries/${entryId}`)),
}
