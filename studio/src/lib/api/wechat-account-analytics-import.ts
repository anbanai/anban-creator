import { http, unwrap } from '@/lib/http-client'

export interface WechatAccountAnalyticsRow {
  source_row: number
  stat_date: string
  read_users: number | null
  share_users: number | null
  original_read_users: number | null
  like_users: number | null
  collection_users: number | null
  comment_count: number | null
  follower_gain_count: number | null
  published_articles: number | null
  raw?: Record<string, string>
  parse_error?: string
}

export interface WechatTrafficSourceRow {
  source_row: number
  stat_date: string
  source: string
  original_label: string
  read_users: number | null
  raw?: Record<string, string>
  parse_error?: string
}

export interface WechatAccountAnalyticsPreview {
  source: string
  report_type: string
  supported_fields: string[]
  date_from?: string
  date_to?: string
  file_name: string
  parser_version: string
  rows: WechatAccountAnalyticsRow[]
  sources: WechatTrafficSourceRow[]
  unknown_headers?: string[]
  duplicate_dates?: string[]
  duplicate_sources?: string[]
  errors?: string[]
}

export interface WechatAccountAnalyticsBatch {
  id: string
  user_id?: string
  project_id: string
  asset_id?: string
  file_name: string
  sha256?: string
  source?: string
  timezone?: string
  parser_version?: string
  status: string
  data_as_of_at?: string
  received_at?: string
  effective_at?: string
  revoked_at?: string | null
  created_at?: string
}

export interface WechatAccountAnalyticsImportSummary {
  revision: number
  batch: WechatAccountAnalyticsBatch
}

const path = (projectId: string) => `/projects/${encodeURIComponent(projectId)}/wechat-analytics/account-imports`

export const wechatAccountAnalyticsImportApi = {
  preview: (projectId: string, payload: { upload_id: string; timezone?: string }, signal?: AbortSignal) =>
    unwrap<WechatAccountAnalyticsPreview>(http.post(`${path(projectId)}/preview`, payload, { signal })),
  import: (projectId: string, payload: { upload_id: string; idempotency_key: string; data_as_of_at: string; timezone?: string; client_file_modified_at?: string }, signal?: AbortSignal) =>
    unwrap<WechatAccountAnalyticsImportSummary>(http.post(path(projectId), payload, { signal })),
  listBatches: (projectId: string, params?: { offset?: number; limit?: number }, signal?: AbortSignal) =>
    unwrap<{ items: WechatAccountAnalyticsBatch[]; total: number }>(http.get(path(projectId), { params, signal })),
  getBatch: (projectId: string, batchId: string, signal?: AbortSignal) =>
    unwrap<WechatAccountAnalyticsImportSummary>(http.get(`${path(projectId)}/${encodeURIComponent(batchId)}`, { signal })),
  revoke: (projectId: string, batchId: string, signal?: AbortSignal) =>
    unwrap<WechatAccountAnalyticsImportSummary>(http.post(`${path(projectId)}/${encodeURIComponent(batchId)}/revoke`, undefined, { signal })),
}
