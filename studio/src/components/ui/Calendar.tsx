import { useState, useCallback, useMemo } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import Button from '@/components/ui/Button'
import { formatDateYMD, getWeekRange, getMonthRange } from '@/lib/labels'

// --- Helpers ---

function parseDate(s: string): Date {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(y, m - 1, d)
}

function isSameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear()
    && a.getMonth() === b.getMonth()
    && a.getDate() === b.getDate()
}

function getDaysInMonth(year: number, month: number): Date[] {
  const firstDay = new Date(year, month, 1)
  // Monday=0, Tuesday=1, ..., Sunday=6
  let startWeekday = firstDay.getDay() - 1
  if (startWeekday < 0) startWeekday = 6

  const startDate = new Date(year, month, 1 - startWeekday)

  const days: Date[] = []
  // Always 6 rows * 7 = 42 cells to keep grid stable
  for (let i = 0; i < 42; i++) {
    const d = new Date(startDate)
    d.setDate(startDate.getDate() + i)
    days.push(d)
  }
  return days
}

// --- Component ---

interface CalendarRangePickerProps {
  value: { from: string; to: string } | null
  onChange: (range: { from: string; to: string } | null) => void
  onClose: () => void
}

export function CalendarRangePicker({ value, onChange, onClose }: CalendarRangePickerProps) {
  const today = useMemo(() => {
    const now = new Date()
    return new Date(now.getFullYear(), now.getMonth(), now.getDate())
  }, [])

  const [viewMonth, setViewMonth] = useState(() => {
    if (value?.from) {
      const d = parseDate(value.from)
      return { year: d.getFullYear(), month: d.getMonth() }
    }
    return { year: today.getFullYear(), month: today.getMonth() }
  })

  // Internal selection state: null | { from: Date; to?: Date }
  const [pending, setPending] = useState<{ from: Date; to?: Date } | null>(() => {
    if (value?.from) {
      const from = parseDate(value.from)
      if (value.to) {
        return { from, to: parseDate(value.to) }
      }
      return { from }
    }
    return null
  })

  const days = useMemo(
    () => getDaysInMonth(viewMonth.year, viewMonth.month),
    [viewMonth]
  )

  const prevMonth = useCallback(() => {
    setViewMonth((prev) => {
      const m = prev.month === 0 ? 11 : prev.month - 1
      const y = prev.month === 0 ? prev.year - 1 : prev.year
      return { year: y, month: m }
    })
  }, [])

  const nextMonth = useCallback(() => {
    setViewMonth((prev) => {
      const m = prev.month === 11 ? 0 : prev.month + 1
      const y = prev.month === 11 ? prev.year + 1 : prev.year
      return { year: y, month: m }
    })
  }, [])

  const handleDayClick = useCallback((day: Date) => {
    setPending((prev) => {
      if (!prev || prev.to) {
        // Start new selection
        return { from: day }
      }
      // Complete selection
      if (day < prev.from) {
        return { from: day, to: prev.from }
      }
      return { from: prev.from, to: day }
    })
  }, [])

  const handleQuickSelect = useCallback(
    (type: 'today' | 'week' | 'month') => {
      let range: { from: string; to: string }
      switch (type) {
        case 'today':
          range = { from: formatDateYMD(today), to: formatDateYMD(today) }
          break
        case 'week':
          range = getWeekRange(today)
          break
        case 'month':
          range = getMonthRange(today)
          break
      }
      setPending({ from: parseDate(range.from), to: parseDate(range.to) })
      // Jump view to the selected month
      const d = parseDate(range.from)
      setViewMonth({ year: d.getFullYear(), month: d.getMonth() })
    },
    [today]
  )

  const handleApply = useCallback(() => {
    if (pending) {
      if (pending.to) {
        const from = formatDateYMD(pending.from)
        const to = formatDateYMD(pending.to)
        onChange({ from, to })
      } else {
        const d = formatDateYMD(pending.from)
        onChange({ from: d, to: d })
      }
      onClose()
    }
  }, [pending, onChange, onClose])

  const handleClear = useCallback(() => {
    setPending(null)
    onChange(null)
  }, [onChange])

  // Determine range for highlighting
  const rangeFrom = pending?.from
  const rangeTo = pending?.to ?? pending?.from

  const monthLabel = `${viewMonth.year}年${viewMonth.month + 1}月`
  const weekLabels = ['一', '二', '三', '四', '五', '六', '日']

  return (
    <div className="w-[320px] select-none rounded-lg border border-border bg-card p-3 shadow-xl">
      {/* Header: month/year + nav */}
      <div className="mb-2 flex items-center justify-between">
        <Button variant="ghost" size="icon-xs" onClick={prevMonth}>
          <ChevronLeft className="size-4" />
        </Button>
        <span className="text-sm font-medium text-foreground">{monthLabel}</span>
        <Button variant="ghost" size="icon-xs" onClick={nextMonth}>
          <ChevronRight className="size-4" />
        </Button>
      </div>

      {/* Quick select */}
      <div className="mb-2 flex gap-1">
        <Button variant="ghost" size="xs" onClick={() => handleQuickSelect('today')}>
          今天
        </Button>
        <Button variant="ghost" size="xs" onClick={() => handleQuickSelect('week')}>
          本周
        </Button>
        <Button variant="ghost" size="xs" onClick={() => handleQuickSelect('month')}>
          本月
        </Button>
      </div>

      {/* Day-of-week headers */}
      <div className="grid grid-cols-7 mb-1">
        {weekLabels.map((label) => (
          <div
            key={label}
            className="flex h-8 items-center justify-center text-xs font-medium text-muted-foreground"
          >
            {label}
          </div>
        ))}
      </div>

      {/* Day grid */}
      <div className="grid grid-cols-7">
        {days.map((day, i) => {
          const isCurrentMonth = day.getMonth() === viewMonth.month
          const isToday = isSameDay(day, today)
          const isInRange =
            rangeFrom &&
            rangeTo &&
            day >= rangeFrom &&
            day <= rangeTo &&
            isCurrentMonth
          const isStart =
            rangeFrom && isSameDay(day, rangeFrom) && isCurrentMonth
          const isEnd =
            rangeTo && !isSameDay(rangeFrom!, rangeTo!) && isSameDay(day, rangeTo) && isCurrentMonth
          const isSingle =
            rangeFrom &&
            rangeTo &&
            isSameDay(rangeFrom, rangeTo) &&
            isSameDay(day, rangeFrom) &&
            isCurrentMonth

          return (
            <button
              key={i}
              type="button"
              onClick={() => handleDayClick(day)}
              className={cn(
                'relative flex h-8 items-center justify-center rounded-md text-sm transition-colors',
                isCurrentMonth
                  ? 'text-foreground'
                  : 'text-muted-foreground/40',
                !isInRange && !isStart && !isEnd && !isSingle && isCurrentMonth &&
                  'hover:bg-muted',
                isInRange && !isStart && !isEnd && 'bg-primary/15',
                isToday && !isStart && !isEnd && !isSingle &&
                  'ring-1 ring-primary/60',
              )}
            >
              {/* Range start highlight */}
              {isStart && !isSingle && (
                <>
                  <span className="absolute inset-y-0 right-0 w-1/2 bg-primary/15" />
                  <span className="relative flex h-8 w-full items-center justify-center rounded-l-md bg-primary/20">
                    {day.getDate()}
                  </span>
                </>
              )}
              {/* Range end highlight */}
              {isEnd && (
                <>
                  <span className="absolute inset-y-0 left-0 w-1/2 bg-primary/15" />
                  <span className="relative flex h-8 w-full items-center justify-center rounded-r-md bg-primary/20">
                    {day.getDate()}
                  </span>
                </>
              )}
              {/* Single day selection */}
              {isSingle && (
                <span className="relative flex h-8 w-full items-center justify-center rounded-md bg-primary/20">
                  {day.getDate()}
                </span>
              )}
              {/* Normal day */}
              {!isStart && !isEnd && !isSingle && day.getDate()}
            </button>
          )
        })}
      </div>

      {/* Footer: selected range info + actions */}
      <div className="mt-2 flex items-center justify-between border-t border-border pt-2">
        <span className="truncate text-xs text-muted-foreground">
          {pending
            ? pending.to
              ? `${formatDateYMD(pending.from)} ~ ${formatDateYMD(pending.to)}`
              : `${formatDateYMD(pending.from)} ~ ?`
            : '选择日期范围'}
        </span>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="xs" onClick={handleClear}>
            清除
          </Button>
          <Button
            variant="default"
            size="xs"
            disabled={!pending}
            onClick={handleApply}
          >
            确定
          </Button>
        </div>
      </div>
    </div>
  )
}

export default CalendarRangePicker
