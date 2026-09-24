import { http, unwrap } from '@/lib/http-client'
import type { AnalyticsImportPayload, AnalyticsPreview } from '@/types/content-analytics'
import type { SeednoteImportBatch, SeednoteImportOverview, SeednoteImportSummary, SeednoteMetricVersion, SeednotePostIdentity } from '@/types/seednote-import'

export const seednoteImportApi = {
  preview: (projectId: string, payload: { upload_id: string; timezone?: string }) => unwrap<AnalyticsPreview>(http.post(`/projects/${projectId}/seednote-analytics/imports/preview`, payload)),
  import: (projectId: string, payload: AnalyticsImportPayload) => unwrap<SeednoteImportSummary>(http.post(`/projects/${projectId}/seednote-analytics/imports`, payload)),
  listBatches: (projectId: string, params?: { offset?: number; limit?: number }) => unwrap<{ items: SeednoteImportBatch[]; total: number }>(http.get(`/projects/${projectId}/seednote-analytics/imports`, { params })),
  getBatch: (projectId: string, batchId: string) => unwrap<SeednoteImportSummary>(http.get(`/projects/${projectId}/seednote-analytics/imports/${batchId}`)),
  revoke: (projectId: string, batchId: string) => unwrap<SeednoteImportSummary>(http.post(`/projects/${projectId}/seednote-analytics/imports/${batchId}/revoke`)),
  overview: (projectId: string, params?: { from?: string; to?: string }) => unwrap<SeednoteImportOverview>(http.get(`/projects/${projectId}/seednote-analytics/overview`, { params })),
  posts: (projectId: string, search?: string, params?: { offset?: number; limit?: number }) => unwrap<{ items: SeednotePostIdentity[]; total: number }>(http.get(`/projects/${projectId}/seednote-analytics/posts`, { params: { search, ...params } })),
  post: (projectId: string, postId: string, params?: { from?: string; to?: string }) => unwrap<{ post: SeednotePostIdentity; versions: SeednoteMetricVersion[] }>(http.get(`/projects/${projectId}/seednote-analytics/posts/${postId}`, { params })),
  file: (projectId: string, batchId: string) => unwrap<{ url: string; expires_at: string }>(http.get(`/projects/${projectId}/seednote-analytics/imports/${batchId}/file`)),
}
