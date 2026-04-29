import * as React from 'react'
import { Check, ChevronsUpDown, X } from 'lucide-react'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

export interface ComboboxOption {
  value: string
  label: string
  group?: string
  icon?: React.ReactNode
}

interface ComboboxProps {
  options: ComboboxOption[]
  value: string
  onChange: (value: string) => void
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  className?: string
  triggerClassName?: string
  disabled?: boolean
}

export function Combobox({
  options,
  value,
  onChange,
  placeholder = '选择...',
  searchPlaceholder = '搜索...',
  emptyText = '没有找到',
  className,
  triggerClassName,
  disabled,
}: ComboboxProps) {
  const [open, setOpen] = React.useState(false)

  const selectedLabel = React.useMemo(
    () => options.find((o) => o.value === value)?.label,
    [options, value]
  )

  // Group options
  const grouped = React.useMemo(() => {
    const groups: { group: string; items: ComboboxOption[] }[] = []
    const ungrouped: ComboboxOption[] = []

    for (const opt of options) {
      if (opt.group) {
        const existing = groups.find((g) => g.group === opt.group)
        if (existing) {
          existing.items.push(opt)
        } else {
          groups.push({ group: opt.group, items: [opt] })
        }
      } else {
        ungrouped.push(opt)
      }
    }

    if (groups.length > 0 && ungrouped.length === 0) {
      return groups
    }

    if (ungrouped.length > 0) {
      return [{ group: '', items: ungrouped }, ...groups]
    }

    return groups
  }, [options])

  return (
    <div className={className}>
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        render={
          <button
            role="combobox"
            aria-expanded={open}
            className={cn(
              'flex h-8 w-full items-center justify-between gap-1.5 rounded-lg border border-input bg-transparent px-2.5 py-2 text-sm transition-colors outline-none select-none hover:bg-accent/50 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 dark:aria-invalid:border-destructive/50',
              triggerClassName
            )}
          />
        }
      >
        {selectedLabel ? (
          <span className="truncate">{selectedLabel}</span>
        ) : (
          <span className="text-muted-foreground truncate">{placeholder}</span>
        )}
        {value && (
          <button
            type="button"
            className="shrink-0 rounded-full p-0.5 hover:bg-muted-foreground/20"
            onClick={(e) => {
              e.stopPropagation()
              onChange('')
            }}
          >
            <X className="size-3 text-muted-foreground" />
          </button>
        )}
        <ChevronsUpDown className="pointer-events-none shrink-0 size-3.5 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent className="w-auto min-w-[--radix-popover-trigger-width] p-0" align="start">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyText}</CommandEmpty>
            {grouped.map(({ group, items }) => (
              <CommandGroup key={group || '_ungrouped'} heading={group || undefined}>
                {items.map((opt) => (
                  <CommandItem
                    key={opt.value}
                    value={`${opt.group ? `${opt.group} ` : ''}${opt.label} ${opt.value}`}
                    onSelect={() => {
                      onChange(opt.value)
                      setOpen(false)
                    }}
                  >
                    <Check
                      className={cn(
                        'mr-1 size-4 shrink-0',
                        value === opt.value ? 'opacity-100' : 'opacity-0'
                      )}
                    />
                    {opt.icon && <span className="shrink-0">{opt.icon}</span>}
                    <span className="truncate">{opt.label}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
    </div>
  )
}
