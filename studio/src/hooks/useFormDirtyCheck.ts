import { useEffect } from 'react'
import type { FieldValues, UseFormReturn } from 'react-hook-form'

/**
 * Shows a beforeunload warning when the form is dirty and the dialog is open.
 */
export function useFormDirtyCheck<T extends FieldValues>(
  form: UseFormReturn<T>,
  isOpen: boolean,
) {
  useEffect(() => {
    if (!isOpen || !form.formState.isDirty) return

    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault()
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [isOpen, form.formState.isDirty, form])
}
