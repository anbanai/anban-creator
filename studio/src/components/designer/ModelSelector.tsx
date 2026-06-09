import { useState } from 'react'
import { Check, ChevronsUpDown, Coins, Lock } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { getModelCapabilities } from '@/types/designer'
import type { DesignerProvider } from '@/types/designer'
import { cn } from '@/lib/utils'

interface ModelSelectorProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onChange: (id: string) => void
  className?: string
}

export default function ModelSelector({
  providers,
  selectedProviderId,
  onChange,
  className,
}: ModelSelectorProps) {
  const [open, setOpen] = useState(false)
  const selected = providers.find((p) => p.id === selectedProviderId && p.enabled)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <button
            role="combobox"
            aria-expanded={open}
            className={cn(
              'flex w-full items-center justify-between gap-2 rounded-xl border border-border/50 bg-muted/20 px-3 py-2.5 text-left text-sm outline-none transition-colors hover:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring/50',
              className,
            )}
          />
        }
      >
        <div className="flex min-w-0 items-center gap-2">
          <span className={cn('truncate font-medium', !selected && 'text-muted-foreground')}>
            {selected?.name ?? '选择模型...'}
          </span>
          {selected && selected.credits > 0 && (
            <Tooltip>
              <TooltipTrigger>
                <Badge variant="secondary" className="h-4 shrink-0 gap-0.5 px-1.5 text-[9px]">
                  <Coins className="h-2.5 w-2.5" />
                  {selected.credits}
                </Badge>
              </TooltipTrigger>
              <TooltipContent>每次生成消耗 {selected.credits} 积分</TooltipContent>
            </Tooltip>
          )}
        </div>
        <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent className="p-0 w-(--anchor-width)" align="start" sideOffset={4}>
        <Command
          filter={(value, search) =>
            value.toLowerCase().includes(search.toLowerCase()) ? 1 : 0
          }
        >
          <CommandInput placeholder="搜索模型..." />
          <CommandList>
            <CommandEmpty>没有找到模型</CommandEmpty>
            {providers.map((p) => {
              const isSelected = p.id === selectedProviderId && p.enabled
              const isDisabled = !p.enabled
              const caps = getModelCapabilities(p.provider, p.model)
              return (
                <CommandItem
                  key={p.id}
                  value={p.name}
                  disabled={isDisabled}
                  onSelect={() => {
                    if (!isDisabled) {
                      onChange(p.id)
                      setOpen(false)
                    }
                  }}
                  className="flex flex-col items-start [&>svg:last-child]:hidden"
                >
                  <div className="flex items-center gap-2">
                    <Check
                      className={cn(
                        'h-3.5 w-3.5 shrink-0',
                        isSelected ? 'opacity-100' : 'opacity-0',
                      )}
                    />
                    <span className="text-[13px] font-medium">{p.name}</span>
                    {isDisabled && (
                      <Badge
                        variant="outline"
                        className="h-4 gap-0.5 px-1.5 text-[9px] text-muted-foreground"
                      >
                        <Lock className="h-2.5 w-2.5" />
                        未启用
                      </Badge>
                    )}
                    {!isDisabled && p.credits > 0 && (
                      <Tooltip>
                        <TooltipTrigger>
                          <Badge variant="secondary" className="h-4 px-1.5 text-[9px]">
                            <Coins className="h-2.5 w-2.5" />
                            {p.credits}
                          </Badge>
                        </TooltipTrigger>
                        <TooltipContent>每次生成消耗 {p.credits} 积分</TooltipContent>
                      </Tooltip>
                    )}
                  </div>
                  {caps && !isDisabled && (caps.batch || caps.inpainting) && (
                    <div className="ml-[22px] flex gap-1">
                      {caps.batch && (
                        <Badge variant="secondary" className="h-4 px-1.5 text-[9px]">
                          批量
                        </Badge>
                      )}
                      {caps.inpainting && (
                        <Badge variant="secondary" className="h-4 px-1.5 text-[9px]">
                          局部编辑
                        </Badge>
                      )}
                    </div>
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
