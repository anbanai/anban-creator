import { Suspense } from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ErrorBoundary } from './ErrorBoundary'
import { lazyWithRecovery } from '@/lib/lazy-with-recovery'

describe('ErrorBoundary lazy import recovery', () => {
  it('offers a real document reload when a cached lazy import has failed', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    const Page = lazyWithRecovery(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module: /assets/page-old.js')))
    render(<ErrorBoundary><Suspense fallback="Loading"><Page /></Suspense></ErrorBoundary>)
    expect(await screen.findByRole('button', { name: '刷新页面' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument()
    log.mockRestore()
  })
})
