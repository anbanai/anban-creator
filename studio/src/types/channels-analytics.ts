export type ChannelsTrackingStatus = 'tracking' | 'stopped' | 'failed'

export interface ChannelsTrackingInfo {
  status: ChannelsTrackingStatus | string
  video_url: string
  video_title?: string
  author_name?: string
  cover_url?: string
  published_at?: string | null
  last_run_at?: string | null
  next_run_at?: string | null
  run_count: number
  stop_reason?: string
  last_error?: string
  provider_name: string
}

export interface ChannelsMetricInfo {
  like_count: number
  favorite_count: number
  comment_count: number
  forward_count: number
  captured_at?: string | null
}

export interface ChannelsMetricDelta {
  like_count: number
  favorite_count: number
  comment_count: number
  forward_count: number
}

export interface ChannelsMetricSeriesItem {
  captured_at: string
  like_count: number
  favorite_count: number
  comment_count: number
  forward_count: number
}

export interface ChannelsAnalytics {
  tracking?: ChannelsTrackingInfo
  latest?: ChannelsMetricInfo
  deltas?: ChannelsMetricDelta
  series: ChannelsMetricSeriesItem[]
}
