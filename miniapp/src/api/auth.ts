import { get, post, put } from './request'
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

  /** Change existing password */
  changePassword: (oldPassword: string, newPassword: string) =>
    put<null>('/auth/password', { old_password: oldPassword, new_password: newPassword }),

  /** Set initial password for WeChat-created accounts */
  setPassword: (password: string) =>
    post<null>('/auth/set-password', { password }),
}
