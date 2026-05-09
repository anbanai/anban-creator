import { get, post } from './index'
import type { ViralAnalysis, CreateViralAnalysisRequest, PaginatedResponse } from '@/types'

export const viralAnalysesApi = {
  create: (data: CreateViralAnalysisRequest) =>
    post<ViralAnalysis>('/viral-analyses', data),

  get: (id: string) =>
    get<ViralAnalysis>(`/viral-analyses/${id}`),

  list: (params?: { limit?: number; offset?: number }) =>
    get<PaginatedResponse<ViralAnalysis>>('/viral-analyses', params as Record<string, any>),
}
