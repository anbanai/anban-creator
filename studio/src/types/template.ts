export type TemplateType = 'poster' | 'seednote' | 'article' | 'ecommerce'
export type TemplateVisibility = 'public' | 'private'
export type TemplateScope = 'all' | 'public' | 'mine'

export interface Template {
  id: string
  type: TemplateType
  name: string
  user_id?: string
  visibility: TemplateVisibility
  category: string
  thumbnail_url: string
  structure: Record<string, unknown>
  style_prompt: string
  // Legacy columns kept on older rows. New templates are visual-only and Studio
  // does not send or render these fields.
  writer?: string
  theme?: string
  author?: string
  example_content: Record<string, unknown>
  tags: string[]
  sort_order: number
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface CreateTemplateRequest {
  name: string
  type: TemplateType
  thumbnail_url: string
  style_prompt: string
  visibility: TemplateVisibility
  category?: string
  tags?: string[]
}

export type UpdateTemplateRequest = CreateTemplateRequest
