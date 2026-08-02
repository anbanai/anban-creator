export interface User {
  id: string
  email: string
  phone: string
  nickname: string
  avatar: string
  tier: string
  max_concurrent_limit: number
  invite_code: string
  invite_count: number
  max_invites: number
  has_password: boolean
  is_admin: boolean
  created_at: string
  updated_at: string
}

export interface AuthResponse {
  token: string
  refresh_token: string
  expires_at: number
  user: User
  has_password: boolean
  max_invites: number
}

export interface ApiResponse<T = unknown> {
  code: number
  msg: string
  data: T
}
