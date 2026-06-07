import { TrendingUp, TrendingDown, Minus } from 'lucide-react'

interface StatsCardProps {
  title: string
  value: string | number
  description?: string
  trend?: {
    value: number
    label?: string
  }
}

export default function StatsCard({ title, value, description, trend }: StatsCardProps) {
  const TrendIcon = trend
    ? trend.value > 0
      ? TrendingUp
      : trend.value < 0
        ? TrendingDown
        : Minus
    : null

  const trendColor = trend
    ? trend.value > 0
      ? 'text-emerald-400'
      : trend.value < 0
        ? 'text-red-400'
        : 'text-muted-foreground'
    : ''

  return (
    <div className="rounded-xl ring-1 ring-foreground/10 bg-card p-5">
      <p className="text-sm font-medium text-muted-foreground">{title}</p>
      <div className="mt-2 flex items-end gap-2">
        <span className="text-3xl font-bold tracking-tight">{value}</span>
        {TrendIcon && trend && (
          <span className={`mb-0.5 flex items-center gap-0.5 text-xs font-medium ${trendColor}`}>
            <TrendIcon className="h-3.5 w-3.5" />
            {trend.value > 0 ? '+' : ''}{trend.value}%
            {trend.label && <span className="text-muted-foreground"> {trend.label}</span>}
          </span>
        )}
      </div>
      {description && (
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      )}
    </div>
  )
}
