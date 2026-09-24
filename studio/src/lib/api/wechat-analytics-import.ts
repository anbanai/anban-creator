import { http, unwrap } from '@/lib/http-client'
import type { AnalyticsImportPayload } from '@/types/content-analytics'
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
  import: (projectId: string, payload: AnalyticsImportPayload) =>
    unwrap<WechatAnalyticsImportReceipt>(http.post(`/projects/${projectId}/wechat-analytics/imports`, payload)),
  listBatches: (projectId: string, params?: { offset?: number; limit?: number }) =>
    unwrap<{ items: WechatAnalyticsImportBatch[]; total: number }>(http.get(`/projects/${projectId}/wechat-analytics/imports`, { params })),
  getBatch: (projectId: string, batchId: string) =>
    unwrap<WechatAnalyticsImportSummary>(http.get(`/projects/${projectId}/wechat-analytics/imports/${batchId}`)),
  revoke: (projectId: string, batchId: string) =>
    unwrap<WechatAnalyticsImportSummary>(http.post(`/projects/${projectId}/wechat-analytics/imports/${batchId}/revoke`)),
  overview: (projectId: string) =>
    unwrap<WechatAnalyticsOverview>(http.get(`/projects/${projectId}/wechat-analytics/overview`)),
  articles: (projectId: string) =>
    unwrap<{ items: WechatAnalyticsArticleView[] }>(http.get(`/projects/${projectId}/wechat-analytics/articles`)),
  article: (projectId: string, articleId: string) =>
    unwrap<{ items: WechatAnalyticsSnapshot[] }>(http.get(`/projects/${projectId}/wechat-analytics/articles/${articleId}`)),
}
