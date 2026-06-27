export type TemplateType = 'poster' | 'seednote' | 'article' | 'ecommerce'
export type TemplateVisibility = 'public' | 'private'
export type TemplateScope = 'all' | 'public' | 'mine'

// E-commerce template defaults (server model.EcommerceTemplateDefaults). Carried
// on type="ecommerce" templates and merged into the task at creation. Only the
// visual StylePrompt dimension is used (3-dimension architecture: ecommerce
// uses Style only, not WritingStyle/Theme/author). Product photos are uploaded
// per-task and are never part of the template.
export interface EcommerceTemplateDefaults {
  default_selected_modules?: Record<string, number>
  target_platform?: string
  brand_brief?: string
  image_model_key?: string
}

export interface Template {
  id: string
  type: TemplateType
  name: string
  user_id?: string
  visibility?: string
  category: string
  thumbnail_url: string
  structure: Record<string, unknown>
  style_prompt: string
  // writing_style is the legacy writer-key scaffold (poster). Surfaced to the
  // agent via get_project_profile(task_id). Old rows omit it.
  writing_style?: string
  // theme is the 排版样式 dimension (Markdown→HTML layout theme). Old rows omit.
  theme?: string
  // Author persona — 公众号 写作风格 dimension, defined inline on the template
  // (not a writer key). Old rows (and non-article types) omit these.
  author_name?: string
  author_avatar_url?: string
  author_style_intro?: string
  example_content: Record<string, unknown>
  tags: string[]
  // E-commerce template defaults (type="ecommerce" only). Surfaced to the task
  // creation form as pre-filled module/brand/model defaults.
  ecommerce?: EcommerceTemplateDefaults
  sort_order: number
  is_active: boolean
  created_at: string
  updated_at: string
}

// Create/Update payload — mirrors studio CreateTemplateRequest /
// UpdateTemplateRequest. structure / example_content are passed as raw markdown
// text; the backend wraps them as { text: <markdown> } on storage.
export interface CreateTemplateRequest {
  name: string
  type: TemplateType
  thumbnail_url: string
  style_prompt: string
  visibility: TemplateVisibility
  // Content scaffold (optional, poster).
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
  // E-commerce template defaults. Sent only for ecommerce templates.
  ecommerce?: EcommerceTemplateDefaults
}

export type UpdateTemplateRequest = CreateTemplateRequest
