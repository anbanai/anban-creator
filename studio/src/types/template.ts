export type TemplateType = 'seednote'
export type TemplateVisibility = 'public' | 'private'
export type TemplateScope = 'all' | 'public' | 'private' | 'inactive'

export const SEEDNOTE_TEMPLATE_CATEGORIES = [
  '好物种草',
  '美妆护肤',
  '健康养生',
  '美食生活',
  '家居家装',
  '知识科普',
] as const

export type SeednoteTemplateCategory = (typeof SEEDNOTE_TEMPLATE_CATEGORIES)[number]

export interface Template {
  id: string
  type: TemplateType
  name: string
  category: string
  thumbnail?: ReferenceAssetView | null
  prompt: string
  prompt_source: 'manual' | 'analysis' | ''
  readiness_status: 'analyzing' | 'ready' | 'failed'
  activate_when_ready: boolean
  image_analysis?: ImageAnalysis | null
  visibility: TemplateVisibility
  sort_order: number
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface CreateTemplateRequest {
  name: string
  type: TemplateType
  category: SeednoteTemplateCategory
  thumbnail_image: ReferenceImageSelection
  prompt?: string
  visibility: TemplateVisibility
  sort_order: number
  is_active: boolean
}

export type UpdateTemplateRequest = Partial<CreateTemplateRequest>
import type { ReferenceAssetView, ReferenceImageSelection } from './asset'
import type { ImageAnalysis } from './image-analysis'
