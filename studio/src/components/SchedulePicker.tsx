import { useState, useEffect, useRef } from 'react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import TimePicker from '@/components/TimePicker'

interface SchedulePickerProps {
  value: string          // cron expression, e.g. "0 9 * * 1,3,5"
  onChange: (cron: string) => void
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
  const hour = parseInt(hourStr) || 9
  const minute = parseInt(min) || 0
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

export default function SchedulePicker({ value, onChange }: SchedulePickerProps) {
  const parsed = parseCron(value)
  const [frequency, setFrequency] = useState<Frequency>(parsed.frequency)
  const [days, setDays] = useState<number[]>(parsed.days)
  const [time, setTime] = useState(`${String(parsed.hour).padStart(2, '0')}:${String(parsed.minute).padStart(2, '0')}`)
  const syncRef = useRef(false)

  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange

  useEffect(() => {
    const next = parseCron(value)
    const nextTime = `${String(next.hour).padStart(2, '0')}:${String(next.minute).padStart(2, '0')}`
    const sameDays = next.days.length === days.length && next.days.every((day, index) => day === days[index])

    if (frequency === next.frequency && sameDays && time === nextTime) {
      return
    }

    syncRef.current = true
    setFrequency(next.frequency)
    setDays(next.days)
    setTime(nextTime)
  }, [value])

  useEffect(() => {
    if (syncRef.current) {
      syncRef.current = false
      return
    }
    const [h, m] = time.split(':').map(Number)
    onChangeRef.current(toCron(frequency, days, h || 0, m || 0))
  }, [frequency, days, time])

  function handleFrequencyChange(val: string[]) {
    const freq = (val[0] || 'daily') as Frequency
    setFrequency(freq)
    if (freq === 'daily') setDays([])
  }

  function handleDaysChange(val: string[]) {
    setDays(val.map(Number))
  }

  const dayLabels = days
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
        <TimePicker value={time} onChange={setTime} />
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
