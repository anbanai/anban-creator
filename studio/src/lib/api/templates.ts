import { http, unwrap } from '@/lib/http-client'
import type { Template } from '@/types'

export interface ListTemplatesParams {
  type?: string
  category?: string
  tag?: string
  offset?: number
  limit?: number
}

export const templatesApi = {
  list: (params?: ListTemplatesParams) =>
    unwrap<{ items: Template[]; total: number }>(http.get('/templates', { params })),

  get: (id: string) =>
    unwrap<Template>(http.get(`/templates/${id}`)),
}
