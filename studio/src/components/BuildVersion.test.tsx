import { render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BuildVersion } from './BuildVersion'
afterEach(() => vi.unstubAllGlobals())
it('shows seven characters with the full revision available on hover', () => {
  const sha = '1234567890abcdef1234567890abcdef12345678'
  vi.stubGlobal('__APP_COMMIT_SHA__', sha)
  render(<BuildVersion />)
  expect(screen.getByText('1234567')).toBeInTheDocument()
  expect(screen.getByTitle(`前端完整版本：${sha}`)).toBeInTheDocument()
})
it('does not invent a revision when the build has no metadata', () => {
  vi.stubGlobal('__APP_COMMIT_SHA__', 'unknown')
  render(<BuildVersion />)
  expect(screen.getByText('开发版')).toBeInTheDocument()
})
