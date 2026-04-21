import { http, unwrap } from '@/lib/http-client'
import type { APIKey, CreateAPIKeyResponse } from '@/types'

export const apiKeysApi = {
  list: () =>
    unwrap<{ items: APIKey[] }>(http.get('/api-keys')),

  create: (name: string) =>
    unwrap<CreateAPIKeyResponse>(http.post('/api-keys', { name })),

  revoke: (id: string) =>
    unwrap<{ revoked: boolean }>(http.delete(`/api-keys/${id}`)),
}
