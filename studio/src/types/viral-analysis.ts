export type ViralAnalysisStatus = 'pending' | 'analyzing' | 'completed' | 'failed'
export type ViralAnalysisSourceType = 'note'
export type AnalysisConfidence = 'high' | 'medium' | 'low'
export type CloneDepth = 'style-only' | 'medium' | 'tight'
export type Transferability = 'high' | 'medium' | 'low'
export type AnalysisDimensionName =
  | 'topic_angle'
  | 'title'
  | 'cover'
  | 'body'
  | 'interaction'
  | 'tags'
  | 'comment_signals'

export interface AnalysisResult {
  summary: string[]
  evidence_table: EvidenceTableItem[]
  dimensions: EvidenceDrivenDimension[]
  clone_suggestions: CloneSuggestions
  risks: string[]
  recommended_clone_depth: CloneDepth
  overall_score: ScoreResult
  viral_template: ViralTemplate
  template_meta: ViralTemplateMeta
}

export interface ScoreResult {
  score: number
  confidence: AnalysisConfidence
  evidence_count: number
  missing_data: string[]
  why_not_higher: string
}

export interface EvidenceTableItem {
  claim: string
  evidence: string
  source: 'title' | 'cover' | 'body' | 'tags' | 'metrics' | 'comments' | string
}

export interface EvidenceDrivenDimension {
  name: AnalysisDimensionName
  observation: string
  mechanism: string
  transferability: Transferability
  action: string
}

export interface CloneSuggestions {
  title: string[]
  body: string[]
  cover: string[]
  tags: string[]
  interaction: string[]
}

export interface ViralTemplate {
  title_template: string
  cover_template: string
  body_template: string
  interaction_template: string
  tag_template: string
  audience_insight: string
  viral_mechanism: string
  rewrite_constraints: string[]
  do_not_copy: string[]
  recommended_clone_depth: CloneDepth
  confidence: AnalysisConfidence
}

export interface ViralTemplateMeta {
  type: 'seednote' | string
  name: string
  category: string
  source_feed_id: string
  source_url: string
  tags: string[]
  template_hash: string
  save_eligible: boolean
}

export interface ViralAnalysis {
  id: string
  user_id: string
  source_type: ViralAnalysisSourceType
  source_url: string
  source_data: Record<string, unknown>
  analysis_result: AnalysisResult | null
  status: ViralAnalysisStatus
  error_message?: string
  created_at: string
  updated_at: string
}

export interface CreateViralAnalysisRequest {
  source_type: ViralAnalysisSourceType
  source_url: string
}
