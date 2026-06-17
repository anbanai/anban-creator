import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { LayoutTemplate, X, Loader2, Inbox } from 'lucide-react'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { Template, TemplateType } from '@/types'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'
import { SignedImage } from '@/components/ui/SignedImage'

interface TemplatePickerProps {
  /** Restrict picker to a single template type (e.g. matching the surrounding task/plan type). */
  type: TemplateType
  /** Currently selected template (or null). */
  selected: Template | null
  /** Called when user picks a template. */
  onSelect: (template: Template) => void
  /** Called when user clicks the clear button. */
  onClear: () => void
  /** Disable the trigger (e.g. when surrounding type is viral_analysis). */
  disabled?: boolean
}

export function TemplatePicker({ type, selected, onSelect, onClear, disabled }: TemplatePickerProps) {
  const [open, setOpen] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.templates.list({ type, scope: 'all' }),
    queryFn: () =>
      api.templates.list({
        type,
        scope: 'all',
        limit: 50,
      }),
    enabled: open,
  })

  const templates = data?.items ?? []

  if (selected) {
    return (
      <div className="flex items-center gap-2 rounded-md border border-primary/40 bg-primary/5 px-2.5 py-1.5">
        <LayoutTemplate className="h-3.5 w-3.5 text-primary" />
        <span className="text-xs font-medium text-foreground">{selected.name}</span>
        {selected.visibility === 'private' && (
          <Badge variant="secondary" className="text-[10px] px-1.5">
            私有
          </Badge>
        )}
        <button
          type="button"
          onClick={onClear}
          className="ml-1 rounded-sm p-0.5 text-muted-foreground hover:bg-foreground/5 hover:text-foreground"
          aria-label="清除模板"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
    )
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled}
            className="h-7 gap-1 px-2 text-xs"
          >
            <LayoutTemplate className="h-3.5 w-3.5" />
            选择模板
          </Button>
        }
      />
      <PopoverContent className="w-80 p-0" align="start">
        <div className="border-b border-border px-3 py-2">
          <p className="text-xs font-medium text-foreground">选择模板覆盖账号风格</p>
          <p className="mt-0.5 text-[11px] text-muted-foreground">
            选中后，将使用模板的参考图和风格，而非账号本身的风格。
          </p>
        </div>
        <div className="max-h-72 overflow-y-auto p-2">
          {isLoading ? (
            <div className="flex items-center justify-center py-6">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : templates.length === 0 ? (
            <div className="flex flex-col items-center gap-1 px-3 py-6 text-center">
              <Inbox className="h-6 w-6 text-muted-foreground/40" />
              <p className="text-xs text-muted-foreground">暂无此类型的模板</p>
              <p className="text-[11px] text-muted-foreground/70">
                可以到「模板库」页面新建一个
              </p>
            </div>
          ) : (
            <ul className="space-y-1">
              {templates.map((t) => (
                <li key={t.id}>
                  <button
                    type="button"
                    onClick={() => {
                      onSelect(t)
                      setOpen(false)
                    }}
                    className={cn(
                      'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-accent',
                    )}
                  >
                    {t.thumbnail_url ? (
                      <SignedImage
                        src={t.thumbnail_url}
                        alt={t.name}
                        className="h-9 w-9 shrink-0 rounded border border-border object-cover"
                        fallbackIcon={<LayoutTemplate className="h-4 w-4 text-muted-foreground/50" />}
                        fallbackClassName="h-9 w-9 shrink-0 rounded border border-border bg-muted"
                        showLoading={false}
                      />
                    ) : (
                      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded border border-border bg-muted">
                        <LayoutTemplate className="h-4 w-4 text-muted-foreground/50" />
                      </div>
                    )}
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-xs font-medium text-foreground">{t.name}</p>
                      {t.style_prompt && (
                        <p className="truncate text-[11px] text-muted-foreground">
                          {t.style_prompt}
                        </p>
                      )}
                    </div>
                    {t.visibility === 'private' && (
                      <Badge variant="secondary" className="text-[10px] px-1.5">
                        私有
                      </Badge>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}
