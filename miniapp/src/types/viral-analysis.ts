export type ViralAnalysisStatus = 'pending' | 'analyzing' | 'completed' | 'failed'
export type ViralAnalysisSourceType = 'note' | 'profile'

export interface TitleAnalysis {
  score: number
  technique: string
  breakdown: string
  rewrite_suggestions: string[]
}

export interface CoverAnalysis {
  score: number
  style: string
  breakdown: string
  key_elements: string[]
}

export interface CopywritingAnalysis {
  score: number
  structure: string
  breakdown: string
  formulas_used: string[]
  word_count: number
}

export interface TagAnalysis {
  score: number
  tags: string[]
  breakdown: string
  suggested_tags: string[]
}

export interface InteractionAnalysis {
  score: number
  techniques: string[]
  breakdown: string
  cta_type: string
}

export interface ViralFactors {
  topic: number
  title: number
  content: number
  visual: number
  interaction: number
}

export interface AnalysisResult {
  title_analysis: TitleAnalysis
  cover_analysis: CoverAnalysis
  copywriting_analysis: CopywritingAnalysis
  tag_analysis: TagAnalysis
  interaction_analysis: InteractionAnalysis
  overall_score: number
  viral_factors: ViralFactors
  suggestions: string[]
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
