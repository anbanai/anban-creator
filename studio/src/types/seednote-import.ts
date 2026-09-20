export interface SeednoteImportBatch {
  id: string
  project_id: string
  file_name: string
  file_size: number
  received_at: string
  client_modified_at?: string | null
  source_created_at?: string | null
  source_modified_at?: string | null
  data_as_of_at: string
  timezone: string
  status: string
  total_rows: number
  resolved_rows: number
  review_rows: number
  invalid_rows: number
  error_summary?: string
}
export interface SeednoteImportRow {
  id: string
  source_row: number
  title: string
  genre?: string
  first_published_at?: string | null
  parse_error?: string
  match_status: string
  candidate_post_id?: string
  candidate_confidence?: number
  post_id?: string
  exposure_count?: number | null
  view_count?: number | null
  cover_click_rate?: number | null
  like_count?: number | null
  comment_count?: number | null
  collect_count?: number | null
  follower_gain_count?: number | null
  share_count?: number | null
  avg_watch_duration?: number | null
  barrage_count?: number | null
}
export interface SeednoteImportSummary { batch: SeednoteImportBatch; rows: SeednoteImportRow[] }
export interface SeednoteOverviewPoint { date: string; exposure_count: number; view_count: number; like_count: number; comment_count: number; collect_count: number; follower_gain_count: number; share_count: number; barrage_count: number; cover_click_rate?: number; avg_watch_duration?: number }
export interface SeednotePostIdentity {
  id: string
  title: string
  note_id?: string
  note_url?: string
  first_published_at?: string | null
}
export interface SeednotePostSummary extends SeednotePostIdentity {
  exposure_count?: number | null
  view_count?: number | null
  cover_click_rate?: number | null
  like_count?: number | null
  comment_count?: number | null
  collect_count?: number | null
  follower_gain_count?: number | null
  share_count?: number | null
  avg_watch_duration?: number | null
  barrage_count?: number | null
}
export interface SeednoteImportOverview {
  dates: string[]
  series: SeednoteOverviewPoint[]
  posts: SeednotePostIdentity[]
  post_summaries: SeednotePostSummary[]
}
export interface SeednoteMetricVersion { id: string; data_as_of_at: string; imported_at: string; exposure_count?: number | null; view_count?: number | null; cover_click_rate?: number | null; like_count?: number | null; comment_count?: number | null; collect_count?: number | null; follower_gain_count?: number | null; share_count?: number | null; avg_watch_duration?: number | null; barrage_count?: number | null }
