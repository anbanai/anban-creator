import { clsx } from 'clsx'
import type { ReactNode } from 'react'

interface CardProps {
  children: ReactNode
  className?: string
}

export function Card({ children, className }: CardProps) {
  return (
    <div className={clsx('rounded-xl border border-border bg-card', className)}>
      {children}
    </div>
  )
}

export function CardHeader({ children, className }: CardProps) {
  return (
    <div className={clsx('border-b border-border px-4 py-3', className)}>
      {children}
    </div>
  )
}

export function CardBody({ children, className }: CardProps) {
  return <div className={clsx('p-4', className)}>{children}</div>
}

// Alias for backward compatibility
export const CardContent = CardBody

export function CardTitle({ children, className }: CardProps) {
  return <h3 className={clsx('text-sm font-semibold text-card-foreground', className)}>{children}</h3>
}

export function CardDescription({ children, className }: CardProps) {
  return <p className={clsx('text-xs text-muted-foreground', className)}>{children}</p>
}

export function CardFooter({ children, className }: CardProps) {
  return (
    <div className={clsx('border-t border-border px-4 py-3', className)}>
      {children}
    </div>
  )
}
