export interface AnalyticsTarget { kind: 'task' | 'wechat_publication' | 'seednote_post'; id: string }
export interface AnalyticsCandidate {
  target: AnalyticsTarget
  title: string
  content_type: string
  status: string
  date?: string
  url?: string
}
export interface AnalyticsSelection { source_row: number; target: AnalyticsTarget }
export interface AnalyticsPreviewRow {
  source_row: number
  title: string
  content_type: string
  match_status: string
  target?: AnalyticsTarget
  target_title?: string
  parse_error?: string
  published_date?: string
  first_published_at?: string
  read_users?: number
  view_count?: number
}
export interface AnalyticsPreview { file_name: string; total_rows: number; rows: AnalyticsPreviewRow[] }
export interface AnalyticsImportPayload {
  upload_id: string
  selections: AnalyticsSelection[]
  data_as_of_at: string
  timezone: string
  client_file_modified_at?: string
}
