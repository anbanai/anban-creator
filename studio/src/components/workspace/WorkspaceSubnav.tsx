import type { ReactNode } from 'react'
import { NavLink } from 'react-router-dom'
import { cn } from '@/lib/utils'

export interface WorkspaceSubnavItem {
  label: string
  href: string
  icon?: ReactNode
  end?: boolean
}

export interface WorkspaceSubnavProps {
  items: readonly WorkspaceSubnavItem[]
  label?: string
  className?: string
}

export function WorkspaceSubnav({ items, label = '工作区导航', className }: WorkspaceSubnavProps) {
  return (
    <nav aria-label={label} className={cn('border-b border-border', className)}>
      <ul className="flex min-w-0 gap-1 overflow-x-auto">
        {items.map((item) => (
          <li key={item.href}>
            <NavLink
              to={item.href}
              end={item.end ?? true}
              className={({ isActive }) => cn(
                'relative inline-flex min-h-9 shrink-0 items-center gap-1.5 border-b-2 px-2.5 text-sm font-medium text-muted-foreground outline-none transition-colors focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50',
                isActive
                  ? 'border-primary text-foreground'
                  : 'border-transparent hover:border-border hover:text-foreground',
              )}
            >
              {item.icon ? <span aria-hidden="true" className="[&>svg]:size-3.5">{item.icon}</span> : null}
              <span>{item.label}</span>
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}
