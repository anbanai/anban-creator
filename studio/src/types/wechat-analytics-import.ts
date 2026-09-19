import type { WechatPublication } from './wechat-publication'

export type WechatImportRowStatus = 'matched' | 'needs_review' | 'unmatched' | 'invalid' | string

export interface WechatAnalyticsImportBatch {
  id: string
  file_name: string
  source?: string
  data_as_of_at: string
  parser_version: string
  status: string
  total_rows: number
  matched_rows: number
  review_rows: number
  unmatched_rows: number
  invalid_rows: number
}

export interface WechatAnalyticsImportRow {
  id: string
  source_row: number
  title: string
  published_date?: string
  article_url?: string
  read_users?: number
  share_users?: number
  read_to_follow_users?: number
  delivered_users?: number
  delivery_completion_rate?: number
  read_completion_rate?: number
  parse_error?: string
  match_status: WechatImportRowStatus
  publication_id?: string
}

export interface WechatAnalyticsImportSummary {
  batch: WechatAnalyticsImportBatch
  rows: WechatAnalyticsImportRow[]
}

export interface WechatAnalyticsImportReceipt {
  batch_id: string
  source_file: string
  total_rows: number
  matched_rows: number
  ambiguous_rows: number
  unmatched_rows: number
  invalid_rows: number
  parser_version: string
}

export interface WechatAnalyticsFieldMapping {
  excel_field: string
  internal_field: string
}

export interface WechatAnalyticsImportPreview {
  source: string
  file_name: string
  total_rows: number
  parser_version: string
  field_mapping: WechatAnalyticsFieldMapping[]
  rows: Array<{
    source_row: number
    source: string
    title: string
    published_date?: string
    article_url?: string
    read_users?: number
    share_users?: number
    read_to_follow_users?: number
    delivered_users?: number
    delivery_completion_rate?: number
    read_completion_rate?: number
    parse_error?: string
  }>
}

export interface WechatAnalyticsArticleView {
  publication: WechatPublication
  latest?: WechatAnalyticsSnapshot
  snapshots?: WechatAnalyticsSnapshot[]
}

export interface WechatAnalyticsSnapshot {
  id: string
  publication_id: string
  data_as_of_at: string
  imported_at: string
  source?: string
  read_users?: number
  share_users?: number
  read_to_follow_users?: number
  delivered_users?: number
  delivery_completion_rate?: number
  read_completion_rate?: number
}

export interface WechatAnalyticsOverview {
  articles: number
  published: number
  with_data: number
  read_users: number
  share_users: number
  read_to_follow_users: number
  delivered_users: number
}
