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
  // writing_style is the legacy writer-key scaffold (poster). Surfaced to the
  // agent via get_channel_profile(task_id). Old rows omit it.
  writing_style?: string
  // theme is the 排版样式 dimension (Markdown→HTML layout theme). Surfaced to
  // the agent as template_theme. Old rows omit it.
  theme?: string
  // Author persona — the 公众号 写作风格 dimension, defined inline on the
  // template (not a writer key). Surfaced to the agent as template_author_*
  // (name/avatar) and template_writing_style (= author_style_intro). Old rows
  // (and non-article types) omit these.
  author_name?: string
  author_avatar_url?: string
  author_style_intro?: string
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
  // Content scaffold (optional, poster). The form wraps structure/example_content
  // as { text: <markdown> } on submit; backend extracts .text when delivering to
  // the agent. Category/tags are passed through verbatim.
  writing_style?: string
  theme?: string
  structure?: string
  example_content?: string
  category?: string
  tags?: string[]
  // Author persona (公众号). Sent only for article templates.
  author_name?: string
  author_avatar_url?: string
  author_style_intro?: string
}

export type UpdateTemplateRequest = CreateTemplateRequest
