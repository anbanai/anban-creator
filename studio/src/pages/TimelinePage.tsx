import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { Calendar, Loader2, RefreshCw } from 'lucide-react'
import PageHeader from '@/components/layout/PageHeader'
import { api, type TimelineItem } from '@/lib/api'
import { taskStatusLabel, planStatusLabel, contentTypeLabel, timelineItemTypeLabel, formatDateLabelCN, formatMonthCN, formatTimeCN } from '@/lib/labels'
import Badge from '@/components/ui/Badge'
import Button from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'
import EmptyState from '@/components/EmptyState'

type ViewMode = 'day' | 'week' | 'month'

function getDateRange(mode: ViewMode): { from: string; to: string } {
  const now = new Date()
  const from = new Date(now)
  from.setDate(now.getDate() - 1)

  if (mode === 'day') {
    const to = new Date(from)
    to.setDate(from.getDate() + 2)
    return { from: formatDate(from), to: formatDate(to) }
  }
  if (mode === 'week') {
    const to = new Date(from)
    to.setDate(from.getDate() + 7)
    return { from: formatDate(from), to: formatDate(to) }
  }
  const to = new Date(from)
  to.setDate(from.getDate() + 30)
  return { from: formatDate(from), to: formatDate(to) }
}

function formatDate(d: Date): string {
  return d.toISOString().split('T')[0]
}

function statusBadge(status: string, type: string) {
  if (type === 'plan') {
    return status === 'active' ? 'info' : 'neutral'
  }
  switch (status) {
    case 'completed': return 'success'
    case 'failed': return 'danger'
    case 'running': return 'warning'
    case 'cancelled': return 'neutral'
    default: return 'neutral'
  }
}

const getItemDate = (item: TimelineItem) => {
  return item.scheduled_at || new Date().toISOString()
}

interface GroupedItems {
  [dateKey: string]: TimelineItem[]
}

interface MonthGroup {
  month: string
  dates: { dateKey: string; label: string; items: TimelineItem[] }[]
}

