import { renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useSystemTheme } from './useSystemTheme'

const themeState = vi.hoisted(() => ({ theme: 'dark' as string | undefined, setTheme: vi.fn() }))

vi.mock('next-themes', () => ({
  useTheme: () => themeState,
}))

describe('useSystemTheme', () => {
  afterEach(() => {
    themeState.theme = 'dark'
    themeState.setTheme.mockClear()
  })

  it('keeps the original theme until the last public page unmounts', () => {
    const first = renderHook(() => useSystemTheme())
    const second = renderHook(() => useSystemTheme())

    expect(themeState.setTheme).toHaveBeenCalledWith('system')
    expect(themeState.setTheme).toHaveBeenCalledTimes(2)

    first.unmount()
    expect(themeState.setTheme).toHaveBeenCalledTimes(2)

    second.unmount()
    expect(themeState.setTheme).toHaveBeenLastCalledWith('dark')
  })

  it('does not alter the theme when disabled', () => {
    const { unmount } = renderHook(() => useSystemTheme(false))

    expect(themeState.setTheme).not.toHaveBeenCalled()
    unmount()
    expect(themeState.setTheme).not.toHaveBeenCalled()
  })
})
