import { useEffect } from 'react'

/** Keep the composer within the visible viewport when a phone keyboard opens. */
export function useMobileViewport() {
  useEffect(() => {
    const viewport = window.visualViewport
    const root = document.documentElement
    const update = () => {
      const mobile = window.matchMedia('(max-width: 767px)').matches
      root.style.setProperty('--studio-visible-height', `${mobile && viewport ? viewport.height : window.innerHeight}px`)
      root.style.setProperty('--studio-visible-top', `${mobile && viewport ? viewport.offsetTop : 0}px`)
      const input = document.activeElement
      const editing = input instanceof HTMLTextAreaElement || input instanceof HTMLInputElement || input instanceof HTMLElement && input.isContentEditable
      const keyboard = mobile && editing && !!viewport && window.innerHeight - viewport.height > 120
      root.toggleAttribute('data-mobile-keyboard', keyboard)
    }
    update()
    viewport?.addEventListener('resize', update)
    viewport?.addEventListener('scroll', update)
    window.addEventListener('resize', update)
    document.addEventListener('focusin', update)
    document.addEventListener('focusout', update)
    return () => {
      viewport?.removeEventListener('resize', update)
      viewport?.removeEventListener('scroll', update)
      window.removeEventListener('resize', update)
      document.removeEventListener('focusin', update)
      document.removeEventListener('focusout', update)
      root.style.removeProperty('--studio-visible-height')
      root.style.removeProperty('--studio-visible-top')
      root.removeAttribute('data-mobile-keyboard')
    }
  }, [])
}
