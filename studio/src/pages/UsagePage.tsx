import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import PageHeader from '@/components/layout/PageHeader'
import QueryErrorState from '@/components/QueryErrorState'
import StatsCard from '@/components/StatsCard'
import StatsCardSkeleton from '@/components/StatsCardSkeleton'
import { Card } from '@/components/ui/card'
import { api } from '@/lib/api'
import { contentTypeLabel, formatDateYMD } from '@/lib/labels'
import { queryKeys } from '@/lib/query-keys'
import type { UsageStats } from '@/types'

type DateRange = '7d' | '30d' | '90d' | 'this_month'

const dateRangeOptions: { value: DateRange; label: string }[] = [
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: '90d', label: '近 90 天' },
  { value: 'this_month', label: '本月' },
]

function getDateRange(range: DateRange): { from: string; to: string } {
  const now = new Date()
  const to = formatDateYMD(now)
  if (range === 'this_month') {
    return { from: formatDateYMD(new Date(now.getFullYear(), now.getMonth(), 1)), to }
  }
  const days = range === '7d' ? 7 : range === '90d' ? 90 : 30
  const from = new Date(now)
  from.setDate(now.getDate() - days + 1)
  return { from: formatDateYMD(from), to }
}

export default function UsagePage() {
  const [dateRange, setDateRange] = useState<DateRange>('30d')
  const params = useMemo(() => getDateRange(dateRange), [dateRange])
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: queryKeys.usage.stats(params),
    queryFn: () => api.usage.stats(params),
  })

  if (isError) {
    return (
      <div className="space-y-6">
        <PageHeader title="任务用量" description="查看各类创作任务数量。" />
        <QueryErrorState onRetry={() => refetch()} />
      </div>
    )
  }

  const stats: UsageStats = data ?? { total_tasks: 0, by_type: {} }
  const byType = Object.entries(stats.by_type ?? {}).sort((a, b) => b[1].count - a[1].count)

  return (
    <div className="space-y-6">
      <PageHeader title="任务用量" description="查看各类创作任务数量。">
        <div className="flex items-center gap-1 rounded-lg border border-border p-0.5">
          {dateRangeOptions.map(({ value, label }) => (
            <button
              key={value}
              onClick={() => setDateRange(value)}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                dateRange === value ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </PageHeader>

      {isLoading ? <StatsCardSkeleton /> : (
        <div className="max-w-sm">
          <StatsCard title="任务数" value={stats.total_tasks.toLocaleString()} description="按创建时间统计" />
        </div>
      )}

      <Card>
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold text-foreground">按类型分组</h2>
        </div>
        {isLoading ? (
          <div className="p-4 text-sm text-muted-foreground">加载中...</div>
        ) : byType.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground">当前时间范围内暂无任务</div>
        ) : (
          <div className="divide-y divide-border">
            {byType.map(([type, entry]) => (
              <div key={type} className="flex items-center justify-between px-4 py-3 text-sm">
                <span className="font-medium">{contentTypeLabel[type as keyof typeof contentTypeLabel] ?? type}</span>
                <span className="tabular-nums text-muted-foreground">{entry.count.toLocaleString()} 个</span>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
