export type TemplateType = 'poster' | 'seednote' | 'article' | 'ecommerce'
export type TemplateVisibility = 'public' | 'private'
export type TemplateScope = 'all' | 'public' | 'mine'

// E-commerce template defaults (server model.EcommerceTemplateDefaults). Carried
// on type="ecommerce" templates and merged into the task at creation
// (service/task.go CreateManual) when the task omits its own values. Only the
// visual StylePrompt dimension is used (3-dimension architecture: ecommerce
// uses Style only, not WritingStyle/Theme/byline). Product photos are uploaded
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
  visibility: TemplateVisibility
  category: string
  thumbnail_url: string
  structure: Record<string, unknown>
  style_prompt: string
  // writer_key is the legacy writer-key scaffold (poster). Templates are project-
  // launchers (imported once, then detached); the agent receives the project's
  // flat fields via get_project_profile(task_id) — there is no template_* namespace. Old rows omit it.
  writer_key?: string
  // theme is the 排版样式 dimension (Markdown→HTML layout theme). Same as above:
  // pre-fills project.theme; the project surfaces flat — no template_* namespace. Old rows omit it.
  theme?: string
  // Author persona — the 公众号 写作风格 dimension, defined inline on the
  // template (not a writer key). author_name=署名(pre-fills project.byline),
  // writing_voice=自由文本口吻(pre-fills project.writing_voice, ≠ writer_key),
  // persona_avatar=头像. The project surfaces these flat — no template_* namespace.
  // Old rows (and non-article types) omit these.
  author_name?: string
  persona_avatar?: string
  writing_voice?: string
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

export interface CreateTemplateRequest {
  name: string
  type: TemplateType
  thumbnail_url: string
  style_prompt: string
  visibility: TemplateVisibility
  // Content scaffold (optional, poster). The form wraps structure/example_content
  // as { text: <markdown> } on submit; backend extracts .text when delivering to
  // the agent. Category/tags are passed through verbatim.
  writer_key?: string
  theme?: string
  structure?: string
  example_content?: string
  category?: string
  tags?: string[]
  // Author persona (公众号). Sent only for article templates.
  author_name?: string
  persona_avatar?: string
  writing_voice?: string
  // E-commerce template defaults. Sent only for ecommerce templates.
  ecommerce?: EcommerceTemplateDefaults
}

export type UpdateTemplateRequest = CreateTemplateRequest
