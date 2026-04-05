import { clsx } from 'clsx'
import type { ReactNode } from 'react'

interface CardProps {
  children: ReactNode
  className?: string
}

export function Card({ children, className }: CardProps) {
  return (
    <div className={clsx('rounded-xl border border-gray-700 bg-gray-800', className)}>
      {children}
    </div>
  )
}

export function CardHeader({ children, className }: CardProps) {
  return (
    <div className={clsx('border-b border-gray-700 px-4 py-3', className)}>
      {children}
    </div>
  )
}

export function CardBody({ children, className }: CardProps) {
  return <div className={clsx('p-4', className)}>{children}</div>
}

export function CardFooter({ children, className }: CardProps) {
  return (
    <div className={clsx('border-t border-gray-700 px-4 py-3', className)}>
      {children}
    </div>
  )
}
