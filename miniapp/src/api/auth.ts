import { get, post } from './index'
import type { AuthResponse, User } from '@/types'

export const authApi = {
  /** WeChat mini-program login */
  wxLogin: (code: string, nickname?: string, avatar?: string) =>
    post<AuthResponse>('/auth/wx-login', { code, nickname, avatar }),

  /** Refresh JWT token */
  refresh: (refreshToken: string) =>
    post<AuthResponse>('/auth/refresh', { refresh_token: refreshToken }),

  /** Get current authenticated user */
  me: () =>
    get<User>('/auth/me'),

  /** Logout */
  logout: () =>
    post<void>('/auth/logout'),
}
