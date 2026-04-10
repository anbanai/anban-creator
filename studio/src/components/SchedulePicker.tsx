import { useState, useEffect } from 'react'

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

  useEffect(() => {
    const [h, m] = time.split(':').map(Number)
    onChange(toCron(frequency, days, h || 0, m || 0))
  }, [frequency, days, time])

  function toggleDay(day: number) {
    setDays(prev =>
      prev.includes(day) ? prev.filter(d => d !== day) : [...prev, day]
    )
  }

  const dayLabels = days
    .sort((a, b) => a - b)
    .map(d => WEEK_DAYS.find(w => w.value === d)?.label)
    .filter(Boolean)

  return (
    <div className="space-y-3">
      {/* 频率选择 */}
      <div className="flex gap-2">
        <button
          type="button"
          className={`rounded-lg px-3 py-1.5 text-sm transition-colors ${
            frequency === 'daily' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-foreground hover:bg-accent'
          }`}
          onClick={() => { setFrequency('daily'); setDays([]) }}
        >
          每天
        </button>
        <button
          type="button"
          className={`rounded-lg px-3 py-1.5 text-sm transition-colors ${
            frequency === 'weekly' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-foreground hover:bg-accent'
          }`}
          onClick={() => setFrequency('weekly')}
        >
          每周
        </button>
      </div>

      {/* 星期选择（仅每周模式） */}
      {frequency === 'weekly' && (
        <div className="flex flex-wrap gap-1.5">
          {WEEK_DAYS.map(day => (
            <button
              key={day.value}
              type="button"
              className={`rounded-md px-2.5 py-1 text-xs transition-colors ${
                days.includes(day.value)
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-secondary text-muted-foreground hover:bg-accent'
              }`}
              onClick={() => toggleDay(day.value)}
            >
              {day.label}
            </button>
          ))}
        </div>
      )}

      {/* 时间选择 */}
      <div className="flex items-center gap-2">
        <label className="text-sm text-muted-foreground">时间</label>
        <input
          type="time"
          value={time}
          onChange={e => setTime(e.target.value)}
          className="rounded-lg border border-input bg-secondary px-3 py-1.5 text-sm text-foreground focus:border-ring focus:outline-none"
        />
      </div>

      {/* 预览 */}
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
