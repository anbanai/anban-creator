import { useMemo, useState } from 'react'
import { Check, ChevronsUpDown, Crown } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import type { ImageCapabilityOption } from '@/types/imageCapability'
import { cn } from '@/lib/utils'

interface ImageCapabilitySelectorProps {
  options: ImageCapabilityOption[]
  value: string
  onChange: (value: string) => void
  className?: string
  disabled?: boolean
}

function TierBadge({ tier }: { tier?: string }) {
  if (!tier || tier === 'free') return null
  return (
    <Badge variant="secondary" className="h-4 gap-0.5 px-1.5 text-[9px]">
      <Crown className="h-2.5 w-2.5" />
      {tier === 'enterprise' ? '企业' : 'Pro'}
    </Badge>
  )
}

export function ImageCapabilitySelector({ options, value, onChange, className, disabled }: ImageCapabilitySelectorProps) {
  const [open, setOpen] = useState(false)
  const sorted = useMemo(() => [...options].sort((left, right) => {
    if ((left.sort_order ?? 0) !== (right.sort_order ?? 0)) {
      if (!left.sort_order) return 1
      if (!right.sort_order) return -1
      return left.sort_order - right.sort_order
    }
    return left.display_name.localeCompare(right.display_name, 'zh-Hans-CN')
  }), [options])
  const selected = sorted.find((option) => option.key === value)
    ?? (value ? { key: value, display_name: '已停用能力' } : undefined)

  if (sorted.length === 0) return null

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        render={<button type="button" role="combobox" aria-expanded={open} disabled={disabled} className={cn('flex w-full items-center justify-between gap-2 rounded-md border border-border/60 bg-background px-3 py-2.5 text-left text-sm outline-none transition-colors hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50', className)} />}
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate font-medium">{selected?.display_name ?? '请选择图像能力'}</span>
          <TierBadge tier={selected?.min_tier} />
        </span>
        <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent className="w-(--anchor-width) p-0" align="start" sideOffset={4}>
        <Command>
          <div className="border-b border-border/50 px-1 pb-1"><CommandInput placeholder="搜索图像能力..." /></div>
          <CommandList>
            <CommandEmpty>没有找到图像能力</CommandEmpty>
            {sorted.map((option) => (
              <CommandItem
                key={option.key}
                value={`${option.display_name} ${option.description ?? ''}`}
                disabled={option.enabled !== true || option.price_available !== true}
                onSelect={() => {
                  if (option.enabled !== true || option.price_available !== true) return
                  onChange(option.key)
                  setOpen(false)
                }}
                className="flex flex-col items-start gap-1 [&>svg:last-child]:hidden"
              >
                <span className="flex items-center gap-2">
                  <Check className={cn('h-3.5 w-3.5 shrink-0', option.key === value ? 'opacity-100' : 'opacity-0')} />
                  <span className="text-[13px] font-medium">{option.display_name}</span>
                  <TierBadge tier={option.min_tier} />
                </span>
                {option.description ? <span className="ml-[22px] text-[11px] text-muted-foreground">{option.description}</span> : null}
                {option.price_available && typeof option.price_credits === 'number' ? (
                  <span className="ml-[22px] text-[11px] text-muted-foreground">每张 {option.price_credits.toLocaleString()} 积分</span>
                ) : <span className="ml-[22px] text-[11px] text-muted-foreground">价格暂不可用</span>}
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
