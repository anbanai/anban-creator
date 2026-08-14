import { http, unwrap } from '@/lib/http-client'

export interface SeednoteLoginStatus {
  available: boolean
  logged_in: boolean
  message: string
}

export const seednoteAdminApi = {
  loginStatus: () =>
    unwrap<SeednoteLoginStatus>(http.get('/admin/seednote/login-status')),

  loginQRCode: () =>
    unwrap<{ qrcode_image: string }>(http.get('/admin/seednote/login-qrcode')),

  logout: () =>
    unwrap<{ logged_in: false }>(http.delete('/admin/seednote/login')),
}
