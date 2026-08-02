import { useEffect, useId, useState, type ReactNode } from 'react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import TimePicker from '@/components/TimePicker'

interface SchedulePickerProps {
  value: string
  onChange: (cron: string) => void
  onInteraction?: () => void
  onValidityChange?: (valid: boolean) => void
  footer?: ReactNode
}

type Frequency = 'daily' | 'weekly'

const WEEK_DAYS = [
  { value: 1, label: '周一', shortLabel: '一' },
  { value: 2, label: '周二', shortLabel: '二' },
  { value: 3, label: '周三', shortLabel: '三' },
  { value: 4, label: '周四', shortLabel: '四' },
  { value: 5, label: '周五', shortLabel: '五' },
  { value: 6, label: '周六', shortLabel: '六' },
  { value: 0, label: '周日', shortLabel: '日' },
]

interface ParsedSchedule {
  frequency: Frequency
  days: number[]
  hour: number
  minute: number
  supported: boolean
}

const SAFE_DAILY_SCHEDULE: ParsedSchedule = {
  frequency: 'daily',
  days: [],
  hour: 9,
  minute: 0,
  supported: false,
}

function parseCron(cron: string): ParsedSchedule {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return SAFE_DAILY_SCHEDULE
  const [minuteText, hourText, dayOfMonth, month, weekday] = parts
  if (
    !/^\d{1,2}$/.test(minuteText)
    || !/^\d{1,2}$/.test(hourText)
    || dayOfMonth !== '*'
    || month !== '*'
  ) return SAFE_DAILY_SCHEDULE

  const hour = Number(hourText)
  const minute = Number(minuteText)
  if (hour > 23 || minute > 59) return SAFE_DAILY_SCHEDULE
  if (weekday === '*') return { frequency: 'daily', days: [], hour, minute, supported: true }
  if (!/^[0-6](?:,[0-6])*$/.test(weekday)) return SAFE_DAILY_SCHEDULE

  const days = [...new Set(weekday.split(',').map(Number))]
  return {
    frequency: 'weekly',
    days,
    hour,
    minute,
    supported: true,
  }
}

function toCron(frequency: Frequency, days: number[], time: string): string | null {
  const [hour, minute] = time.split(':').map(Number)
  if (frequency === 'weekly' && days.length === 0) return null
  const weekday = frequency === 'daily' ? '*' : WEEK_DAYS
    .filter(({ value }) => days.includes(value))
    .map(({ value }) => value)
    .join(',')
  return `${minute || 0} ${hour || 0} * * ${weekday}`
}

export default function SchedulePicker({
  value,
  onChange,
  onInteraction,
  onValidityChange,
  footer,
}: SchedulePickerProps) {
  const parsed = parseCron(value)
  const timePickerId = useId()
  const [frequency, setFrequency] = useState<Frequency>(parsed.frequency)
  const [days, setDays] = useState<number[]>(parsed.days)
  const [time, setTime] = useState(`${String(parsed.hour).padStart(2, '0')}:${String(parsed.minute).padStart(2, '0')}`)
  const valid = frequency === 'daily' || days.length > 0

  useEffect(() => {
    const next = parseCron(value)
    setFrequency(next.frequency)
    setDays(next.days)
    setTime(`${String(next.hour).padStart(2, '0')}:${String(next.minute).padStart(2, '0')}`)
    if (!next.supported) onChange('0 9 * * *')
  }, [onChange, value])

  useEffect(() => {
    onValidityChange?.(valid)
  }, [onValidityChange, valid])

  function emit(nextFrequency: Frequency, nextDays: number[], nextTime: string) {
    const cron = toCron(nextFrequency, nextDays, nextTime)
    if (cron) onChange(cron)
  }

  function handleFrequencyChange(values: string[]) {
    onInteraction?.()
    const nextFrequency = (values[0] || frequency) as Frequency
    const nextDays = nextFrequency === 'weekly' && days.length === 0 ? [1] : days
    setFrequency(nextFrequency)
    setDays(nextDays)
    emit(nextFrequency, nextDays, time)
  }

  function handleDaysChange(values: string[]) {
    onInteraction?.()
    const nextDays = values.map(Number)
    setDays(nextDays)
    emit(frequency, nextDays, time)
  }

  function handleTimeChange(nextTime: string) {
    onInteraction?.()
    setTime(nextTime)
    emit(frequency, days, nextTime)
  }

  const dayLabels = WEEK_DAYS
    .filter(({ value }) => days.includes(value))
    .map(({ shortLabel }) => shortLabel)

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card">
      <div className="space-y-4 p-3 sm:p-4">
        <ToggleGroup
          value={[frequency]}
          onValueChange={handleFrequencyChange}
          variant="outline"
          size="sm"
          spacing={0}
          className="grid w-full grid-cols-2"
          aria-label="执行频率"
        >
          <ToggleGroupItem value="daily" className="w-full">每天</ToggleGroupItem>
          <ToggleGroupItem value="weekly" className="w-full">每周</ToggleGroupItem>
        </ToggleGroup>

        {frequency === 'weekly' && (
          <ToggleGroup
            value={days.map(String)}
            onValueChange={handleDaysChange}
            multiple
            variant="outline"
            size="sm"
            spacing={1}
            className="grid w-full grid-cols-7"
            aria-label="每周执行日期"
          >
            {WEEK_DAYS.map((day) => (
              <ToggleGroupItem
                key={day.value}
                value={String(day.value)}
                aria-label={day.label}
                className="h-9 min-w-0 px-0"
              >
                <span className="hidden sm:inline">{day.label}</span>
                <span className="sm:hidden">{day.shortLabel}</span>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        )}

        <div className="flex items-center justify-between gap-3">
          <label htmlFor={timePickerId} className="text-sm font-medium text-foreground">执行时间</label>
          <TimePicker id={timePickerId} value={time} onChange={handleTimeChange} />
        </div>

        {valid ? (
          <p className="text-sm text-muted-foreground">
            {frequency === 'daily'
              ? `每天 ${time} 自动执行`
              : `每周${dayLabels.join('、')} ${time} 自动执行`}
          </p>
        ) : (
          <p role="alert" className="text-sm font-medium text-destructive">请至少选择一天</p>
        )}
      </div>
      {footer ? <div className="border-t border-border bg-muted/20 p-3 sm:px-4">{footer}</div> : null}
    </div>
  )
}
