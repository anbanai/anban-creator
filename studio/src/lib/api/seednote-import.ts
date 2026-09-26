import { http, unwrap } from '@/lib/http-client'
import type { AnalyticsImportPayload, AnalyticsPreview } from '@/types/content-analytics'
import type { SeednoteImportBatch, SeednoteImportSummary } from '@/types/seednote-import'

export const seednoteImportApi = {
  preview: (projectId: string, payload: { upload_id: string; timezone?: string }) => unwrap<AnalyticsPreview>(http.post(`/projects/${projectId}/seednote-analytics/imports/preview`, payload)),
  import: (projectId: string, payload: AnalyticsImportPayload) => unwrap<SeednoteImportSummary>(http.post(`/projects/${projectId}/seednote-analytics/imports`, payload)),
  listBatches: (projectId: string, params?: { offset?: number; limit?: number }, signal?: AbortSignal) => unwrap<{ items: SeednoteImportBatch[]; total: number }>(http.get(`/projects/${projectId}/seednote-analytics/imports`, { params, signal })),
  getBatch: (projectId: string, batchId: string) => unwrap<SeednoteImportSummary>(http.get(`/projects/${projectId}/seednote-analytics/imports/${batchId}`)),
  revoke: (projectId: string, batchId: string) => unwrap<SeednoteImportSummary>(http.post(`/projects/${projectId}/seednote-analytics/imports/${batchId}/revoke`)),
  file: (projectId: string, batchId: string) => unwrap<{ url: string; expires_at: string }>(http.get(`/projects/${projectId}/seednote-analytics/imports/${batchId}/file`)),
}
