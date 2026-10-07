import type { ComponentType, SVGProps } from 'react'
import { Archive, CircleCheck, CirclePause, CirclePlay, CircleX, LoaderCircle } from 'lucide-react'
import { cn } from '@/lib/utils'

export type StatusPillStatus = 'active' | 'paused' | 'completed' | 'failed' | 'archived' | 'running' | 'pending' | 'default'

type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral'

const statusPresentation: Record<StatusPillStatus, { tone: Tone; icon: ComponentType<SVGProps<SVGSVGElement>> }> = {
  active: { tone: 'success', icon: CirclePlay },
  running: { tone: 'info', icon: LoaderCircle },
  completed: { tone: 'success', icon: CircleCheck },
  failed: { tone: 'danger', icon: CircleX },
  paused: { tone: 'warning', icon: CirclePause },
  pending: { tone: 'neutral', icon: LoaderCircle },
  archived: { tone: 'neutral', icon: Archive },
  default: { tone: 'neutral', icon: CircleCheck },
}

export interface StatusPillProps {
  status?: StatusPillStatus | string
  label: string
  tone?: Tone
  className?: string
}

export function StatusPill({ status = 'default', label, tone, className }: StatusPillProps) {
  const presentation = statusPresentation[status as StatusPillStatus] ?? statusPresentation.default
  const effectiveTone = tone ?? presentation.tone
  const Icon = presentation.icon
  return (
    <span
      data-testid="status-pill"
      data-status={status}
      data-tone={effectiveTone}
      className={cn(
        'inline-flex h-5 items-center gap-1 rounded-full border px-2 text-[11px] font-medium whitespace-nowrap',
        effectiveTone === 'success' && 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
        effectiveTone === 'warning' && 'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300',
        effectiveTone === 'danger' && 'border-destructive/25 bg-destructive/10 text-destructive',
        effectiveTone === 'info' && 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300',
        effectiveTone === 'neutral' && 'border-border bg-muted/50 text-muted-foreground',
        className,
      )}
    >
      <Icon aria-hidden="true" className={cn('size-3', status === 'running' || status === 'pending' ? 'animate-spin motion-reduce:animate-none' : '')} />
      <span>{label}</span>
    </span>
  )
}
