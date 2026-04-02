import { clsx } from 'clsx'
import type { SelectHTMLAttributes } from 'react'

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
  hint?: string
  error?: string
  options: { value: string; label: string }[]
}

export default function Select({
  label,
  hint,
  error,
  options,
  className,
  id,
  ...props
}: SelectProps) {
  const inputId = id || label?.toLowerCase().replace(/\s+/g, '-')
  return (
    <div>
      {label && (
        <label htmlFor={inputId} className="mb-1.5 block text-sm font-medium text-gray-300">
          {label}
        </label>
      )}
      <select
        id={inputId}
        className={clsx(
          'w-full rounded-lg border bg-gray-700 px-3 py-2 text-sm text-gray-100 transition-colors focus:outline-none focus:ring-1',
          error
            ? 'border-red-500 focus:border-red-500 focus:ring-red-500'
            : 'border-gray-600 focus:border-blue-500 focus:ring-blue-500',
          className,
        )}
        {...props}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      {(hint || error) && (
        <p className={clsx('mt-1 text-xs', error ? 'text-red-400' : 'text-gray-500')}>
          {error || hint}
        </p>
      )}
    </div>
  )
}
