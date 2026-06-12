import { get } from './request'
import type { Template, PaginatedResponse } from '@/types'

export interface ListTemplatesParams {
  type?: string
  category?: string
  tag?: string
  limit?: number
  offset?: number
}

export const templatesApi = {
  list: (params?: ListTemplatesParams) =>
    get<PaginatedResponse<Template>>('/templates', params as Record<string, any>),

  get: (id: string) =>
    get<Template>(`/templates/${id}`),
}
