import { useId } from 'react'
import { cn } from '@/lib/utils'
import type { MontagePipelineCapability } from '@/types'

interface MontagePipelineSelectorProps {
  items: MontagePipelineCapability[]
  value?: string
  loading: boolean
  disabled: boolean
  error?: string
  compact?: boolean
  onChange: (capability: MontagePipelineCapability) => void
}

export function MontagePipelineSelector({
  items,
  value,
  loading,
  disabled,
  error,
  compact = false,
  onChange,
}: MontagePipelineSelectorProps) {
  const groupID = useId()

  return (
    <fieldset className="space-y-2" disabled={disabled}>
      <legend className="text-sm font-medium text-foreground">视频类型</legend>
      {loading ? (
        <div className="grid gap-2 sm:grid-cols-2" aria-label="正在加载视频类型">
          {Array.from({ length: 4 }).map((_, index) => (
            <div key={index} className={cn('animate-pulse rounded-lg border border-border bg-muted/40', compact ? 'h-24' : 'h-32')} />
          ))}
        </div>
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {items.map((capability) => {
            const selected = capability.key === value
            return (
              <label
                key={capability.key}
                className={cn(
                  'relative flex cursor-pointer flex-col gap-2 rounded-lg border p-3 text-left transition-colors',
                  compact ? 'min-h-24' : 'min-h-32',
                  'hover:border-foreground/30 hover:bg-muted/30 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring/50',
                  selected ? 'border-primary bg-primary/5' : 'border-border bg-background',
                )}
              >
                <input
                  type="radio"
                  name={`montage-pipeline-${groupID}`}
                  value={capability.key}
                  checked={selected}
                  onChange={() => onChange(capability)}
                  className="sr-only"
                />
                <span className="flex items-start justify-between gap-3">
                  <span>
                    <span className="block text-sm font-medium text-foreground">{capability.display_name}</span>
                    <span className="mt-0.5 block text-xs text-muted-foreground">{capability.description}</span>
                  </span>
                  <span
                    className={cn(
                      'mt-0.5 size-4 shrink-0 rounded-full border',
                      selected ? 'border-[5px] border-primary' : 'border-input',
                    )}
                    aria-hidden="true"
                  />
                </span>
                <span className="text-[11px] leading-4 text-muted-foreground">
                  适合：{capability.best_for.join('、')}
                </span>
                {!compact && (
                  <span className="mt-auto grid grid-cols-[2.5rem_1fr] gap-x-1 text-[11px] leading-4">
                    <span className="text-muted-foreground">素材</span>
                    <span className="text-foreground/80">{capability.source_hint}</span>
                    <span className="text-muted-foreground">产物</span>
                    <span className="text-foreground/80">{capability.output_hint}</span>
                  </span>
                )}
              </label>
            )
          })}
        </div>
      )}
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
    </fieldset>
  )
}
