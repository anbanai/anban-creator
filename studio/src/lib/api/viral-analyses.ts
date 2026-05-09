import { http, unwrap } from '@/lib/http-client'
import type { ViralAnalysis, CreateViralAnalysisRequest } from '@/types'

export const viralAnalysesApi = {
  create: (data: CreateViralAnalysisRequest) =>
    unwrap<ViralAnalysis>(http.post('/viral-analyses', data)),

  get: (id: string) =>
    unwrap<ViralAnalysis>(http.get(`/viral-analyses/${id}`)),

  list: (params?: { offset?: number; limit?: number }) =>
    unwrap<{ items: ViralAnalysis[]; total: number }>(http.get('/viral-analyses', { params })),
}
