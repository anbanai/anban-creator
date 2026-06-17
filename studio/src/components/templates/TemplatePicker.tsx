import { useQuery } from '@tanstack/react-query'
import { LayoutTemplate, Loader2, Inbox, Check } from 'lucide-react'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { Template, TemplateType } from '@/types'
import { Badge } from '@/components/ui/badge'
import {
  Carousel,
  CarouselContent,
  CarouselItem,
  CarouselPrevious,
  CarouselNext,
} from '@/components/ui/carousel'
import { SignedImage } from '@/components/ui/SignedImage'

interface TemplatePickerProps {
  /** Restrict picker to a single template type (e.g. matching the surrounding task/plan type). */
  type: TemplateType
  /** Currently selected template (or null). Controls only the active-card highlight. */
  selected: Template | null
  /** Called when user picks a template (clicking the active card again also fires this). */
  onSelect: (template: Template) => void
  /** Disable the picker (e.g. when surrounding type is viral_analysis). */
  disabled?: boolean
}

export function TemplatePicker({ type, selected, onSelect, disabled }: TemplatePickerProps) {
  const { data, isLoading } = useQuery({
    queryKey: queryKeys.templates.list({ type, scope: 'all' }),
    queryFn: () =>
      api.templates.list({
        type,
        scope: 'all',
        limit: 50,
      }),
  })

  const templates = data?.items ?? []

  return (
    <div className="rounded-md border border-border bg-card p-2">
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <LayoutTemplate className="h-3.5 w-3.5 text-muted-foreground" />
          <p className="text-xs font-medium text-foreground">选择模板覆盖账号风格</p>
        </div>
        {templates.length > 0 && (
          <span className="text-[11px] text-muted-foreground">{templates.length} 个</span>
        )}
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : templates.length === 0 ? (
        <div className="flex flex-col items-center gap-1 px-3 py-6 text-center">
          <Inbox className="h-6 w-6 text-muted-foreground/40" />
          <p className="text-xs text-muted-foreground">暂无此类型的模板</p>
          <p className="text-[11px] text-muted-foreground/70">可以到「模板库」页面新建一个</p>
        </div>
      ) : (
        <div className="relative">
          <Carousel
            opts={{ align: 'start', dragFree: true, containScroll: 'trimSnaps' }}
            className="w-full"
          >
            <CarouselContent className="-ml-2">
              {templates.map((t) => {
                const isActive = selected?.id === t.id
                return (
                <CarouselItem key={t.id} className="basis-[96px] pl-2">
                  <button
                    type="button"
                    disabled={disabled}
                    aria-pressed={isActive}
                    onClick={() => onSelect(t)}
                    className={`group flex w-full flex-col gap-1.5 rounded-md border bg-background p-1.5 text-left transition-all hover:shadow-sm disabled:cursor-not-allowed disabled:opacity-50 ${
                      isActive
                        ? 'border-primary ring-2 ring-primary/30'
                        : 'border-border hover:border-primary/40'
                    }`}
                  >
                    <div className="relative aspect-[3/4] w-full overflow-hidden rounded bg-muted">
                      {t.thumbnail_url ? (
                        <SignedImage
                          src={t.thumbnail_url}
                          alt={t.name}
                          className="h-full w-full object-cover transition-transform duration-200 group-hover:scale-105"
                          fallbackIcon={<LayoutTemplate className="h-5 w-5 text-muted-foreground/50" />}
                          fallbackClassName="h-full w-full flex items-center justify-center"
                          showLoading={false}
                        />
                      ) : (
                        <div className="flex h-full w-full items-center justify-center">
                          <LayoutTemplate className="h-5 w-5 text-muted-foreground/50" />
                        </div>
                      )}
                      {isActive && (
                        <span className="absolute right-1 top-1 flex h-4 w-4 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-sm">
                          <Check className="h-2.5 w-2.5" />
                        </span>
                      )}
                      {t.visibility === 'private' && !isActive && (
                        <Badge variant="secondary" className="absolute right-1 top-1 px-1 text-[9px]">
                          私
                        </Badge>
                      )}
                    </div>
                    <p className={`truncate text-[11px] font-medium ${isActive ? 'text-primary' : 'text-foreground'}`}>{t.name}</p>
                  </button>
                </CarouselItem>
                )
              })}
            </CarouselContent>
            <CarouselPrevious className="-left-1 top-1/2 h-6 w-6 shadow-sm" />
            <CarouselNext className="-right-1 top-1/2 h-6 w-6 shadow-sm" />
          </Carousel>
        </div>
      )}
    </div>
  )
}
