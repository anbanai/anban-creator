import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ImageOff, Loader2, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

import EmptyState from '@/components/EmptyState'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import {
  SEEDNOTE_TEMPLATE_CATEGORIES,
  type SeednoteTemplateCategory,
  type Template,
  type TemplateScope,
} from '@/types'
import { TemplateCreateDialog } from './TemplateCreateDialog'

const scopeOptions: Array<{ value: TemplateScope; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'public', label: '公开' },
  { value: 'private', label: '私有' },
  { value: 'inactive', label: '停用' },
]

export function TemplateGrid() {
  const queryClient = useQueryClient()
  const [scope, setScope] = useState<TemplateScope>('all')
  const [category, setCategory] = useState<SeednoteTemplateCategory | ''>('')
  const [editingTemplate, setEditingTemplate] = useState<Template | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deletingTemplate, setDeletingTemplate] = useState<Template | null>(null)
  const [failedImages, setFailedImages] = useState<Set<string>>(() => new Set())

  const templatesQuery = useQuery({
    queryKey: queryKeys.templates.list({ type: 'seednote', category: category || undefined, scope }),
    queryFn: () => api.templates.list({
      type: 'seednote',
      category: category || undefined,
      scope,
      limit: 100,
    }),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.templates.remove(id),
  })

  const openCreate = () => {
    setEditingTemplate(null)
    setDialogOpen(true)
  }

  const openEdit = (template: Template) => {
    setEditingTemplate(template)
    setDialogOpen(true)
  }

  const confirmDelete = async () => {
    if (!deletingTemplate) return
    try {
      await deleteMutation.mutateAsync(deletingTemplate.id)
      setDeletingTemplate(null)
      await queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
      toast.success('模板已删除')
    } catch (error) {
      toast.error(getApiErrorMessage(error, '删除模板失败，请重试'))
    }
  }

  const templates = templatesQuery.data?.items ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex gap-1 rounded-md border border-border bg-muted p-1" aria-label="模板状态">
          {scopeOptions.map((option) => (
            <Button
              key={option.value}
              type="button"
              size="sm"
              variant={scope === option.value ? 'default' : 'ghost'}
              onClick={() => setScope(option.value)}
            >
              {option.label}
            </Button>
          ))}
        </div>
        <Button type="button" size="sm" onClick={openCreate}>
          <Plus />
          新建模板
        </Button>
      </div>

      <div className="overflow-x-auto pb-1" aria-label="行业分类">
        <div className="flex min-w-max gap-2">
          <Button
            type="button"
            size="sm"
            variant={category === '' ? 'secondary' : 'outline'}
            onClick={() => setCategory('')}
          >
            全部行业
          </Button>
          {SEEDNOTE_TEMPLATE_CATEGORIES.map((value) => (
            <Button
              key={value}
              type="button"
              size="sm"
              variant={category === value ? 'secondary' : 'outline'}
              onClick={() => setCategory(value)}
            >
              {value}
            </Button>
          ))}
        </div>
      </div>

      {templatesQuery.isLoading ? (
        <div className="flex min-h-64 items-center justify-center" aria-label="正在加载模板">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      ) : templatesQuery.isError ? (
        <div className="flex min-h-64 flex-col items-center justify-center gap-3 text-center">
          <p className="text-sm text-muted-foreground">模板加载失败</p>
          <Button type="button" variant="outline" size="sm" onClick={() => void templatesQuery.refetch()}>
            <RefreshCw />
            重试
          </Button>
        </div>
      ) : templates.length === 0 ? (
        <EmptyState
          icon={ImageOff}
          title="暂无模板"
          description="当前筛选条件下没有模板。"
        />
      ) : (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
          {templates.map((template) => {
            const imageFailed = failedImages.has(template.id)
            return (
              <article key={template.id} className="overflow-hidden rounded-md border border-border bg-card">
                <div className="relative aspect-[3/4] bg-muted">
                  {imageFailed ? (
                    <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
                      <ImageOff className="size-6" />
                      <span className="text-xs">图片加载失败</span>
                    </div>
                  ) : (
                    <img
                      src={template.thumbnail_url}
                      alt={template.name}
                      className="h-full w-full object-cover"
                      onError={() => setFailedImages((current) => new Set(current).add(template.id))}
                    />
                  )}
                  <div className="absolute left-2 top-2 flex flex-wrap gap-1">
                    <Badge variant={template.visibility === 'public' ? 'secondary' : 'outline'}>
                      {template.visibility === 'public' ? '公开' : '私有'}
                    </Badge>
                    {!template.is_active ? <Badge variant="destructive">已停用</Badge> : null}
                  </div>
                </div>
                <div className="space-y-2 p-3">
                  <div className="min-w-0">
                    <h3 className="line-clamp-2 min-h-10 text-sm font-medium leading-5">{template.name}</h3>
                    <div className="mt-1 flex items-center justify-between gap-2 text-xs text-muted-foreground">
                      <span className="truncate">{template.category}</span>
                      <span className="shrink-0">排序 {template.sort_order}</span>
                    </div>
                  </div>
                  <div className="flex justify-end gap-1 border-t border-border pt-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`编辑 ${template.name}`}
                      title="编辑模板"
                      onClick={() => openEdit(template)}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`删除 ${template.name}`}
                      title="删除模板"
                      onClick={() => setDeletingTemplate(template)}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      )}

      <TemplateCreateDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        template={editingTemplate}
      />

      <AlertDialog
        open={Boolean(deletingTemplate)}
        onOpenChange={(open) => { if (!open) setDeletingTemplate(null) }}
      >
        <AlertDialogContent size="sm">
          <AlertDialogHeader>
            <AlertDialogTitle>删除模板？</AlertDialogTitle>
            <AlertDialogDescription>
              删除“{deletingTemplate?.name}”后无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deleteMutation.isPending}
              onClick={() => void confirmDelete()}
            >
              {deleteMutation.isPending ? <Loader2 className="animate-spin" /> : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
