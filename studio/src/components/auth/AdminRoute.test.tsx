import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminRoute from './AdminRoute'

const authState = vi.hoisted(() => ({ isAdmin: false }))

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { is_admin: authState.isAdmin } }),
}))

function renderRoute() {
  return render(
    <MemoryRouter initialEntries={['/templates']}>
      <Routes>
        <Route path="/" element={<h1>工作台</h1>} />
        <Route
          path="/templates"
          element={<AdminRoute><h1>模板管理</h1></AdminRoute>}
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('AdminRoute', () => {
  beforeEach(() => {
    authState.isAdmin = false
  })

  it('拒绝普通用户直接访问模板管理路由', async () => {
    renderRoute()
    expect(await screen.findByRole('heading', { name: '工作台' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '模板管理' })).not.toBeInTheDocument()
  })

  it('允许管理员访问模板管理路由', () => {
    authState.isAdmin = true
    renderRoute()
    expect(screen.getByRole('heading', { name: '模板管理' })).toBeInTheDocument()
  })
})
