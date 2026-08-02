import { get } from './request'
import type {
  Template,
  TemplateType,
  SeednoteTemplateCategory,
  PaginatedResponse,
} from '@/types'

export interface ListTemplatesParams {
  type?: TemplateType
  category?: SeednoteTemplateCategory
  limit?: number
  offset?: number
}

export const templatesApi = {
  list: (params?: ListTemplatesParams) =>
    get<PaginatedResponse<Template>>('/templates', params as Record<string, any>),
}
