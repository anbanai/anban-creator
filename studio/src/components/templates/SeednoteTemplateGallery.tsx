import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ImageOff, LayoutTemplate, RefreshCw } from 'lucide-react'

import { SignedImage } from '@/components/ui/SignedImage'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { cn } from '@/lib/utils'
import {
  SEEDNOTE_TEMPLATE_CATEGORIES,
  type ProjectPlatform,
  type SeednoteTemplateCategory,
  type Template,
} from '@/types'

const ALL_CATEGORY = '全部'
const CATEGORY_OPTIONS = [ALL_CATEGORY, ...SEEDNOTE_TEMPLATE_CATEGORIES] as const

interface SeednoteTemplateGalleryProps {
  platform?: ProjectPlatform | null
  onApply: (prompt: string, template: Template) => void
  className?: string
}

export function SeednoteTemplateGallery({ platform, onApply, className }: SeednoteTemplateGalleryProps) {
  const [category, setCategory] = useState<(typeof CATEGORY_OPTIONS)[number]>(ALL_CATEGORY)
  const selectedCategory = category === ALL_CATEGORY ? undefined : category
  const query = useQuery({
    queryKey: queryKeys.templates.list({ type: 'seednote', category: selectedCategory }),
    queryFn: () => api.templates.list({
      type: 'seednote',
      ...(selectedCategory ? { category: selectedCategory } : {}),
      limit: 100,
    }),
    enabled: platform === 'seednote',
  })

  if (platform !== 'seednote') return null

  const templates = query.data?.items ?? []

  return (
    <section className={cn('flex min-w-0 flex-col gap-3', className)} aria-labelledby="seednote-template-gallery-title">
      <div className="flex items-center justify-between gap-3">
        <h3 id="seednote-template-gallery-title" className="text-sm font-medium text-foreground">
          参考模板
        </h3>
        {templates.length > 0 ? (
          <span className="text-xs text-muted-foreground">{templates.length} 个</span>
        ) : null}
      </div>

      <div className="overflow-x-auto pb-1">
        <ToggleGroup
          aria-label="模板分类"
          value={[category]}
          onValueChange={(values) => {
            const next = values[0] as (typeof CATEGORY_OPTIONS)[number] | undefined
            if (next) setCategory(next)
          }}
          variant="outline"
          size="sm"
          spacing={2}
          className="w-max"
        >
          {CATEGORY_OPTIONS.map((option) => (
            <ToggleGroupItem key={option} value={option} aria-label={option} className="whitespace-nowrap">
              {option}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      {query.isLoading ? (
        <div aria-label="模板加载中" className="grid auto-cols-[132px] grid-flow-col gap-3 overflow-hidden sm:auto-cols-[156px]">
          {Array.from({ length: 5 }).map((_, index) => (
            <div key={index} className="flex flex-col gap-2">
              <Skeleton className="aspect-[3/4] w-full rounded-md" />
              <Skeleton className="h-4 w-4/5" />
            </div>
          ))}
        </div>
      ) : query.isError ? (
        <Empty className="min-h-44 border">
          <EmptyHeader>
            <EmptyMedia variant="icon"><RefreshCw /></EmptyMedia>
            <EmptyTitle>模板加载失败</EmptyTitle>
            <EmptyDescription>请检查网络后重试。</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button type="button" variant="outline" size="sm" onClick={() => void query.refetch()}>
              <RefreshCw data-icon="inline-start" />
              重新加载
            </Button>
          </EmptyContent>
        </Empty>
      ) : templates.length === 0 ? (
        <Empty className="min-h-44 border">
          <EmptyHeader>
            <EmptyMedia variant="icon"><LayoutTemplate /></EmptyMedia>
            <EmptyTitle>这个分类还没有模板</EmptyTitle>
            <EmptyDescription>模板由管理员创建后会显示在这里。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="grid auto-cols-[132px] grid-flow-col gap-3 overflow-x-auto overscroll-x-contain pb-2 sm:auto-cols-[156px]">
          {templates.map((template) => (
            <button
              key={template.id}
              type="button"
              className="group flex min-w-0 flex-col gap-2 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              aria-label={`${template.name}，使用此模板`}
              onClick={() => onApply(template.prompt, template)}
            >
              <span className="relative aspect-[3/4] w-full overflow-hidden rounded-md border border-border bg-muted">
                {template.thumbnail_url ? (
                  <SignedImage
                    src={template.thumbnail_url}
                    alt={template.name}
                    className="size-full object-cover transition-transform duration-200 group-hover:scale-[1.02]"
                    fallbackIcon={<ImageOff />}
                    fallbackClassName="text-muted-foreground"
                    showLoading={false}
                  />
                ) : (
                  <span className="flex size-full items-center justify-center text-muted-foreground">
                    <ImageOff />
                  </span>
                )}
              </span>
              <span className="line-clamp-2 min-h-10 text-sm font-medium leading-5 text-foreground">
                {template.name}
              </span>
            </button>
          ))}
        </div>
      )}
    </section>
  )
}

export type { SeednoteTemplateCategory }
