import { http, unwrap } from '@/lib/http-client'
import type { ResourceEntry, ResourceListResponse } from '@/types/resource'

export const resourcesApi = {
  list: (category: string, platform?: string) =>
    unwrap<ResourceListResponse>(
      http.get(`/resources/${category}`, { params: { platform } }),
    ),
  get: (category: string, name: string) =>
    unwrap<ResourceEntry>(http.get(`/resources/${category}/${name}`)),
}
