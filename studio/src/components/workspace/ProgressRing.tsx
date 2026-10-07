import { RadialBar, RadialBarChart } from 'recharts'
import { cn } from '@/lib/utils'

export interface ProgressRingProps {
  value: number
  max?: number
  size?: number
  label?: string
  className?: string
}

function clamp(value: number, min: number, max: number) {
  if (!Number.isFinite(value)) return min
  return Math.min(max, Math.max(min, value))
}

export function ProgressRing({ value, max = 100, size = 76, label = '进度', className }: ProgressRingProps) {
  const safeMax = max > 0 ? max : 100
  const safeValue = clamp(value, 0, safeMax)
  const percent = Math.round((safeValue / safeMax) * 100)
  const strokeWidth = Math.max(6, Math.round(size / 10))
  return (
    <div
      role="progressbar"
      aria-label={`${label}：${percent}%`}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      data-progress={percent}
      data-motion="reduced-safe"
      className={cn('relative shrink-0', className)}
      style={{ width: size, height: size }}
    >
      <RadialBarChart
        width={size}
        height={size}
        cx="50%"
        cy="50%"
        innerRadius={size / 2 - strokeWidth}
        outerRadius={size / 2 - 1}
        barSize={strokeWidth}
        startAngle={90}
        endAngle={-270}
        data={[{ value: safeValue }]}
        aria-hidden="true"
      >
        <RadialBar
          dataKey="value"
          background={{ fill: 'var(--muted)' }}
          cornerRadius={strokeWidth / 2}
          fill="var(--primary)"
          isAnimationActive={false}
        />
      </RadialBarChart>
      <span aria-hidden="true" className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm font-semibold tabular-nums text-foreground">
        {percent}%
      </span>
      <span className="sr-only">{label}：{percent}%</span>
    </div>
  )
}
