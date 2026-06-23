import { http, unwrap } from '@/lib/http-client'
import type { ResourceEntry, ResourceListResponse } from '@/types/resource'

export const resourcesApi = {
  list: (category: string, platform?: string) =>
    unwrap<ResourceListResponse>(
      http.get(`/resources/${category}`, { params: { platform } }),
    ),
  get: (category: string, name: string) =>
    unwrap<ResourceEntry>(http.get(`/resources/${category}/${name}`)),
  // previewTheme fetches the rendered WeChat HTML for a theme (built-in sample
  // markdown, deterministic — no LLM). Returns raw HTML text (NOT JSON), so it
  // bypasses unwrap and the default JSON transformResponse.
  previewTheme: (name: string) =>
    http
      .get(`/resources/themes/${encodeURIComponent(name)}/preview`, {
        responseType: 'text',
        transformResponse: [(data) => data],
      })
      .then((res) => res.data as string),
}
