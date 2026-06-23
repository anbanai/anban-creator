export type TemplateType = 'poster' | 'seednote' | 'article'
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
  // writing_style is the content/tonality scaffold (separate from the visual
  // style_prompt). Surfaced to the agent via get_channel_profile(task_id) as
  // template_writing_style. Old rows omit it.
  writing_style?: string
  // theme is the 排版样式 dimension (Markdown→HTML layout theme). Surfaced to
  // the agent as template_theme. Old rows omit it.
  theme?: string
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
  // Content scaffold (optional). The form wraps structure/example_content as
  // { text: <markdown> } on submit; backend extracts .text when delivering to
  // the agent. Category/tags are passed through verbatim.
  writing_style?: string
  theme?: string
  structure?: string
  example_content?: string
  category?: string
  tags?: string[]
}

export type UpdateTemplateRequest = CreateTemplateRequest
