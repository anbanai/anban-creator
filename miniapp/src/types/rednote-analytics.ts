export type RednoteTrackingStatus = 'waiting_discovery' | 'tracking' | 'stopped' | 'failed'

export interface RednoteTrackingInfo {
  status: RednoteTrackingStatus | string
  note_url?: string
  note_title?: string
  note_cover_url?: string
  discovered_at?: string | null
  last_run_at?: string | null
  next_run_at?: string | null
  run_count: number
  stop_reason?: string
  last_error?: string
}

export interface RednoteMetricInfo {
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
  view_count: number | null
  captured_at?: string | null
}

export interface RednoteMetricDelta {
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
}

export interface RednoteMetricSeriesItem {
  captured_at: string
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
  view_count: number | null
}

export interface RednoteAnalytics {
  tracking?: RednoteTrackingInfo
  latest?: RednoteMetricInfo
  deltas?: RednoteMetricDelta
  series: RednoteMetricSeriesItem[]
}
