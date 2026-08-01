import { useState, useEffect } from 'react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import TimePicker from '@/components/TimePicker'

interface SchedulePickerProps {
  value: string          // cron expression, e.g. "0 9 * * 1,3,5"
  onChange: (cron: string) => void
  onInteraction?: () => void
}

type Frequency = 'daily' | 'weekly'

const WEEK_DAYS = [
  { value: 1, label: '周一' },
  { value: 2, label: '周二' },
  { value: 3, label: '周三' },
  { value: 4, label: '周四' },
  { value: 5, label: '周五' },
  { value: 6, label: '周六' },
  { value: 0, label: '周日' },
]

function parseCron(cron: string): { frequency: Frequency; days: number[]; hour: number; minute: number } {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return { frequency: 'daily', days: [], hour: 9, minute: 0 }
  const [min, hourStr, , , weekday] = parts
  const parsedHour = Number.parseInt(hourStr, 10)
  const parsedMinute = Number.parseInt(min, 10)
  const hour = Number.isNaN(parsedHour) ? 9 : parsedHour
  const minute = Number.isNaN(parsedMinute) ? 0 : parsedMinute
  if (weekday === '*') {
    return { frequency: 'daily', days: [], hour, minute }
  }
  return {
    frequency: 'weekly',
    days: weekday.split(',').map(Number).filter(n => !isNaN(n)),
    hour,
    minute,
  }
}

function toCron(frequency: Frequency, days: number[], hour: number, minute: number): string {
  if (frequency === 'daily') {
    return `${minute} ${hour} * * *`
  }
  const sortedDays = [...days].sort((a, b) => a - b)
  return `${minute} ${hour} * * ${sortedDays.join(',')}`
}

export default function SchedulePicker({ value, onChange, onInteraction }: SchedulePickerProps) {
  const parsed = parseCron(value)
  const [frequency, setFrequency] = useState<Frequency>(parsed.frequency)
  const [days, setDays] = useState<number[]>(parsed.days)
  const [time, setTime] = useState(`${String(parsed.hour).padStart(2, '0')}:${String(parsed.minute).padStart(2, '0')}`)

  useEffect(() => {
    const next = parseCron(value)
    setFrequency(next.frequency)
    setDays(next.days)
    setTime(`${String(next.hour).padStart(2, '0')}:${String(next.minute).padStart(2, '0')}`)
  }, [value])

  useEffect(() => {
    const [h, m] = time.split(':').map(Number)
    onChange(toCron(frequency, days, h || 0, m || 0))
  }, [frequency, days, time, onChange])

  function handleFrequencyChange(val: string[]) {
    onInteraction?.()
    const freq = (val[0] || 'daily') as Frequency
    setFrequency(freq)
    if (freq === 'daily') setDays([])
  }

  function handleDaysChange(val: string[]) {
    onInteraction?.()
    setDays(val.map(Number))
  }

  const dayLabels = [...days]
    .sort((a, b) => a - b)
    .map(d => WEEK_DAYS.find(w => w.value === d)?.label)
    .filter(Boolean)

  return (
    <div className="space-y-3">
      {/* Frequency selection */}
      <ToggleGroup
        value={[frequency]}
        onValueChange={handleFrequencyChange}
        variant="outline"
        size="sm"
        spacing={2}
      >
        <ToggleGroupItem value="daily">每天</ToggleGroupItem>
        <ToggleGroupItem value="weekly">每周</ToggleGroupItem>
      </ToggleGroup>

      {/* Day of week selection (weekly only) */}
      {frequency === 'weekly' && (
        <ToggleGroup
          value={days.map(String)}
          onValueChange={handleDaysChange}
        multiple
        variant="outline"
        size="sm"
        spacing={1}
        className="grid w-full grid-cols-4 sm:grid-cols-7"
      >
          {WEEK_DAYS.map(day => (
            <ToggleGroupItem key={day.value} value={String(day.value)}>
              {day.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      )}

      {/* Time selection */}
      <div className="flex items-center gap-2">
        <label className="text-sm text-muted-foreground">时间</label>
        <TimePicker value={time} onChange={(next) => { onInteraction?.(); setTime(next) }} />
      </div>

      {/* Preview */}
      <p className="text-xs text-muted-foreground">
        {frequency === 'daily'
          ? `每天 ${time} 自动执行`
          : dayLabels.length > 0
            ? `每${dayLabels.join('、')} ${time} 自动执行`
            : '请选择至少一天'
        }
      </p>
    </div>
  )
}
