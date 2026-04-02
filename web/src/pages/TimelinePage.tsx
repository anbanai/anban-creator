import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type TimelineItem } from '@/lib/api'
import Badge from '@/components/ui/Badge'
import Button from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'

type ViewMode = 'day' | 'week' | 'month'

function getDateRange(mode: ViewMode): { from: string; to: string } {
  const now = new Date()
  const from = new Date(now)
  from.setDate(now.getDate() - 1) // start from yesterday

  if (mode === 'day') {
    const to = new Date(from)
    to.setDate(from.getDate() + 2)
    return {
      from: formatDate(from),
      to: formatDate(to),
    }
  }
  if (mode === 'week') {
    const to = new Date(from)
    to.setDate(from.getDate() + 7)
    return {
      from: formatDate(from),
      to: formatDate(to),
    }
  }
  // month
  const to = new Date(from)
  to.setDate(from.getDate() + 30)
  return {
    from: formatDate(from),
    to: formatDate(to),
  }
}

function formatDate(d: Date): string {
  return d.toISOString().split('T')[0]
}

function formatDateLabel(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const tomorrow = new Date(today)
  tomorrow.setDate(today.getDate() + 1)

  const dDate = new Date(d)
  dDate.setHours(0, 0, 0, 0)

  const diff = dDate.getTime() - today.getTime()

  if (diff === 0) return 'Today'
  if (diff === 86400000) return 'Tomorrow'
  if (diff === -86400000) return 'Yesterday'

  return d.toLocaleDateString('en-US', {
    weekday: 'long',
    month: 'short',
    day: 'numeric',
  })
}

function formatTime(dateStr: string): string {
  const d = new Date(dateStr)
  return d.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function formatMonth(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  return d.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })
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

function contentTypeIcon(type: string) {
  switch (type) {
    case 'rednote': return 'RedNote'
    case 'article': return 'Article'
    case 'xls': return 'XLS'
    default: return type
  }
}

