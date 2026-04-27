import { http, unwrap } from '@/lib/http-client'
import type { AuthResponse, User } from '@/types'

export const authApi = {
  register: (email: string, password: string, code: string, inviteCode: string, nickname?: string) =>
    unwrap<AuthResponse>(http.post('/auth/register', { email, password, code, invite_code: inviteCode, nickname })),

  sendVerificationCode: (email: string) =>
    unwrap<{ msg: string }>(http.post('/auth/send-code', { email })),

  login: (email: string, password: string) =>
    unwrap<AuthResponse>(http.post('/auth/login', { email, password })),

  refresh: (refreshToken: string) =>
    unwrap<AuthResponse>(http.post('/auth/refresh', { refresh_token: refreshToken })),

  logout: () =>
    unwrap<void>(http.post('/auth/logout')),

  me: () =>
    unwrap<User>(http.get('/auth/me')),

  wxLogin: (code: string, nickname?: string, avatar?: string) =>
    unwrap<AuthResponse>(http.post('/auth/wx-login', { code, nickname, avatar })),

  changePassword: (oldPassword: string, newPassword: string) =>
    unwrap<null>(http.put('/auth/password', { old_password: oldPassword, new_password: newPassword })),
}
