import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import StatsCardSkeleton from '@/components/StatsCardSkeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { contentTypeLabel } from '@/lib/labels'
import { formatDateYMD } from '@/lib/labels'
import { Card } from '@/components/ui/Card'
import StatsCard from '@/components/StatsCard'
import PageHeader from '@/components/layout/PageHeader'
import type { UsageStats, TypeStatEntry } from '@/types'

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

function formatUSD(n: number): string {
  if (n < 0.01) return `$${n.toFixed(4)}`
  return `$${n.toFixed(2)}`
}

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
  switch (range) {
    case '7d': {
      const from = new Date(now)
      from.setDate(now.getDate() - 6)
      return { from: formatDateYMD(from), to }
    }
    case '30d': {
      const from = new Date(now)
      from.setDate(now.getDate() - 29)
      return { from: formatDateYMD(from), to }
    }
    case '90d': {
      const from = new Date(now)
      from.setDate(now.getDate() - 89)
      return { from: formatDateYMD(from), to }
    }
    case 'this_month': {
      const from = new Date(now.getFullYear(), now.getMonth(), 1)
      return { from: formatDateYMD(from), to }
    }
  }
}

export default function UsagePage() {
  const [dateRange, setDateRange] = useState<DateRange>('30d')

  const { from, to } = useMemo(() => getDateRange(dateRange), [dateRange])

  const { data: stats, isLoading, isError, refetch } = useQuery({
    queryKey: queryKeys.usage.stats({ from, to }),
    queryFn: () => api.usage.stats({ from, to }),
  })

  if (isError) {
    return (
      <div className="space-y-6">
        <PageHeader title="用量统计" description="查看你的 LLM 资源消耗和费用。" />
        <QueryErrorState onRetry={() => refetch()} />
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <PageHeader title="用量统计" description="查看你的 LLM 资源消耗和费用。" />
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
          {Array.from({ length: 5 }).map((_, i) => <StatsCardSkeleton key={i} />)}
        </div>
      </div>
    )
  }

  const s: UsageStats = stats ?? {
    total_tasks: 0,
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cache_read_tokens: 0,
    total_cache_creation_tokens: 0,
    total_cost_usd: 0,
    by_type: {},
  }

  const byTypeEntries = Object.entries(s.by_type ?? {})

  return (
    <div className="space-y-6">
      <PageHeader title="用量统计" description="查看你的 LLM 资源消耗和费用。">

        <div className="flex items-center gap-1 rounded-lg border border-border p-0.5">
          {dateRangeOptions.map(({ value, label }) => (
            <button
              key={value}
              onClick={() => setDateRange(value)}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                dateRange === value
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </PageHeader>

      {/* Summary cards */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
        <StatsCard
          title="总输入 Tokens"
          value={formatTokenCount(s.total_input_tokens)}
          description="发送到模型的 token 数"
        />
        <StatsCard
          title="总输出 Tokens"
          value={formatTokenCount(s.total_output_tokens)}
          description="模型生成的 token 数"
        />
        <StatsCard
          title="缓存读取"
          value={formatTokenCount(s.total_cache_read_tokens)}
          description="从缓存读取的 token 数"
        />
        <StatsCard
          title="缓存创建"
          value={formatTokenCount(s.total_cache_creation_tokens)}
          description="写入缓存的 token 数"
        />
        <StatsCard
          title="总费用"
          value={formatUSD(s.total_cost_usd)}
          description={`共 ${s.total_tasks} 个任务`}
        />
      </div>

      {/* Per-type breakdown */}
      {byTypeEntries.length > 0 && (
        <Card>
          <div className="border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold text-foreground">按类型分组</h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="px-4 py-3 font-medium">类型</th>
                  <th className="px-4 py-3 font-medium">任务数</th>
                  <th className="px-4 py-3 font-medium">输入 Tokens</th>
                  <th className="px-4 py-3 font-medium">输出 Tokens</th>
                  <th className="px-4 py-3 font-medium">缓存读取</th>
                  <th className="px-4 py-3 font-medium">缓存创建</th>
                  <th className="px-4 py-3 font-medium">费用</th>
                </tr>
              </thead>
              <tbody>
                {byTypeEntries.map(([type, entry]: [string, TypeStatEntry]) => (
                  <tr key={type} className="border-b border-border transition-colors duration-150 hover:bg-accent">
                    <td className="px-4 py-3 font-medium">
                      {contentTypeLabel[type as keyof typeof contentTypeLabel] ?? type}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">{entry.count}</td>
                    <td className="px-4 py-3 text-muted-foreground">{formatTokenCount(entry.input_tokens)}</td>
                    <td className="px-4 py-3 text-muted-foreground">{formatTokenCount(entry.output_tokens)}</td>
                    <td className="px-4 py-3 text-muted-foreground">{formatTokenCount(entry.cache_read_tokens)}</td>
                    <td className="px-4 py-3 text-muted-foreground">{formatTokenCount(entry.cache_creation_tokens)}</td>
                    <td className="px-4 py-3 font-medium">{formatUSD(entry.cost_usd)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </div>
  )
}