function itemTypeIcon(item: TimelineItem) {
  return item.type === 'plan' ? 'plan' : 'task'
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
    refetchInterval: 10000, // auto-refresh every 10s for running tasks
  })

  const items = data?.items ?? []

  const grouped = useMemo((): MonthGroup[] => {
    const byDate: GroupedItems = {}
    for (const item of items) {
      const dateKey = item.scheduled_at.split('T')[0]
      if (!byDate[dateKey]) byDate[dateKey] = []
      byDate[dateKey].push(item)
    }

    // Sort items within each date by time
    for (const key of Object.keys(byDate)) {
      byDate[key].sort((a, b) =>
        new Date(a.scheduled_at).getTime() - new Date(b.scheduled_at).getTime()
      )
    }

    // Group by month
    const monthMap: Record<string, { dateKey: string; label: string; items: TimelineItem[] }[]> = {}
    const sortedDates = Object.keys(byDate).sort()

    for (const dateKey of sortedDates) {
      const month = formatMonth(dateKey)
      if (!monthMap[month]) monthMap[month] = []
      monthMap[month].push({
        dateKey,
        label: formatDateLabel(dateKey),
        items: byDate[dateKey],
      })
    }

    return Object.entries(monthMap).map(([month, dates]) => ({ month, dates }))
  }, [items])

  const hasRunningItems = items.some(
    (i) => i.type === 'task' && i.status === 'running'
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">Timeline</h1>
          <p className="mt-1 text-sm text-gray-400">Your scheduled content calendar.</p>
        </div>
        <div className="flex items-center gap-2">
          {(['day', 'week', 'month'] as ViewMode[]).map((mode) => (
            <Button
              key={mode}
              variant={viewMode === mode && !customFrom ? 'primary' : 'secondary'}
              size="sm"
              onClick={() => {
                setViewMode(mode)
                setCustomFrom('')
                setCustomTo('')
              }}
            >
              {mode.charAt(0).toUpperCase() + mode.slice(1)}
            </Button>
          ))}
        </div>
      </div>

      {/* Custom date range */}
      <Card>
        <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-end">
          <Input
            label="From"
            type="date"
            value={customFrom}
            onChange={(e) => setCustomFrom(e.target.value)}
            className="sm:max-w-[180px]"
          />
          <Input
            label="To"
            type="date"
            value={customTo}
            onChange={(e) => setCustomTo(e.target.value)}
            className="sm:max-w-[180px]"
          />
          {(customFrom || customTo) && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setCustomFrom('')
                setCustomTo('')
              }}
            >
              Clear
            </Button>
          )}
          <Button variant="secondary" size="sm" onClick={() => refetch()}>
            Refresh
          </Button>
        </div>
      </Card>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <svg className="h-8 w-8 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
      ) : grouped.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-gray-700 bg-gray-800 py-16">
          <svg className="mb-4 h-12 w-12 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
          </svg>
          <p className="text-sm text-gray-400">No content scheduled</p>
          <p className="mt-1 text-xs text-gray-500">Create a plan or task to get started.</p>
        </div>
      ) : (
        <div className="space-y-8">
          {grouped.map((monthGroup) => (
            <div key={monthGroup.month}>
              {/* Month header */}
              <div className="mb-4 flex items-center gap-3">
                <h2 className="text-lg font-semibold text-gray-200">{monthGroup.month}</h2>
                <div className="h-px flex-1 bg-gray-700" />
              </div>

              {/* Date groups */}
              <div className="relative ml-4 border-l-2 border-gray-700 pl-6">
                {monthGroup.dates.map((dateGroup) => (
                  <div key={dateGroup.dateKey} className="mb-6 last:mb-0">
                    {/* Date header */}
                    <div className="mb-3 flex items-center gap-2">
                      <div className="absolute -left-[1.05rem] h-3 w-3 rounded-full border-2 border-gray-600 bg-gray-800" />
                      <span className="text-sm font-medium text-gray-300">{dateGroup.label}</span>
                      <span className="text-xs text-gray-500">
                        ({dateGroup.dateKey})
                      </span>
                    </div>

                    {/* Items */}
                    <div className="space-y-2">
                      {dateGroup.items.map((item, idx) => {
                        const isLast = idx === dateGroup.items.length - 1
                        const linkTo = item.type === 'plan'
                          ? `/plans` // plan list for now
                          : `/tasks/${item.id}`

                        return (
                          <div
                            key={`${item.type}-${item.id}`}
                            className={`group relative rounded-lg border border-gray-700 bg-gray-800 px-4 py-3 transition-colors hover:border-gray-600 ${
                              isLast ? '' : ''
                            }`}
                          >
                            <Link to={linkTo} className="block">
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0 flex-1">
                                  <div className="flex items-center gap-2">
                                    <span className="text-xs text-gray-500">
                                      {formatTime(item.scheduled_at)}
                                    </span>
                                    <Badge variant="outline" className="text-[10px]">
                                      {contentTypeIcon(item.content_type)}
                                    </Badge>
                                    <Badge variant="outline" className="text-[10px]">
                                      {itemTypeIcon(item)}
                                    </Badge>
                                    {item.type === 'task' && item.status === 'running' && (
                                      <Badge variant="warning" className="text-[10px]">
                                        {item.progress ?? 0}%
                                      </Badge>
                                    )}
                                  </div>
                                  <p className="mt-1 truncate text-sm font-medium text-gray-100">
                                    {item.title}
                                  </p>
                                  {item.error && (
                                    <p className="mt-1 truncate text-xs text-red-400">{item.error}</p>
                                  )}
                                </div>
                                <Badge variant={statusBadge(item.status, item.type)}>
                                  {item.status}
                                </Badge>
                              </div>
                            </Link>

                            {/* Actions for plan items */}
                            {item.type === 'plan' && (
                              <div className="mt-2 flex items-center gap-2">
                                {item.status === 'active' && (
                                  <button
                                    className="text-xs text-amber-400 hover:text-amber-300"
                                    onClick={(e) => {
                                      e.preventDefault()
                                      api.plans.pause(Number(item.id)).then(() => refetch())
                                    }}
                                  >
                                    Pause
                                  </button>
                                )}
                                {item.status === 'paused' && (
                                  <button
                                    className="text-xs text-green-400 hover:text-green-300"
                                    onClick={(e) => {
                                      e.preventDefault()
                                      api.plans.resume(Number(item.id)).then(() => refetch())
                                    }}
                                  >
                                    Resume
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
        <div className="fixed bottom-4 right-4 flex items-center gap-2 rounded-lg bg-gray-800 border border-gray-700 px-3 py-2 text-xs text-gray-400 shadow-lg">
          <span className="h-2 w-2 animate-pulse rounded-full bg-amber-500" />
          Auto-refreshing (running tasks)
        </div>
      )}
    </div>
  )
}
