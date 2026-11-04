import { useTheme } from 'next-themes'
import { useEffect, useRef } from 'react'

let activeUsers = 0
let originalTheme = 'system'
let restoreTheme: ((theme: string) => void) | null = null

/** Keeps public entry pages on the system theme without clobbering the app preference. */
export function useSystemTheme(enabled = true) {
  const { theme, setTheme } = useTheme()
  const acquired = useRef(false)
  const initialTheme = useRef(theme || 'system')

  useEffect(() => {
    if (!enabled || acquired.current) return

    if (activeUsers === 0) {
      originalTheme = initialTheme.current
      restoreTheme = setTheme
    }
    activeUsers += 1
    acquired.current = true
    setTheme('system')

    return () => {
      if (!acquired.current) return
      acquired.current = false
      activeUsers = Math.max(0, activeUsers - 1)
      if (activeUsers === 0) {
        restoreTheme?.(originalTheme)
        restoreTheme = null
      }
    }
  }, [enabled, setTheme])
}
