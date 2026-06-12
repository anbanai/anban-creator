import { get, post, del } from './request'
import type { APIKey, CreateAPIKeyResponse } from '@/types'

export const apiKeysApi = {
  list: () =>
    get<{ items: APIKey[] }>('/api-keys'),

  create: (name: string) =>
    post<CreateAPIKeyResponse>('/api-keys', { name }),

  revoke: (id: string) =>
    del<{ revoked: boolean }>(`/api-keys/${id}`),
}
