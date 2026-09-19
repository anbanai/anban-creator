import { http, unwrap } from '@/lib/http-client'
import type { Template, TemplateScope, CreateTemplateRequest, UpdateTemplateRequest } from '@/types'

export interface ListTemplatesParams {
  type?: string
  category?: string
  scope?: TemplateScope
  offset?: number
  limit?: number
}

export const templatesApi = {
  list: (params?: ListTemplatesParams) =>
    unwrap<{ items: Template[]; total: number }>(http.get('/templates', { params })),

  get: (id: string, signal?: AbortSignal) =>
    unwrap<Template>(http.get(`/templates/${id}`, { signal })),

  create: (data: CreateTemplateRequest) =>
    unwrap<Template>(http.post('/templates', data)),

  update: (id: string, data: UpdateTemplateRequest) =>
    unwrap<Template>(http.put(`/templates/${id}`, data)),

  remove: (id: string) =>
    unwrap<{ deleted: string }>(http.delete(`/templates/${id}`)),
}
