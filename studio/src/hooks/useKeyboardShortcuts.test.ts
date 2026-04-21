import { describe, it, expect, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

// Mock react-router-dom at the module level
vi.mock('react-router-dom', () => ({
  useNavigate: () => vi.fn(),
}))

import { useKeyboardShortcuts } from './useKeyboardShortcuts'

describe('useKeyboardShortcuts', () => {
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
})
