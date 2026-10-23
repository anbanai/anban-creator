import { screen } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import AppLayout from './AppLayout'

vi.mock('./Sidebar', () => ({
  default: () => <nav aria-label="测试侧栏" />,
}))

vi.mock('@/components/PageTransition', () => ({
  PageTransition: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

describe('AppLayout', () => {
  it('keeps the workspace sidebar visible on the Assistant route', () => {
    render(
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<div>页面内容</div>} />
        </Route>
      </Routes>,
    )

    expect(screen.getByRole('navigation', { name: '测试侧栏' })).toBeInTheDocument()
  })

  it('keeps the main scroll container constrained to the viewport layout', () => {
    render(
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<div>页面内容</div>} />
        </Route>
      </Routes>,
    )

    const main = screen.getByRole('main')

    expect(main).toHaveClass('flex', 'min-h-0', 'min-w-0', 'flex-1', 'flex-col', 'overflow-auto')
    expect(main.parentElement).toHaveClass('flex', 'h-dvh')
  })

  it('hides feedback on the immersive Assistant Workspace route', () => {
    render(
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<div>页面内容</div>} />
        </Route>
      </Routes>,
    )

    expect(screen.queryByLabelText('反馈')).not.toBeInTheDocument()
  })
})
