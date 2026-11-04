import { act, render } from '@testing-library/react'
import { expect, it } from 'vitest'
import { useMobileViewport } from './use-mobile-viewport'

it('reflows for the phone keyboard and releases viewport listeners and styles on unmount', () => {
  const viewport = Object.assign(new EventTarget(), { height: 430, offsetTop: 3 })
  const original = window.visualViewport
  Object.defineProperty(window, 'visualViewport', { configurable: true, value: viewport })
  const originalMedia = window.matchMedia
  window.matchMedia = query => ({ matches: true, media: query }) as MediaQueryList
  function Page() { useMobileViewport(); return <textarea aria-label="输入" /> }
  const view = render(<Page />)
  try {
    act(() => { view.getByRole('textbox').focus(); viewport.dispatchEvent(new Event('resize')) })
    expect(document.documentElement.style.getPropertyValue('--studio-visible-height')).toBe('430px')
    expect(document.documentElement.hasAttribute('data-mobile-keyboard')).toBe(true)
    act(() => { viewport.height = window.innerHeight; viewport.dispatchEvent(new Event('resize')) })
    expect(document.documentElement.hasAttribute('data-mobile-keyboard')).toBe(false)
    view.unmount()
    expect(document.documentElement.style.getPropertyValue('--studio-visible-height')).toBe('')
  } finally {
    window.matchMedia = originalMedia
    Object.defineProperty(window, 'visualViewport', { configurable: true, value: original })
  }
})
