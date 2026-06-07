import { useQuery } from '@tanstack/react-query'
import { Clock, ImageIcon } from 'lucide-react'
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
  selectedId?: string
}

export default function HistoryDrawer({ open, onOpenChange, onSelect, selectedId }: HistoryDrawerProps) {
  const { data, isLoading } = useQuery({
    queryKey: ['designer', 'history'],
    queryFn: () => api.designer.getHistory({ page_size: 50 }),
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
      <SheetContent side="right" className="w-80 p-0 sm:max-w-md">
        <SheetHeader className="px-4 pt-4">
          <SheetTitle>历史记录</SheetTitle>
        </SheetHeader>
        <ScrollArea className="flex-1">
          {isLoading ? (
            <div className="space-y-2 p-4">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-14 w-full rounded-md" />
              ))}
            </div>
          ) : items.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-4 py-8 text-center">
              <Clock className="h-5 w-5 text-muted-foreground" />
              <p className="text-xs text-muted-foreground">暂无历史记录</p>
            </div>
          ) : (
            <div className="space-y-1 p-3">
              {items.map((gen) => {
                const thumbnail = getThumbnail(gen)
                const isSelected = selectedId === gen.id
                return (
                  <button
                    key={gen.id}
                    type="button"
                    onClick={() => {
                      onSelect(gen)
                      onOpenChange(false)
                    }}
                    className={`flex w-full items-start gap-2 rounded-md px-2 py-2 text-left transition-colors ${
                      isSelected
                        ? 'bg-primary/10 text-foreground'
                        : 'text-foreground hover:bg-accent/50'
                    }`}
                  >
                    <div className="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted">
                      {thumbnail ? (
                        <img src={thumbnail} alt="" className="h-full w-full object-cover" />
                      ) : (
                        <ImageIcon className="h-4 w-4 text-muted-foreground" />
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="line-clamp-2 text-xs leading-relaxed text-foreground">
                        {gen.prompt}
                      </p>
                      <p className="mt-0.5 text-[10px] text-muted-foreground">
                        {formatTime(gen.created_at)}
                      </p>
                    </div>
                  </button>
                )
              })}
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
