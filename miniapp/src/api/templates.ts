import { get, post, put, del } from './request'
import type {
  Template,
  TemplateScope,
  CreateTemplateRequest,
  UpdateTemplateRequest,
  PaginatedResponse,
} from '@/types'

export interface ListTemplatesParams {
  type?: string
  category?: string
  tag?: string
  scope?: TemplateScope
  limit?: number
  offset?: number
}

export const templatesApi = {
  list: (params?: ListTemplatesParams) =>
    get<PaginatedResponse<Template>>('/templates', params as Record<string, any>),

  get: (id: string) =>
    get<Template>(`/templates/${id}`),

  create: (data: CreateTemplateRequest) =>
    post<Template>('/templates', data),

  update: (id: string, data: UpdateTemplateRequest) =>
    put<Template>(`/templates/${id}`, data),

  remove: (id: string) =>
    del<{ deleted: string }>(`/templates/${id}`),
}
