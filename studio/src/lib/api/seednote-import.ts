import { http, unwrap } from '@/lib/http-client'
import type { SeednoteImportBatch, SeednoteImportOverview, SeednoteImportSummary, SeednoteMetricVersion, SeednotePostIdentity } from '@/types/seednote-import'

export const seednoteImportApi = {
  import: (projectId: string, payload: { upload_id: string; data_as_of_at?: string; timezone?: string; client_file_modified_at?: string }) => unwrap<SeednoteImportSummary>(http.post(`/projects/${projectId}/seednote-analytics/imports`, payload)),
  listBatches: (projectId: string) => unwrap<{ items: SeednoteImportBatch[]; total: number }>(http.get(`/projects/${projectId}/seednote-analytics/imports`)),
  getBatch: (projectId: string, batchId: string) => unwrap<SeednoteImportSummary>(http.get(`/projects/${projectId}/seednote-analytics/imports/${batchId}`)),
  resolve: (projectId: string, batchId: string, actions: Array<{ row_id: string; action: string; post_id?: string }>) => unwrap<SeednoteImportSummary>(http.post(`/projects/${projectId}/seednote-analytics/imports/${batchId}/resolve`, { actions })),
  overview: (projectId: string, params?: { from?: string; to?: string }) => unwrap<SeednoteImportOverview>(http.get(`/projects/${projectId}/seednote-analytics/overview`, { params })),
  posts: (projectId: string, search?: string, params?: { offset?: number; limit?: number }) => unwrap<{ items: SeednotePostIdentity[]; total: number }>(http.get(`/projects/${projectId}/seednote-analytics/posts`, { params: { search, ...params } })),
  post: (projectId: string, postId: string, params?: { from?: string; to?: string }) => unwrap<{ post: SeednotePostIdentity; versions: SeednoteMetricVersion[] }>(http.get(`/projects/${projectId}/seednote-analytics/posts/${postId}`, { params })),
  file: (projectId: string, batchId: string) => unwrap<{ url: string; expires_at: string }>(http.get(`/projects/${projectId}/seednote-analytics/imports/${batchId}/file`)),
}
