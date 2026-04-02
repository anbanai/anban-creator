import { clsx } from 'clsx'
import type { ReactNode } from 'react'

type BadgeVariant = 'success' | 'danger' | 'warning' | 'neutral' | 'info' | 'outline'

const variantClasses: Record<BadgeVariant, string> = {
  success: 'bg-green-500/15 text-green-400 border-green-500/30',
  danger: 'bg-red-500/15 text-red-400 border-red-500/30',
  warning: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
  neutral: 'bg-gray-500/15 text-gray-400 border-gray-500/30',
  info: 'bg-blue-500/15 text-blue-400 border-blue-500/30',
  outline: 'bg-transparent text-gray-300 border-gray-600',
}

interface BadgeProps {
  variant?: BadgeVariant
  children: ReactNode
  className?: string
}

export default function Badge({ variant = 'neutral', children, className }: BadgeProps) {
  return (
    <span
      className={clsx(
        'inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium',
        variantClasses[variant],
        className,
      )}
    >
      {children}
    </span>
  )
}
