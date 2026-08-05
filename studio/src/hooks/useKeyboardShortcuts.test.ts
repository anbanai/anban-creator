import { beforeEach, describe, it, expect, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

const navigate = vi.hoisted(() => vi.fn())

// Mock react-router-dom at the module level
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}))

import { useKeyboardShortcuts } from './useKeyboardShortcuts'

describe('useKeyboardShortcuts', () => {
  beforeEach(() => navigate.mockClear())

  it('calls onShowHelp when ? is pressed', () => {
    const onShowHelp = vi.fn()
    renderHook(() => useKeyboardShortcuts(onShowHelp))

    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '?' }))
    })

    expect(onShowHelp).toHaveBeenCalled()
  })

  it('sets up and tears down event listener', () => {
    const addSpy = vi.spyOn(window, 'addEventListener')
    const removeSpy = vi.spyOn(window, 'removeEventListener')

    const { unmount } = renderHook(() => useKeyboardShortcuts(vi.fn()))

    expect(addSpy).toHaveBeenCalledWith('keydown', expect.any(Function))

    unmount()

    expect(removeSpy).toHaveBeenCalledWith('keydown', expect.any(Function))

    addSpy.mockRestore()
    removeSpy.mockRestore()
  })

  it('only enables the settings shortcut for administrators', () => {
    const regular = renderHook(() => useKeyboardShortcuts(vi.fn(), { isAdmin: false }))

    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'g' }))
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 's' }))
    })
    expect(navigate).not.toHaveBeenCalled()
    regular.unmount()

    renderHook(() => useKeyboardShortcuts(vi.fn(), { isAdmin: true }))
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'g' }))
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 's' }))
    })
    expect(navigate).toHaveBeenCalledWith('/settings')
  })
})
