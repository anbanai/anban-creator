import { beforeEach, describe, expect, it, vi } from 'vitest'

import { http } from '@/lib/http-client'
import { seednoteAdminApi } from './seednote-admin'

vi.mock('@/lib/http-client', () => ({
  http: {
    get: vi.fn(),
    delete: vi.fn(),
  },
  unwrap: async <T>(request: Promise<{ data: { data: T } }>) => (await request).data.data,
}))

describe('seednoteAdminApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses the administrator-only login endpoints', async () => {
    vi.mocked(http.get)
      .mockResolvedValueOnce({ data: { data: { available: true, logged_in: false, message: '未登录' } } })
      .mockResolvedValueOnce({ data: { data: { qrcode_image: 'cG5n' } } })
    vi.mocked(http.delete).mockResolvedValueOnce({ data: { data: { logged_in: false } } })

    await expect(seednoteAdminApi.loginStatus()).resolves.toMatchObject({ available: true, logged_in: false })
    await expect(seednoteAdminApi.loginQRCode()).resolves.toEqual({ qrcode_image: 'cG5n' })
    await expect(seednoteAdminApi.logout()).resolves.toEqual({ logged_in: false })

    expect(http.get).toHaveBeenNthCalledWith(1, '/admin/seednote/login-status')
    expect(http.get).toHaveBeenNthCalledWith(2, '/admin/seednote/login-qrcode')
    expect(http.delete).toHaveBeenCalledWith('/admin/seednote/login')
  })
})
