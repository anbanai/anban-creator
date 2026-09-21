import { http, unwrap } from '@/lib/http-client'
import type {
  WechatAnalyticsArticleView,
  WechatAnalyticsImportBatch,
  WechatAnalyticsImportPreview,
  WechatAnalyticsImportReceipt,
  WechatAnalyticsImportSummary,
  WechatAnalyticsOverview,
  WechatAnalyticsSnapshot,
} from '@/types/wechat-analytics-import'

export const wechatAnalyticsImportApi = {
  preview: (projectId: string, payload: { upload_id: string; timezone?: string }) =>
    unwrap<WechatAnalyticsImportPreview>(http.post(`/projects/${projectId}/wechat-analytics/imports/preview`, payload)),
  import: (projectId: string, payload: { upload_id: string; data_as_of_at?: string; client_file_modified_at?: string; timezone?: string }) =>
    unwrap<WechatAnalyticsImportReceipt>(http.post(`/projects/${projectId}/wechat-analytics/imports`, payload)),
  listBatches: (projectId: string) =>
    unwrap<{ items: WechatAnalyticsImportBatch[]; total: number }>(http.get(`/projects/${projectId}/wechat-analytics/imports`)),
  getBatch: (projectId: string, batchId: string) =>
    unwrap<WechatAnalyticsImportSummary>(http.get(`/projects/${projectId}/wechat-analytics/imports/${batchId}`)),
  revoke: (projectId: string, batchId: string) =>
    unwrap<WechatAnalyticsImportSummary>(http.post(`/projects/${projectId}/wechat-analytics/imports/${batchId}/revoke`)),
  resolve: (projectId: string, batchId: string, actions: Array<{ row_id: string; action: string; publication_id?: string }>) =>
    unwrap<WechatAnalyticsImportSummary>(http.post(`/projects/${projectId}/wechat-analytics/imports/${batchId}/resolve`, { actions })),
  overview: (projectId: string) =>
    unwrap<WechatAnalyticsOverview>(http.get(`/projects/${projectId}/wechat-analytics/overview`)),
  articles: (projectId: string) =>
    unwrap<{ items: WechatAnalyticsArticleView[] }>(http.get(`/projects/${projectId}/wechat-analytics/articles`)),
  article: (projectId: string, articleId: string) =>
    unwrap<{ items: WechatAnalyticsSnapshot[] }>(http.get(`/projects/${projectId}/wechat-analytics/articles/${articleId}`)),
}
