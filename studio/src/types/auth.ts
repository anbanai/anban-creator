export interface User {
  id: string
  email: string
  phone: string
  nickname: string
  avatar: string
  credits_balance: number
  tier: string
  max_concurrent_limit: number
  created_at: string
  updated_at: string
}

export interface AuthResponse {
  token: string
  refresh_token: string
  expires_at: number
  user: User
}

export interface ApiResponse<T = unknown> {
  code: number
  msg: string
  data: T
}
