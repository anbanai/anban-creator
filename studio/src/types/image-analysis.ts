export type ImageAnalysisKind = 'project_visual_style' | 'template_prompt'
export type ImageAnalysisStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'superseded'

export interface ImageAnalysis {
  id: string
  kind: ImageAnalysisKind
  status: ImageAnalysisStatus
  attempt_count: number
  error_code?: string
  error_message?: string
  can_retry: boolean
  updated_at: string
}

export const isImageAnalysisActive = (analysis?: ImageAnalysis | null) =>
  analysis?.status === 'queued' || analysis?.status === 'running'

export const isImageAnalysisUpdateOlder = (candidate: string | undefined, current: string) => {
  if (!current) return false
  if (!candidate) return true
  return Date.parse(candidate) < Date.parse(current)
}
