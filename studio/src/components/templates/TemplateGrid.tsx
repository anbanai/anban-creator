import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search, Loader2, Inbox } from 'lucide-react'
import { api } from '@/lib/api'
import type { Template } from '@/types'
import { TemplateCard } from './TemplateCard'
import { TemplatePreview } from './TemplatePreview'
import { Input } from '@/components/ui/input'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import EmptyState from '@/components/EmptyState'

const typeOptions: { value: string; label: string }[] = [
  { value: '', label: '全部类型' },
  { value: 'poster', label: '海报' },
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
  { value: 'xls', label: '小绿书' },
]

interface TemplateGridProps {
  /** External filter overrides from parent page */
  type?: string
  category?: string
  tag?: string
}

export function TemplateGrid({ type, category, tag }: TemplateGridProps) {
  // Allow local filter state; external props take precedence
  const [localType, setLocalType] = useState('')
  const [localCategory, setLocalCategory] = useState('')
  const [localTag, setLocalTag] = useState('')
  const [previewTemplate, setPreviewTemplate] = useState<Template | null>(null)

  const filterType = type ?? localType
  const filterCategory = category ?? localCategory
  const filterTag = tag ?? localTag

  const { data, isLoading } = useQuery({
    queryKey: ['templates', filterType, filterCategory, filterTag],
    queryFn: () =>
      api.templates.list({
        type: filterType || undefined,
        category: filterCategory || undefined,
        tag: filterTag || undefined,
      }),
  })

  const templates = data?.items ?? []
  const total = data?.total ?? 0

  return (
    <>
      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-3">
        <Select
          value={filterType || '_all'}
          onValueChange={(v) => {
            if (type === undefined) setLocalType(v === '_all' ? '' : (v ?? ''))
          }}
        >
          <SelectTrigger className="w-32">
            <SelectValue placeholder="全部类型" />
          </SelectTrigger>
          <SelectContent>
            {typeOptions.map((opt) => (
              <SelectItem key={opt.value || '_all'} value={opt.value || '_all'} label={opt.label}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="relative flex-1 min-w-[160px] max-w-xs">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="搜索分类..."
            value={filterCategory}
            onChange={(e) => {
              if (category === undefined) setLocalCategory(e.target.value)
            }}
            disabled={category !== undefined}
          />
        </div>

        <div className="relative flex-1 min-w-[160px] max-w-xs">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="搜索标签..."
            value={filterTag}
            onChange={(e) => {
              if (tag === undefined) setLocalTag(e.target.value)
            }}
            disabled={tag !== undefined}
          />
        </div>

        {total > 0 && (
          <span className="text-xs text-muted-foreground">
            共 {total} 个模板
          </span>
        )}
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      ) : templates.length === 0 ? (
        <EmptyState
          icon={Inbox}
          title="暂无模板"
          description="当前筛选条件下没有找到模板，请尝试调整筛选条件。"
        />
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {templates.map((template) => (
            <TemplateCard
              key={template.id}
              template={template}
              onClick={setPreviewTemplate}
            />
          ))}
        </div>
      )}

      {/* Preview dialog */}
      <TemplatePreview
        template={previewTemplate}
        open={!!previewTemplate}
        onOpenChange={(open) => {
          if (!open) setPreviewTemplate(null)
        }}
      />
    </>
  )
}
