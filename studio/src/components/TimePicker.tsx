import { useState, useMemo, useEffect, useRef } from 'react'
import { Clock } from 'lucide-react'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/Button'

interface TimePickerProps {
  value: string // HH:mm format
  onChange: (value: string) => void
  className?: string
  disabled?: boolean
}

const HOURS = Array.from({ length: 24 }, (_, i) => i)
const MINUTES = Array.from({ length: 12 }, (_, i) => i * 5)

function formatTime(hour: number, minute: number): string {
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

export default function TimePicker({ value, onChange, className, disabled }: TimePickerProps) {
  const [open, setOpen] = useState(false)

  // Snap non-5-minute values to nearest 5-minute interval
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange
  useEffect(() => {
    const raw = parseInt(value.split(':')[1]) || 0
    const snapped = Math.round(raw / 5) * 5
    if (snapped !== raw) {
      onChangeRef.current(formatTime(parseInt(value.split(':')[0]) || 0, snapped))
    }
  }, [value])

  const [hour, minute] = useMemo(() => {
    const parts = value.split(':').map(Number)
    return [parts[0] || 0, Math.round((parts[1] || 0) / 5) * 5]
  }, [value])

  const snapMinute = (m: number) => {
    // Snap to nearest 5-minute interval
    return Math.round(m / 5) * 5
  }

  const handleHourSelect = (h: number) => {
    onChange(formatTime(h, minute))
  }

  const handleMinuteSelect = (m: number) => {
    onChange(formatTime(hour, m))
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        render={
          <button
            className={cn(
              'flex h-8 items-center gap-1.5 rounded-lg border border-input bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors outline-none hover:bg-accent/50 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50',
              className
            )}
          />
        }
      >
        <Clock className="size-3.5 text-muted-foreground" />
        <span>{value || '00:00'}</span>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-2" align="start">
        <div className="flex gap-1">
          {/* Hours column */}
          <div className="flex flex-col gap-0.5">
            <div className="px-2 py-1 text-center text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              时
            </div>
            <div className="flex flex-col rounded-lg border border-border bg-muted/30 p-0.5 max-h-48 overflow-y-auto">
              {HOURS.map((h) => (
                <button
                  key={h}
                  type="button"
                  className={cn(
                    'rounded-md px-3 py-1 text-sm tabular-nums transition-colors',
                    h === hour
                      ? 'bg-primary text-primary-foreground font-medium'
                      : 'text-foreground hover:bg-accent'
                  )}
                  onClick={() => handleHourSelect(h)}
                >
                  {String(h).padStart(2, '0')}
                </button>
              ))}
            </div>
          </div>

          {/* Separator */}
          <div className="flex items-center pb-7">
            <span className="text-lg font-medium text-muted-foreground">:</span>
          </div>

          {/* Minutes column */}
          <div className="flex flex-col gap-0.5">
            <div className="px-2 py-1 text-center text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              分
            </div>
            <div className="flex flex-col rounded-lg border border-border bg-muted/30 p-0.5 max-h-48 overflow-y-auto">
              {MINUTES.map((m) => (
                <button
                  key={m}
                  type="button"
                  className={cn(
                    'rounded-md px-3 py-1 text-sm tabular-nums transition-colors',
                    m === snapMinute(minute)
                      ? 'bg-primary text-primary-foreground font-medium'
                      : 'text-foreground hover:bg-accent'
                  )}
                  onClick={() => handleMinuteSelect(m)}
                >
                  {String(m).padStart(2, '0')}
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* Quick time buttons */}
        <div className="mt-2 flex gap-1 border-t border-border pt-2">
          {[
            { label: '早上', time: '08:00' },
            { label: '中午', time: '12:00' },
            { label: '下午', time: '14:00' },
            { label: '晚上', time: '20:00' },
          ].map((preset) => (
            <Button
              key={preset.time}
              variant={value === preset.time ? 'default' : 'ghost'}
              size="xs"
              className="flex-1"
              onClick={() => onChange(preset.time)}
            >
              {preset.label}
            </Button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  )
}
