import { useQuery } from '@tanstack/react-query'
import { Clock, ImageIcon, RefreshCw } from 'lucide-react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { api } from '@/lib/api'
import type { ImageGeneration } from '@/types/designer'

interface HistoryDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (generation: ImageGeneration) => void
  onRegenerate?: (generation: ImageGeneration) => void
  selectedId?: string
  projectId: string
}

export default function HistoryDrawer({ open, onOpenChange, onSelect, onRegenerate, selectedId, projectId }: HistoryDrawerProps) {
  const { data, isLoading } = useQuery({
    queryKey: ['designer', 'history', projectId],
    queryFn: () => api.designer.getHistory({ project_id: projectId, page_size: 50 }),
    enabled: open,
  })

  const items = data?.items ?? []

  function formatTime(dateStr: string) {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffMin = Math.floor(diffMs / 60000)

    if (diffMin < 1) return '刚刚'
    if (diffMin < 60) return `${diffMin}分钟前`
    const diffHour = Math.floor(diffMin / 60)
    if (diffHour < 24) return `${diffHour}小时前`
    const diffDay = Math.floor(diffHour / 24)
    if (diffDay < 7) return `${diffDay}天前`
    return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
  }

  function getThumbnail(gen: ImageGeneration): string | undefined {
    return gen.results?.[0]?.image_url || gen.results?.[0]?.image_path
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-80 border-l-border/70 bg-popover p-0 sm:max-w-md">
        <SheetHeader className="border-b border-border/70 px-4 pt-4 pb-2">
          <SheetTitle className="text-sm font-medium">历史记录</SheetTitle>
        </SheetHeader>
        <ScrollArea className="flex-1 min-h-0">
          {isLoading ? (
            <div className="space-y-2 p-3">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-16 w-full rounded-xl" />
              ))}
            </div>
          ) : items.length === 0 ? (
            <div className="flex flex-col items-center gap-3 px-4 py-16 text-center">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted/50">
                <Clock className="h-4 w-4 text-muted-foreground" />
              </div>
              <p className="text-xs text-muted-foreground">暂无历史记录</p>
            </div>
          ) : (
            <div className="space-y-1 p-3">
              {items.map((gen) => {
                const thumbnail = getThumbnail(gen)
                const isSelected = selectedId === gen.id
                return (
                  <div
                    key={gen.id}
                    className={`group relative flex w-full items-start gap-3 rounded-xl px-3 py-2.5 text-left transition-all duration-200 ${
                      isSelected
                        ? 'bg-primary/10 text-primary ring-1 ring-primary/20'
                        : 'text-foreground hover:bg-accent/50'
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => {
                        onSelect(gen)
                        onOpenChange(false)
                      }}
                      className="flex min-w-0 flex-1 items-start gap-3 pr-7 text-left"
                    >
                      <div className="flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-muted/50">
                        {thumbnail ? (
                          <img src={thumbnail} alt="" className="h-full w-full object-cover" />
                        ) : (
                          <ImageIcon className="h-4 w-4 text-muted-foreground" />
                        )}
                      </div>
                      <div className="min-w-0 flex-1 pt-0.5">
                        <p className="line-clamp-2 text-xs leading-relaxed text-foreground/90">
                          {gen.prompt}
                        </p>
                        <p className="mt-1 text-[10px] text-muted-foreground/80">
                          {formatTime(gen.created_at)}
                          {gen.billing_status ? ` · ${gen.billing_status}` : ''}
                          {gen.final_cost ? ` · ${gen.final_cost}积分` : gen.estimated_cost ? ` · 预估${gen.estimated_cost}积分` : ''}
                        </p>
                      </div>
                    </button>
                    {onRegenerate && (
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation()
                          onRegenerate(gen)
                          onOpenChange(false)
                        }}
                        className="absolute right-2 top-2 flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover:opacity-100 max-sm:opacity-70"
                        title="重新生成"
                        aria-label="重新生成"
                      >
                        <RefreshCw className="h-3 w-3" />
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
