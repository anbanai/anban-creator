export type ContentAssetStatus = 'candidate' | 'researched' | 'validated' | 'ready' | 'in_progress' | 'exhausted' | 'archived'
export type ContentAssetLifecycle = 'evergreen' | 'seasonal' | 'expiring'

export interface ContentAsset {
  id: string
  user_id: string
  title: string
  core_question?: string
  audience?: string
  semantic_kernel?: string
  lifecycle: ContentAssetLifecycle
  status: ContentAssetStatus
  evidence_quality?: string
  created_at: string
  updated_at: string
}

export interface ContentAssetEvidence {
  id: string
  asset_id: string
  source_type: string
  platform?: string
  source_kind?: string
  source_id?: string
  title?: string
  url?: string
  metrics?: Record<string, unknown>
  data_as_of?: string
  data_source?: string
  freshness?: string
  quality?: string
  notes?: string
  created_at: string
}

export interface ContentAssetLink {
  id: string
  asset_id: string
  project_id?: string
  content_kind: string
  content_id: string
  platform?: string
  relation: string
  angle?: string
  created_at: string
}

export interface ContentAssetScoreSnapshot {
  id: string
  asset_id: string
  project_id: string
  platform: string
  analytics_revision: number
  status: 'scored' | 'exploratory' | 'data_insufficient'
  score: number
  confidence: 'low' | 'medium' | 'high'
  data_as_of?: string
  snapshot: Record<string, unknown>
  created_at: string
}

export interface ContentReuseRun {
  id: string
  user_id: string
  asset_id: string
  source_content_kind?: string
  source_content_id?: string
  target_project_id: string
  target_platform: string
  target_agent_id?: string
  stage: string
  status: string
  idempotency_key: string
  budget_reserved: number
  cost_credits: number
  retry_count: number
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
  stage_executions?: ContentReuseStageExecution[]
  audit_events?: ContentReuseAuditEvent[]
}

export interface ContentReuseStageExecution {
  id: string
  run_id: string
  stage: string
  version: number
  status: string
  task_id?: string
  accepted_execution_id?: string
  artifact_refs?: Array<{ task_id: string; execution_id: string; path: string; content_sha256: string }>
  reserved_credits: number
  settled_credits: number
  retry_count: number
  error_code?: string
  error_message?: string
  recovery_point?: string
  next_run_at?: string
  created_at: string
  updated_at: string
}

export interface ContentReuseAuditEvent {
  id: string
  run_id: string
  stage_execution_id?: string
  entity_kind: string
  entity_id: string
  from_state?: string
  to_state: string
  reason_code?: string
  task_id?: string
  execution_id?: string
  created_at: string
}

export interface ContentAssetDetail {
  asset: ContentAsset
  evidence: ContentAssetEvidence[]
  links: ContentAssetLink[]
  scores: ContentAssetScoreSnapshot[]
}

export interface ContentAssetPlatformContent {
  kind: string
  id: string
  published: boolean
  metrics?: Record<string, unknown>
  data_as_of?: string
}

export interface ContentAssetPlatformPerformance {
  platform: string
  project_id: string
  content_count: number
  published_count: number
  analytics_revision?: number
  data_as_of?: string
  metrics?: Record<string, unknown>
  contents?: ContentAssetPlatformContent[]
  missing?: string[]
}

export interface ContentAssetLineage extends ContentAssetDetail {
  summary: {
    origin_count: number
    derivative_count: number
    run_count: number
    published_count: number
    cost_credits: number
    reserved_credits: number
  }
  platform_performance: ContentAssetPlatformPerformance[]
  latest_runs: ContentReuseRun[]
}

export interface ContentAssetRecommendation {
  asset: ContentAsset
  score?: ContentAssetScoreSnapshot
  run?: ContentReuseRun
  recommended_platform?: string
  recommendation_reason?: string
  estimated_cost_credits: number
  evidence_as_of?: string
  automation_status?: string
  automation_stage?: string
}

export interface ContentAutopilotPolicy {
  user_id: string
  enabled: boolean
  project_ids?: string[]
  platforms?: string[]
  daily_budget_credits: number
  max_run_cost_credits: number
  min_recommendation_score: number
  platform_min_recommendation_scores?: Record<string, number>
  min_video_confidence: number
  video_first?: boolean
  publish_windows?: Array<{ start: string; end: string; timezone: string }>
  draft_enabled: boolean
  publish_enabled: boolean
  compliance_version?: string
  failure_mode: 'pause' | 'skip'
  revision: number
}
