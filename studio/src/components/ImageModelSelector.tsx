import { useMemo, useState } from 'react'
import { Check, ChevronsUpDown, Crown, Sparkles, Wand2 } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Badge } from '@/components/ui/badge'
import type { ImageModelOption } from '@/types/imageModel'
import { cn } from '@/lib/utils'

interface ImageModelSelectorProps {
  options: ImageModelOption[]
  value: string
  onChange: (value: string) => void
  className?: string
  disabled?: boolean
}

function providerLabel(provider: string): string {
  switch (provider) {
    case 'gemini':
      return 'Gemini'
    case 'volcengine':
    case 'volc':
    case 'seedream':
      return '豆包'
    case 'openai':
      return 'GPT Image'
    default:
      return provider
  }
}

function TierBadge({ tier, className }: { tier?: string; className?: string }) {
  if (!tier || tier === 'free') return null
  const isEnterprise = tier === 'enterprise'
  return (
    <Badge
      variant="secondary"
      className={cn('h-4 gap-0.5 px-1.5 text-[9px]', className)}
    >
      <Crown className="h-2.5 w-2.5" />
      {isEnterprise ? '企业' : 'Pro'}
    </Badge>
  )
}

export function ImageModelSelector({
  options,
  value,
  onChange,
  className,
  disabled,
}: ImageModelSelectorProps) {
  const [open, setOpen] = useState(false)

  const sorted = useMemo(() => {
    return [...options].sort((a, b) => {
      if (a.key === '') return -1
      if (b.key === '') return 1
      if (a.is_custom && !b.is_custom) return 1
      if (!a.is_custom && b.is_custom) return -1
      return a.display_name.localeCompare(b.display_name, 'zh-Hans-CN')
    })
  }, [options])

  const selected = sorted.find((opt) => opt.key === value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        render={
          <button
            type="button"
            role="combobox"
            aria-expanded={open}
            disabled={disabled}
            className={cn(
              'flex w-full items-center justify-between gap-2 rounded-xl border border-border/50 bg-muted/20 px-3 py-2.5 text-left text-sm outline-none transition-colors hover:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50',
              className,
            )}
          />
        }
      >
        <div className="flex min-w-0 items-center gap-2">
          <span className={cn('truncate font-medium', !selected && 'text-muted-foreground')}>
            {selected?.display_name ?? '系统默认'}
          </span>
          {selected && selected.key !== '' && !selected.is_custom && (
            <Badge variant="outline" className="h-4 shrink-0 gap-0.5 px-1.5 text-[9px] text-muted-foreground">
              <Wand2 className="h-2.5 w-2.5" />
              {providerLabel(selected.provider)}
            </Badge>
          )}
          {selected && <TierBadge tier={selected.min_tier} className="shrink-0" />}
          {selected?.is_custom && (
            <Badge variant="outline" className="h-4 shrink-0 gap-0.5 px-1.5 text-[9px] text-muted-foreground">
              <Sparkles className="h-2.5 w-2.5" />
              自定义
            </Badge>
          )}
        </div>
        <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent className="p-0 w-(--anchor-width)" align="start" sideOffset={4}>
        <Command
          filter={(v, search) => v.toLowerCase().includes(search.toLowerCase()) ? 1 : 0}
        >
          <div className="border-b border-border/50 px-1 pb-1">
            <CommandInput placeholder="搜索模型..." />
          </div>
          <CommandList>
            <CommandEmpty>没有找到模型</CommandEmpty>
            {sorted.map((opt, idx) => {
              const isSelected = opt.key === value
              return (
                <CommandItem
                  key={opt.key || `__system_default__${idx}`}
                  value={opt.display_name}
                  onSelect={() => {
                    onChange(opt.key)
                    setOpen(false)
                  }}
                  className="flex flex-col items-start gap-1 [&>svg:last-child]:hidden"
                >
                  <div className="flex items-center gap-2">
                    <Check
                      className={cn(
                        'h-3.5 w-3.5 shrink-0',
                        isSelected ? 'opacity-100' : 'opacity-0',
                      )}
                    />
                    <span className="text-[13px] font-medium">{opt.display_name}</span>
                    {opt.key !== '' && !opt.is_custom && (
                      <Badge variant="outline" className="h-4 gap-0.5 px-1.5 text-[9px] text-muted-foreground">
                        <Wand2 className="h-2.5 w-2.5" />
                        {providerLabel(opt.provider)}
                      </Badge>
                    )}
                    <TierBadge tier={opt.min_tier} />
                    {opt.is_custom && (
                      <Badge variant="outline" className="h-4 gap-0.5 px-1.5 text-[9px] text-muted-foreground">
                        <Sparkles className="h-2.5 w-2.5" />
                        自定义
                      </Badge>
                    )}
                  </div>
                  {opt.key === '' && (
                    <span className="ml-[22px] text-[10px] text-muted-foreground">
                      使用平台默认配置
                    </span>
                  )}
                </CommandItem>
              )
            })}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
