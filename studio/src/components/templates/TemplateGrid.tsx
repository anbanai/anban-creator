import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search, Loader2, Inbox, Plus } from 'lucide-react'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { useAuth } from '@/contexts/AuthContext'
import type { Template, TemplateScope } from '@/types'
import { TemplateCard } from './TemplateCard'
import { TemplatePreview } from './TemplatePreview'
import { TemplateCreateDialog } from './TemplateCreateDialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import EmptyState from '@/components/EmptyState'

const typeOptions: { value: string; label: string }[] = [
  { value: 'all', label: '全部类型' },
  { value: 'poster', label: '海报' },
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
  { value: 'ecommerce', label: '电商出图' },
]

const scopeTabs: { value: TemplateScope; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'public', label: '公共' },
  { value: 'mine', label: '我的' },
]

interface TemplateGridProps {
  /** External filter overrides from parent page */
  type?: string
  category?: string
  tag?: string
}

export function TemplateGrid({ type, category, tag }: TemplateGridProps) {
  const { user } = useAuth()
  // Allow local filter state; external props take precedence
  const [localType, setLocalType] = useState('')
  const [localCategory, setLocalCategory] = useState('')
  const [localTag, setLocalTag] = useState('')
  const [scope, setScope] = useState<TemplateScope>('all')
  const [previewTemplate, setPreviewTemplate] = useState<Template | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [editingTemplate, setEditingTemplate] = useState<Template | null>(null)

  const filterType = type ?? localType
  const filterCategory = category ?? localCategory
  const filterTag = tag ?? localTag

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.templates.list({ type: filterType, category: filterCategory, tag: filterTag, scope }),
    queryFn: () =>
      api.templates.list({
        type: filterType || undefined,
        category: filterCategory || undefined,
        tag: filterTag || undefined,
        scope,
      }),
  })

  const templates = data?.items ?? []
  const total = data?.total ?? 0

  const openCreate = () => {
    setEditingTemplate(null)
    setCreateOpen(true)
  }

  const openEdit = (template: Template) => {
    setPreviewTemplate(null)
    setEditingTemplate(template)
    setCreateOpen(true)
  }

  return (
    <>
      {/* Scope tabs + new button */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex gap-1 overflow-x-auto rounded-lg border border-border bg-muted p-1">
          {scopeTabs.map((tab) => (
            <button
              key={tab.value}
              type="button"
              onClick={() => setScope(tab.value)}
              className={`whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                scope === tab.value
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        <Button onClick={openCreate} size="sm">
          <Plus className="h-4 w-4" />
          新建模板
        </Button>
      </div>

      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-3">
        <Select
          items={typeOptions}
          value={filterType || 'all'}
          onValueChange={(v) => {
            if (type === undefined) setLocalType(v === 'all' ? '' : (v ?? ''))
          }}
        >
          <SelectTrigger className="w-32">
            <SelectValue placeholder="全部类型" />
          </SelectTrigger>
          <SelectContent>
            {typeOptions.map((opt) => (
              <SelectItem key={opt.value} value={opt.value} label={opt.label}>
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
          description={
            scope === 'mine'
              ? '你还没有创建过模板，点击右上角"新建模板"开始吧。'
              : '当前筛选条件下没有找到模板，请尝试调整筛选条件。'
          }
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
        currentUserId={user?.id}
        onEdit={openEdit}
      />

      {/* Create / edit dialog */}
      <TemplateCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        template={editingTemplate}
      />
    </>
  )
}
