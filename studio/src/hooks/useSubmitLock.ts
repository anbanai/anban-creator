import { useRef, useCallback, useState } from 'react'

/**
 * Synchronous submission lock using a ref.
 * Prevents double-clicks that bypass React's async state batching.
 */
export function useSubmitLock() {
  const lockRef = useRef(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const submit = useCallback(async <T>(fn: () => Promise<T>): Promise<T | undefined> => {
    if (lockRef.current) return
    lockRef.current = true
    setIsSubmitting(true)
    try {
      return await fn()
    } finally {
      lockRef.current = false
      setIsSubmitting(false)
    }
  }, [])
  return { submit, isSubmitting }
}
