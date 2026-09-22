import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import SeednoteAdminPage from './SeednoteAdminPage'

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      seednoteAdmin: {
        loginStatus: vi.fn(),
        loginQRCode: vi.fn(),
        logout: vi.fn(),
      },
    },
  }
})

describe('SeednoteAdminPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.seednoteAdmin.loginStatus).mockResolvedValue({
      available: true,
      logged_in: false,
      message: '未登录',
    })
    vi.mocked(api.seednoteAdmin.loginQRCode).mockResolvedValue({ qrcode_image: 'cG5n' })
    vi.mocked(api.seednoteAdmin.logout).mockResolvedValue({ logged_in: false })
  })

  it('loads the status and displays an administrator-fetched QR code', async () => {
    render(<SeednoteAdminPage />)

    expect((await screen.findAllByText('未登录')).length).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole('button', { name: '获取登录二维码' }))

    const image = await screen.findByRole('img', { name: '种草笔记登录二维码' })
    expect(image).toHaveAttribute('src', 'data:image/png;base64,cG5n')
    expect(api.seednoteAdmin.loginQRCode).toHaveBeenCalledTimes(1)
  })

  it('confirms logout and refreshes account state', async () => {
    vi.mocked(api.seednoteAdmin.loginStatus).mockResolvedValue({
      available: true,
      logged_in: true,
      message: '已登录',
    })
    render(<SeednoteAdminPage />)

    fireEvent.click(await screen.findByRole('button', { name: '退出登录' }))
    fireEvent.click(screen.getByRole('button', { name: '退出登录' }))

    await waitFor(() => expect(api.seednoteAdmin.logout).toHaveBeenCalledTimes(1))
  })
})
