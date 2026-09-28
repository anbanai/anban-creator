export interface TrendItem {
  title: string
  hot: string
  url: string
  rank: number
}

export interface TrendPlatformResult {
  platform: string
  label: string
  items: TrendItem[]
  fetched_at?: string
  expires_at?: string
  stale: boolean
  source?: string
  last_error?: string
}

export interface TrendQueryResult {
  items: TrendPlatformResult[]
  requested_at: string
  ttl_seconds: number
}
