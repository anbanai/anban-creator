import { clsx } from 'clsx'
import type { InputHTMLAttributes, TextareaHTMLAttributes } from 'react'

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  hint?: string
  error?: string
}

export function Input({ label, hint, error, className, id, ...props }: InputProps) {
  const inputId = id || label?.toLowerCase().replace(/\s+/g, '-')
  return (
    <div>
      {label && (
        <label htmlFor={inputId} className="mb-1.5 block text-sm font-medium text-gray-300">
          {label}
        </label>
      )}
      <input
        id={inputId}
        className={clsx(
          'w-full rounded-lg border bg-gray-700 px-3 py-2 text-sm text-gray-100 placeholder-gray-400 transition-colors focus:outline-none focus:ring-1',
          error
            ? 'border-red-500 focus:border-red-500 focus:ring-red-500'
            : 'border-gray-600 focus:border-blue-500 focus:ring-blue-500',
          className,
        )}
        {...props}
      />
      {(hint || error) && (
        <p className={clsx('mt-1 text-xs', error ? 'text-red-400' : 'text-gray-500')}>
          {error || hint}
        </p>
      )}
    </div>
  )
}

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string
  hint?: string
  error?: string
}

export function Textarea({ label, hint, error, className, id, ...props }: TextareaProps) {
  const inputId = id || label?.toLowerCase().replace(/\s+/g, '-')
  return (
    <div>
      {label && (
        <label htmlFor={inputId} className="mb-1.5 block text-sm font-medium text-gray-300">
          {label}
        </label>
      )}
      <textarea
        id={inputId}
        className={clsx(
          'w-full rounded-lg border bg-gray-700 px-3 py-2 text-sm text-gray-100 placeholder-gray-400 transition-colors focus:outline-none focus:ring-1',
          error
            ? 'border-red-500 focus:border-red-500 focus:ring-red-500'
            : 'border-gray-600 focus:border-blue-500 focus:ring-blue-500',
          className,
        )}
        rows={3}
        {...props}
      />
      {(hint || error) && (
        <p className={clsx('mt-1 text-xs', error ? 'text-red-400' : 'text-gray-500')}>
          {error || hint}
        </p>
      )}
    </div>
  )
}
