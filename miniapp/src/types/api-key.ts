export interface APIKey {
  id: string
  user_id: string
  name: string
  key_prefix: string
  last_used_at: string | null
  created_at: string
}

export interface CreateAPIKeyResponse {
  id: string
  name: string
  key_prefix: string
  key: string
  created_at: string
}
