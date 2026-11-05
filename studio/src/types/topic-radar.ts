export interface TopicSubscription {
  id: string
  project_id: string
  name: string
  enabled: boolean
  timezone: string
  schedule?: { hour: number; minute: number }
  source_config?: { platforms?: string[] }
  weights?: Record<string, number>
  script_policy?: { enabled?: boolean; auto_generate?: boolean; top_n?: number; platform?: string; duration_seconds?: number; aspect_ratio?: string }
  next_run_at?: string
  last_run_at?: string
}

export interface TopicDigest {
  id: string
  subscription_id: string
  status: 'queued' | 'preparing' | 'dispatched' | 'running' | 'finalizing' | 'succeeded' | 'failed' | string
  dispatch_status: string
  candidate_count: number
  error?: string
  planned_at: string
  started_at?: string
  finished_at?: string
  created_at: string
}

export interface TopicSourceRef {
  type: string
  label?: string
  platform?: string
  ref_id?: string
  title?: string
  url?: string
  rank?: number
  hot?: number
  fetched_at?: string
  freshness?: string
}

export interface TopicSuggestion {
  id: string
  digest_id: string
  title: string
  introduction: string
  source_summary: string
  source_refs: TopicSourceRef[]
  scores: Record<string, number>
  video_fit: string
  rank: number
  adoption_status: 'pending' | 'processing' | 'adopted' | 'duplicate' | 'failed' | string
}

export interface TopicScriptJob {
  asset_id?: string
  id: string
  suggestion_id: string
  digest_id: string
  aspect_ratio: string
  task_id?: string
  attempt?: number
  finished_at?: string
  status: string
  target_platform: string
  duration_seconds: number
  error?: string
}

export interface TopicItems<T> { items: T[] }

export type TopicSubscriptionSettings = Pick<TopicSubscription, 'name' | 'enabled' | 'timezone' | 'schedule' | 'source_config' | 'weights' | 'script_policy'>
export interface TopicAdoption { id: string; suggestion_id: string; status: string; error?: string }
export interface TopicNotificationDelivery { id: string; channel_id: string; status: string; attempt: number; last_error?: string; next_attempt_at?: string; sent_at?: string }
export interface TopicDigestDetail { digest: TopicDigest; suggestions: TopicSuggestion[]; adoptions: TopicAdoption[]; scripts: TopicScriptJob[]; notifications: TopicNotificationDelivery[] }
export interface TopicNotificationChannel { id: string; project_id: string; name: string; provider: 'feishu' | 'wecom' | 'dingtalk'; enabled: boolean }
export interface TopicChannelSettings { name: string; provider: TopicNotificationChannel['provider']; enabled: boolean; webhook_url?: string }
export interface TopicScriptArtifact { schema_version: string; title: string; hook: string; scenes: { duration_seconds: number; visual: string; voiceover: string; on_screen_text?: string }[]; cta: string }
