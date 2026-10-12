import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'

export interface MetricStripItem {
  label: string
  value: ReactNode
  detail?: ReactNode
  icon?: ReactNode
  to?: string
  ariaLabel?: string
}

export interface MetricStripProps {
  metrics: readonly MetricStripItem[]
  label?: string
  className?: string
  testId?: string
}

export function MetricStrip({ metrics, label = '指标概览', className, testId }: MetricStripProps) {
  return (
    <div
      role="group"
      aria-label={label}
      data-testid={testId}
      className={cn('grid min-w-0 grid-cols-2 divide-x divide-border rounded-lg border border-border bg-muted/20 sm:grid-cols-3', className)}
    >
      {metrics.map((metric) => {
        const content = (
          <dl key={metric.label} className="min-w-0 px-3 py-2.5 first:pl-3 last:pr-3">
            <dt className="flex min-w-0 items-center gap-1 text-[11px] font-medium text-muted-foreground">
              {metric.icon ? <span aria-hidden="true" className="[&>svg]:size-3">{metric.icon}</span> : null}
              <span className="truncate">{metric.label}</span>
            </dt>
            <dd className="mt-0.5 truncate text-sm font-semibold tabular-nums text-foreground">{metric.value}</dd>
            {metric.detail ? <dd className="mt-0.5 truncate text-[11px] text-muted-foreground">{metric.detail}</dd> : null}
          </dl>
        )
        return metric.to ? (
          <Link key={metric.label} to={metric.to} aria-label={metric.ariaLabel}
            className="min-w-0 rounded-md transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset">
            {content}
          </Link>
        ) : content
      })}
    </div>
  )
}
