export type TemplateType = 'poster' | 'seednote' | 'article'

export interface Template {
  id: string
  type: TemplateType
  name: string
  category: string
  thumbnail_url: string
  structure: Record<string, unknown>
  style_prompt: string
  example_content: Record<string, unknown>
  tags: string[]
  sort_order: number
  is_active: boolean
  created_at: string
  updated_at: string
}
