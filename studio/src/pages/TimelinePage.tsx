import { useState, useMemo, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { Calendar, Loader2, RefreshCw } from 'lucide-react'
import PageHeader from '@/components/layout/PageHeader'
import { api, type TimelineItem } from '@/lib/api'
import {
  taskStatusLabel,
  planStatusLabel,
  contentTypeLabel,
  timelineItemTypeLabel,
  contentTypeFilterOptions,
  timelineItemTypeOptions,
  timelineStatusOptions,
  timelineSortOptions,
  formatDateLabelCN,
  formatMonthCN,
  formatTimeCN,
  getWeekRange,
} from '@/lib/labels'
import Badge from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Card } from '@/components/ui/Card'
import EmptyState from '@/components/EmptyState'
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover'
import { CalendarRangePicker } from '@/components/ui/Calendar'
import { ChannelSelector } from '@/components/ChannelSelector'

// --- Helpers ---

function getDefaultRange(): { from: string; to: string } {
  return getWeekRange(new Date())
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
  return item.scheduled_at || item.completed_at || item.created_at || new Date().toISOString()
}

// --- Interfaces ---

interface MonthGroup {
  month: string
  dates: { dateKey: string; label: string; items: TimelineItem[] }[]
}

// --- Component ---

export default function TimelinePage() {
  const [searchParams, setSearchParams] = useSearchParams()

  // Read filter state from URL params
  const [calendarOpen, setCalendarOpen] = useState(false)

  const dateFrom = searchParams.get('from') || ''
  const dateTo = searchParams.get('to') || ''
  const itemType = searchParams.get('item_type') || ''
  const contentType = searchParams.get('content_type') || ''
  const status = searchParams.get('status') || ''
  const channelId = searchParams.get('channel_id') || ''
  const sort = searchParams.get('sort') || 'date_asc'

  // Derived date range
  const dateRange = useMemo(() => {
    if (dateFrom && dateTo) {
      return { from: dateFrom, to: dateTo }
    }
    return getDefaultRange()
  }, [dateFrom, dateTo])

  const hasCustomDates = !!(dateFrom && dateTo)

  // Update URL params
  const updateFilter = useCallback((key: string, value: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      if (value) {
        next.set(key, value)
      } else {
        next.delete(key)
      }
      return next
    })
  }, [setSearchParams])

  // Data fetching with filters
  const { data, isLoading, refetch } = useQuery({
    queryKey: ['timeline', dateRange.from, dateRange.to, itemType, contentType, status, channelId],
    queryFn: () =>
      api.timeline.get(dateRange.from, dateRange.to, {
        item_type: itemType || undefined,
        content_type: contentType || undefined,
        status: status || undefined,
        channel_id: channelId || undefined,
      }),
    refetchInterval: 10000,
  })

  const items = data?.items ?? []

  // Sort items
  const sortedItems = useMemo(() => {
    const sorted = [...items]
    switch (sort) {
      case 'date_desc':
        sorted.sort((a, b) => new Date(getItemDate(b)).getTime() - new Date(getItemDate(a)).getTime())
        break
      case 'status':
        sorted.sort((a, b) => a.status.localeCompare(b.status))
        break
      case 'title':
        sorted.sort((a, b) => a.title.localeCompare(b.title))
        break
      default: // date_asc
        sorted.sort((a, b) => new Date(getItemDate(a)).getTime() - new Date(getItemDate(b)).getTime())
    }
    return sorted
  }, [items, sort])

  // Group by month/date
  const grouped = useMemo((): MonthGroup[] => {
    const byDate: Record<string, TimelineItem[]> = {}
    for (const item of sortedItems) {
      const dateKey = getItemDate(item).split('T')[0]
      if (!byDate[dateKey]) byDate[dateKey] = []
      byDate[dateKey].push(item)
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
  }, [sortedItems])

  const hasRunningItems = items.some(
    (i) => i.type === 'task' && i.status === 'running'
  )

  // Format the date range display
  const dateRangeLabel = useMemo(() => {
    if (!hasCustomDates) return '本周'
    const from = new Date(dateRange.from + 'T00:00:00')
    const to = new Date(dateRange.to + 'T00:00:00')
    const fmt = (d: Date) => `${d.getMonth() + 1}/${d.getDate()}`
    return from.getTime() === to.getTime()
      ? fmt(from)
      : `${fmt(from)} - ${fmt(to)}`
  }, [dateRange, hasCustomDates])

  return (
    <div className="space-y-6">
      <PageHeader title="时间轴" description="你的内容排期日历。">
        <Button variant="secondary" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-3.5 w-3.5" />
          刷新
        </Button>
      </PageHeader>

      {/* Filter Bar */}
      <Card>
        <div className="flex flex-col gap-3 px-4 py-3 lg:flex-row lg:items-center lg:flex-wrap">
          {/* Date Range Picker */}
          <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
            <PopoverTrigger
              render={
                <Button variant="outline" size="sm" className="gap-1.5 min-w-[140px]">
                  <Calendar className="size-3.5" />
                  {dateRangeLabel}
                </Button>
              }
            />
            <PopoverContent align="start" side="bottom" className="p-0 border-0 bg-transparent shadow-none">
              <CalendarRangePicker
                value={hasCustomDates ? { from: dateRange.from, to: dateRange.to } : null}
                onChange={(range) => {
                  if (range) {
                    updateFilter('from', range.from)
                    updateFilter('to', range.to)
                  } else {
                    updateFilter('from', '')
                    updateFilter('to', '')
                  }
                }}
                onClose={() => setCalendarOpen(false)}
              />
            </PopoverContent>
          </Popover>

          {/* Item Type Filter */}
          <Select value={itemType || undefined} onValueChange={(v) => updateFilter('item_type', v ?? '')}>
            <SelectTrigger size="sm" className="min-w-[100px]">
              <SelectValue placeholder="全部类型" />
            </SelectTrigger>
            <SelectContent>
              {timelineItemTypeOptions.map((opt) => (
                <SelectItem key={opt.value} value={opt.value} label={opt.label}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Content Type Filter */}
          <Select value={contentType || undefined} onValueChange={(v) => updateFilter('content_type', v ?? '')}>
            <SelectTrigger size="sm" className="min-w-[110px]">
              <SelectValue placeholder="全部内容" />
            </SelectTrigger>
            <SelectContent>
              {contentTypeFilterOptions.map((opt) => (
                <SelectItem key={opt.value} value={opt.value} label={opt.label}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Status Filter */}
          <Select value={status || undefined} onValueChange={(v) => updateFilter('status', v ?? '')}>
            <SelectTrigger size="sm" className="min-w-[100px]">
              <SelectValue placeholder="全部状态" />
            </SelectTrigger>
            <SelectContent>
              {timelineStatusOptions.map((opt) => (
                <SelectItem key={opt.value} value={opt.value} label={opt.label}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Channel Filter */}
          <div className="w-36">
            <ChannelSelector
              value={channelId}
              onChange={(id) => updateFilter('channel_id', id)}
            />
          </div>

          {/* Sort */}
          <Select value={sort || undefined} onValueChange={(v) => updateFilter('sort', v ?? '')}>
            <SelectTrigger size="sm" className="min-w-[100px]">
              <SelectValue placeholder="日期 ↑" />
            </SelectTrigger>
            <SelectContent>
              {timelineSortOptions.map((opt) => (
                <SelectItem key={opt.value} value={opt.value} label={opt.label}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Clear all filters */}
          {(itemType || contentType || status || channelId || hasCustomDates) && (
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                setSearchParams({})
              }}
            >
              清除筛选
            </Button>
          )}
        </div>
      </Card>

      {/* Content */}
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
                          ? `/plans?highlight=${item.id}`
                          : `/tasks/${item.id}`

                        return (
                          <div
                            key={`${item.type}-${item.id}`}
                            className="group rounded-lg border border-border bg-card px-4 py-3 transition-colors duration-150 hover:border-primary/30"
                          >
                            <Link to={linkTo} className="block">
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0 flex-1">
                                  <div className="flex flex-wrap items-center gap-1.5">
                                    <span className="text-xs text-muted-foreground">
                                      {formatTimeCN(getItemDate(item))}
                                    </span>
                                    <Badge variant="outline" className="text-[10px]">
                                      {contentTypeLabel[item.content_type] || item.content_type}
                                    </Badge>
                                    <Badge variant="outline" className="text-[10px]">
                                      {timelineItemTypeLabel[item.type] || item.type}
                                    </Badge>
                                    {item.channel_name && (
                                      <Badge variant="secondary" className="text-[10px]">
                                        {item.channel_name}
                                      </Badge>
                                    )}
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
