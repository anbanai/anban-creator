export type SeednoteTrackingStatus = 'waiting_discovery' | 'tracking' | 'stopped' | 'failed'

export interface SeednoteTrackingInfo {
  status: SeednoteTrackingStatus | string
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

export interface SeednoteMetricInfo {
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
  view_count: number | null
  captured_at?: string | null
}

export interface SeednoteMetricDelta {
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
}

export interface SeednoteMetricSeriesItem {
  captured_at: string
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
  view_count: number | null
}

export interface SeednoteAnalytics {
  tracking?: SeednoteTrackingInfo
  latest?: SeednoteMetricInfo
  deltas?: SeednoteMetricDelta
  series: SeednoteMetricSeriesItem[]
}
