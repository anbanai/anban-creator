export interface ResourceEntry {
  name: string
  category: string
  description?: string
  mood?: string
  best_for?: string
  colors?: Record<string, string>
  display_name?: string
  english_name?: string
  category_cn?: string
  layout_category?: string
  serves?: string[]
  when_to_use?: string
  markdown_syntax?: string
  kind?: string
  archetype?: string
  aspect_ratios?: string[]
  default_ratio?: string
  tags?: string[]
}

export interface ResourceListResponse {
  category: string
  items: ResourceEntry[]
}
