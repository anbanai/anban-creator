import { http, unwrap } from '@/lib/http-client'
import type { ViralAnalysis } from '@/types'

export const viralAnalysesApi = {
  get: (id: string) =>
    unwrap<ViralAnalysis>(http.get(`/viral-analyses/${id}`)),

  list: (params?: { offset?: number; limit?: number }) =>
    unwrap<{ items: ViralAnalysis[]; total: number }>(http.get('/viral-analyses', { params })),
}
