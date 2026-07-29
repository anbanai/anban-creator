import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import UserAccountPopover from './UserAccountPopover'
import { api } from '@/lib/api'
import { createTestQueryClient } from '@/test/test-utils'

const logoutMock = vi.hoisted(() => vi.fn())
const setThemeMock = vi.hoisted(() => vi.fn())

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({
    user: {
      id: 'user-1',
      email: 'creator@example.com',
      nickname: '测试用户',
      avatar: '',
      tier: 'pro',
    },
    logout: logoutMock,
  }),
}))

vi.mock('next-themes', () => ({
  useTheme: () => ({ theme: 'light', setTheme: setThemeMock }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      billing: {
        ...actual.api.billing,
        wallet: vi.fn(),
      },
    },
  }
})

function renderPopover() {
  const queryClient = createTestQueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <UserAccountPopover />
      </BrowserRouter>
    </QueryClientProvider>,
  )
}

function openPopover() {
  fireEvent.click(screen.getByRole('button', { name: /测试用户/ }))
}

describe('UserAccountPopover billing summary', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows a wallet skeleton while the balance is loading', async () => {
    vi.mocked(api.billing.wallet).mockImplementation(() => new Promise(() => {}))

    renderPopover()
    openPopover()

    expect(await screen.findByLabelText('正在加载钱包')).toBeInTheDocument()
  })

  it('shows wallet balances, debt guidance, and billing actions', async () => {
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 1000, promotional: 200, debt: 50, balance: 1150 })

    renderPopover()
    openPopover()

    expect(await screen.findByText('1,150')).toBeInTheDocument()
    expect(screen.getByText('1,000')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
    expect(screen.getByText('50')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('当前欠费 50 积分')
    expect(screen.getByRole('menuitem', { name: /账单明细/ })).toHaveAttribute('href', '/billing')
    expect(screen.getByRole('menuitem', { name: /充值/ })).toHaveAttribute('href', '/billing')
  })

  it('retries wallet failures without blocking theme or logout controls', async () => {
    vi.mocked(api.billing.wallet)
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce({ paid: 800, promotional: 100, debt: 0, balance: 900 })

    renderPopover()
    openPopover()

    expect(await screen.findByText('钱包加载失败')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('menuitemradio', { name: '暗色模式' }))
    expect(setThemeMock.mock.calls[0]?.[0]).toBe('dark')

    fireEvent.click(await screen.findByRole('menuitem', { name: '重试钱包' }))
    await waitFor(() => expect(api.billing.wallet).toHaveBeenCalledTimes(2))
    expect(await screen.findByText('900')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('menuitem', { name: '退出登录' }))
    expect(logoutMock).toHaveBeenCalledTimes(1)
  })
})
