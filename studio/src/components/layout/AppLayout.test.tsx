import { screen, within } from '@testing-library/react'
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
  it('keeps feedback in mobile document flow and fixes it only at md+', () => {
    render(
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<div>页面内容</div>} />
        </Route>
      </Routes>,
    )

    const main = screen.getByRole('main')
    const feedback = within(main).getByLabelText('反馈')

    expect(main.lastElementChild).toBe(feedback)
    expect(feedback).toHaveClass(
      'relative',
      'mt-4',
      'flex',
      'justify-end',
      'md:fixed',
      'md:bottom-20',
      'md:right-6',
      'md:mt-0',
    )
    expect(feedback).not.toHaveClass('fixed', 'bottom-20', 'right-6')
  })
})
