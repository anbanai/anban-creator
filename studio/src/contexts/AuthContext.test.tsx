import { render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminRoute from '@/components/auth/AdminRoute'
import { api } from '@/lib/api'
import { createTestQueryClient } from '@/test/test-utils'
import type { User } from '@/types'
import { AuthProvider } from './AuthContext'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      auth: { ...actual.api.auth, me: vi.fn() },
    },
  }
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}

describe('AuthProvider legacy user cache', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('refreshes a cached user missing is_admin without redirecting the admin route', async () => {
    const currentUser = deferred<User>()
    vi.mocked(api.auth.me).mockReturnValue(currentUser.promise)
    localStorage.setItem('anban_creator_token', 'legacy-token')
    localStorage.setItem('anban_creator_refresh_token', 'legacy-refresh')
    localStorage.setItem('anban_creator_user', JSON.stringify({
      id: 'admin-1',
      email: 'admin@example.com',
      nickname: 'Admin',
    }))

    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/templates']}>
            <Routes>
              <Route path="/" element={<h1>工作台</h1>} />
              <Route path="/templates" element={<AdminRoute><h1>模板管理</h1></AdminRoute>} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await waitFor(() => expect(api.auth.me).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('heading', { name: '工作台' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '模板管理' })).not.toBeInTheDocument()

    currentUser.resolve({
      id: 'admin-1', email: 'admin@example.com', phone: '', nickname: 'Admin', avatar: '',
      tier: 'pro', max_concurrent_limit: 3, invite_code: '', invite_count: 0, max_invites: 0,
      has_password: true, is_admin: true,
      created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-01T00:00:00Z',
    })

    expect(await screen.findByRole('heading', { name: '模板管理' })).toBeInTheDocument()
    expect(JSON.parse(localStorage.getItem('anban_creator_user') ?? '{}')).toMatchObject({ is_admin: true })
  })

  it('refreshes database admin status even when the cached user already has a boolean value', async () => {
    vi.mocked(api.auth.me).mockResolvedValue({
      id: 'admin-2', email: 'admin2@example.com', phone: '', nickname: 'Admin 2', avatar: '',
      tier: 'pro', max_concurrent_limit: 3, invite_code: '', invite_count: 0, max_invites: 0,
      has_password: true, is_admin: true,
      created_at: '2026-08-02T00:00:00Z', updated_at: '2026-08-02T00:00:00Z',
    })
    localStorage.setItem('anban_creator_token', 'cached-token')
    localStorage.setItem('anban_creator_refresh_token', 'cached-refresh')
    localStorage.setItem('anban_creator_user', JSON.stringify({
      id: 'admin-2',
      email: 'admin2@example.com',
      nickname: 'Admin 2',
      is_admin: false,
    }))

    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <AuthProvider>
          <div>认证完成</div>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await waitFor(() => expect(api.auth.me).toHaveBeenCalledTimes(1))
    await waitFor(() => {
      expect(JSON.parse(localStorage.getItem('anban_creator_user') ?? '{}')).toMatchObject({ is_admin: true })
    })
  })
})
