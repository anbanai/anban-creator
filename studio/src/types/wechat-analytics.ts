export type WechatTrackingStatus = 'waiting_data' | 'tracking' | 'stopped' | 'failed'

export interface WechatTrackingInfo {
  status: WechatTrackingStatus | string
  article_url: string
  article_title?: string
  published_date: string
  last_run_at?: string | null
  next_run_at?: string | null
  run_count: number
  stop_reason?: string
  last_error?: string
}

export interface WechatMetricInfo {
  target_user: number
  int_page_read_user: number
  int_page_read_count: number
  ori_page_read_user: number
  ori_page_read_count: number
  share_user: number
  share_count: number
  add_to_fav_user: number
  add_to_fav_count: number
  stat_date: string
  captured_at?: string | null
}

export interface WechatMetricDelta {
  int_page_read_user: number
  int_page_read_count: number
  share_count: number
  add_to_fav_count: number
}

export interface WechatMetricSeriesItem {
  captured_at: string
  stat_date: string
  int_page_read_user: number
  int_page_read_count: number
  ori_page_read_user: number
  ori_page_read_count: number
  share_count: number
  add_to_fav_count: number
}

export interface WechatAnalytics {
  tracking?: WechatTrackingInfo
  latest?: WechatMetricInfo
  deltas?: WechatMetricDelta
  series: WechatMetricSeriesItem[]
}