export default function TimelinePage() {
  const [searchParams] = useSearchParams()
  const initialMode: ViewMode = (searchParams.get('mode') as ViewMode) || 'week'

  const [viewMode, setViewMode] = useState<ViewMode>(initialMode)
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')

  const dateRange = useMemo(() => {
    if (customFrom && customTo) {
      return { from: customFrom, to: customTo }
    }
    return getDateRange(viewMode)
  }, [viewMode, customFrom, customTo])

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['timeline', dateRange.from, dateRange.to],
    queryFn: () => api.timeline.get(dateRange.from, dateRange.to),
    refetchInterval: 10000,
  })

  const items = data?.items ?? []

  const grouped = useMemo((): MonthGroup[] => {
    const byDate: GroupedItems = {}
    for (const item of items) {
      const dateKey = getItemDate(item).split('T')[0]
      if (!byDate[dateKey]) byDate[dateKey] = []
      byDate[dateKey].push(item)
    }

    for (const key of Object.keys(byDate)) {
      byDate[key].sort((a, b) =>
        new Date(getItemDate(a)).getTime() - new Date(getItemDate(b)).getTime()
      )
    }

    const monthMap: Record<string, { dateKey: string; label: string; items: TimelineItem[] }[]> = {}
    const sortedDates = Object.keys(byDate).sort()

    for (const dateKey of sortedDates) {
      const month = formatMonthCN(dateKey)
      if (!monthMap[month]) monthMap[month] = []
      monthMap[month].push({
        dateKey,
        label: formatDateLabelCN(dateKey),
        items: byDate[dateKey],
      })
    }

    return Object.entries(monthMap).map(([month, dates]) => ({ month, dates }))
  }, [items])

  const hasRunningItems = items.some(
    (i) => i.type === 'task' && i.status === 'running'
  )

  const viewModeLabels: Record<string, string> = { day: '日', week: '周', month: '月' }

  return (
    <div className="space-y-6">
      <PageHeader title="时间线" description="你的内容排期日历。">
        <div className="flex items-center gap-2">
          {(['day', 'week', 'month'] as ViewMode[]).map((mode) => (
            <Button
              key={mode}
              variant={viewMode === mode && !customFrom ? 'default' : 'secondary'}
              size="sm"
              onClick={() => {
                setViewMode(mode)
                setCustomFrom('')
                setCustomTo('')
              }}
            >
              {viewModeLabels[mode]}
            </Button>
          ))}
        </div>
      </PageHeader>

      {/* Custom date range */}
      <Card>
        <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-end">
          <Input
            label="开始"
            type="date"
            value={customFrom}
            onChange={(e) => setCustomFrom(e.target.value)}
            className="sm:max-w-[180px]"
          />
          <Input
            label="结束"
            type="date"
            value={customTo}
            onChange={(e) => setCustomTo(e.target.value)}
            className="sm:max-w-[180px]"
          />
          {(customFrom || customTo) && (
            <Button variant="ghost" size="sm" onClick={() => { setCustomFrom(''); setCustomTo('') }}>
              清除
            </Button>
          )}
          <Button variant="secondary" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </Button>
        </div>
      </Card>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      ) : grouped.length === 0 ? (
        <EmptyState
          icon={Calendar}
          title="没有排期内容"
          description="创建计划或任务开始创作。"
        />
      ) : (
        <div className="space-y-8">
          {grouped.map((monthGroup) => (
            <div key={monthGroup.month}>
              <div className="mb-4 flex items-center gap-3">
                <h2 className="text-lg font-semibold text-foreground">{monthGroup.month}</h2>
                <div className="h-px flex-1 bg-border" />
              </div>

              <div className="relative ml-4 border-l border-border pl-6">
                {monthGroup.dates.map((dateGroup) => (
                  <div key={dateGroup.dateKey} className="mb-6 last:mb-0">
                    <div className="mb-3 flex items-center gap-2">
                      <div className="absolute -left-[5px] h-2.5 w-2.5 rounded-full border-2 border-primary bg-card" />
                      <span className="text-sm font-medium text-foreground">{dateGroup.label}</span>
                      <span className="text-xs text-muted-foreground">
                        ({dateGroup.dateKey})
                      </span>
                    </div>

                    <div className="space-y-2">
                      {dateGroup.items.map((item) => {
                        const linkTo = item.type === 'plan'
                          ? `/plans`
                          : `/tasks/${item.id}`

                        return (
                          <div
                            key={`${item.type}-${item.id}`}
                            className="group rounded-lg border border-border bg-card px-4 py-3 transition-colors duration-150 hover:border-primary/30"
                          >
                            <Link to={linkTo} className="block">
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0 flex-1">
                                  <div className="flex items-center gap-2">
                                    <span className="text-xs text-muted-foreground">
                                      {formatTimeCN(getItemDate(item))}
                                    </span>
                                    <Badge variant="outline" className="text-[10px]">
                                      {contentTypeLabel[item.content_type] || item.content_type}
                                    </Badge>
                                    <Badge variant="outline" className="text-[10px]">
                                      {timelineItemTypeLabel[item.type] || item.type}
                                    </Badge>
                                    {item.type === 'task' && item.status === 'running' && (
                                      <Badge variant="warning" className="text-[10px]">
                                        {item.progress ?? 0}%
                                      </Badge>
                                    )}
                                  </div>
                                  <p className="mt-1 truncate text-sm font-medium text-foreground">
                                    {item.title}
                                  </p>
                                  {item.error && (
                                    <p className="mt-1 truncate text-xs text-red-400">{item.error}</p>
                                  )}
                                </div>
                                <Badge variant={statusBadge(item.status, item.type)}>
                                  {item.type === 'plan' ? (planStatusLabel[item.status] || item.status) : (taskStatusLabel[item.status] || item.status)}
                                </Badge>
                              </div>
                            </Link>

                            {item.type === 'plan' && (
                              <div className="mt-2 flex items-center gap-2">
                                {item.status === 'active' && (
                                  <button
                                    className="text-xs text-primary hover:text-primary/80"
                                    onClick={(e) => {
                                      e.preventDefault()
                                      api.plans.pause(item.id).then(() => refetch())
                                    }}
                                  >
                                    暂停
                                  </button>
                                )}
                                {item.status === 'paused' && (
                                  <button
                                    className="text-xs text-emerald-400 hover:text-emerald-300"
                                    onClick={(e) => {
                                      e.preventDefault()
                                      api.plans.resume(item.id).then(() => refetch())
                                    }}
                                  >
                                    恢复
                                  </button>
                                )}
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Auto-refresh indicator */}
      {hasRunningItems && (
        <div className="fixed bottom-4 right-4 flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2 text-xs text-muted-foreground shadow-lg">
          <span className="h-2 w-2 animate-pulse-dot rounded-full bg-primary" />
          自动刷新中（运行中的任务）
        </div>
      )}
    </div>
  )
}
