import { get } from './request'
import type { ResourceEntry, ResourceListResponse } from '@/types'

export const resourcesApi = {
  list: (category: string, platform?: string) =>
    get<ResourceListResponse>(`/resources/${category}`, { platform }),

  get: (category: string, name: string) =>
    get<ResourceEntry>(`/resources/${category}/${name}`),
}
