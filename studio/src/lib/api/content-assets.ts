import { http, unwrap } from '@/lib/http-client'
import type { ContentAsset, ContentAssetDetail, ContentAssetLineage, ContentAssetRecommendation, ContentAssetScoreSnapshot, ContentAutopilotPolicy, ContentReuseRun } from '@/types'

export const contentAssetsApi = {
  list: (params?: { status?: string; q?: string; platform?: string; source?: string; lifecycle?: string; project_id?: string; offset?: number; limit?: number }) =>
    unwrap<{ items: ContentAsset[]; total: number }>(http.get('/content-assets', { params })),
  recommendations: (limit = 5) =>
    unwrap<{ items: ContentAssetRecommendation[] }>(http.get('/content-assets/recommendations', { params: { limit } })),
  create: (data: { title: string; core_question?: string; audience?: string; semantic_kernel?: string; lifecycle?: string }) =>
    unwrap<ContentAsset>(http.post('/content-assets', data)),
  ingest: (data: { title: string; core_question?: string; audience?: string; semantic_kernel?: string; lifecycle?: string; evidence?: unknown[] }) =>
    unwrap<ContentAssetDetail>(http.post('/content-assets/ingest', data)),
  get: (id: string) => unwrap<ContentAssetDetail>(http.get(`/content-assets/${id}`)),
  getLineage: (id: string) => unwrap<ContentAssetLineage>(http.get(`/content-assets/${id}/lineage`)),
  forContent: (params: { content_kind: string; content_id: string; project_id?: string }) => unwrap<{ items: ContentAssetLineage[] }>(http.get('/content-assets/by-content', { params })),
  update: (id: string, data: Partial<Pick<ContentAsset, 'title' | 'core_question' | 'audience' | 'semantic_kernel' | 'lifecycle'>>) =>
    unwrap<ContentAsset>(http.patch(`/content-assets/${id}`, data)),
  score: (id: string, project_id: string) => unwrap<ContentAssetScoreSnapshot>(http.post(`/content-assets/${id}/score`, { project_id })),
  createReuseRun: (id: string, data: { target_project_id: string; target_platform: string; source_content_kind?: string; source_content_id?: string; target_agent_id?: string; idempotency_key?: string }) =>
    unwrap<ContentReuseRun>(http.post(`/content-assets/${id}/reuse-runs`, data)),
  getRun: (id: string) => unwrap<ContentReuseRun>(http.get(`/content-reuse-runs/${id}`)),
  listRuns: (params?: { asset_id?: string; target_project_id?: string; target_platform?: string; status?: string; stage?: string; offset?: number; limit?: number }) =>
    unwrap<{ items: ContentReuseRun[]; total: number; offset: number; limit: number }>(http.get('/content-reuse-runs', { params })),
  setRunStatus: (id: string, status: 'paused' | 'queued' | 'cancelled') =>
    unwrap<ContentReuseRun>(http.post(`/content-reuse-runs/${id}/${status}`)),
  getPolicy: () => unwrap<ContentAutopilotPolicy>(http.get('/content-autopilot-policy')),
  updatePolicy: (data: Partial<ContentAutopilotPolicy>) => unwrap<ContentAutopilotPolicy>(http.put('/content-autopilot-policy', data)),
}
