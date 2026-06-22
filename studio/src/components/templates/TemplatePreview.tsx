import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import type { Template, TemplateType } from '@/types'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Loader2, ImageIcon, Pencil, Trash2 } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
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
import { cn } from '@/lib/utils'
import { SignedImage } from '@/components/ui/SignedImage'

const typeBadgeMap: Record<TemplateType, { label: string; className: string }> = {
  poster: { label: '海报', className: 'bg-blue-500/10 text-blue-400 ring-1 ring-blue-500/20' },
  seednote: { label: '种草笔记', className: 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20' },
  article: { label: '公众号', className: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20' },
}

function JsonDisplay({ data, title }: { data: Record<string, unknown>; title: string }) {
  const [expanded, setExpanded] = useState(false)

  if (!data || Object.keys(data).length === 0) return null

  return (
    <div className="rounded-lg border border-border">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/50"
      >
        <span>{title}</span>
        <span className="text-[10px]">{expanded ? '收起' : '展开'}</span>
      </button>
      {expanded && (
        <div className="border-t border-border px-3 py-2">
          <pre className="max-h-48 overflow-auto text-xs text-muted-foreground whitespace-pre-wrap break-words">
            {JSON.stringify(data, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}

// Scaffold fields (structure / example_content) are stored as { text: <markdown> }.
// Extract the text for readable display; returns '' when absent (legacy rows or
// never-set), in which case the caller falls back to the raw JSON expand.
function scaffoldText(value: Record<string, unknown> | undefined): string {
  if (!value) return ''
  const text = value.text
  return typeof text === 'string' ? text : ''
}

// Readable scaffold block: renders the .text as preformatted text. Used for the
// writing_style / structure / example channels that the agent consumes.
function ScaffoldBlock({ label, text }: { label: string; text: string }) {
  if (!text.trim()) return null
  return (
    <div className="rounded-lg border border-border px-3 py-2">
      <p className="text-xs font-medium text-muted-foreground mb-1">{label}</p>
      <p className="text-sm text-foreground whitespace-pre-wrap break-words">{text}</p>
    </div>
  )
}

interface TemplatePreviewProps {
  template: Template | null
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Current user id — when it matches template.user_id, edit/delete buttons are shown. */
  currentUserId?: string
  /** Called when user clicks "edit". Parent typically closes preview and opens the edit dialog. */
  onEdit?: (template: Template) => void
}

export function TemplatePreview({ template, open, onOpenChange, currentUserId, onEdit }: TemplatePreviewProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [fullTemplate, setFullTemplate] = useState<Template | null>(null)
  const [loading, setLoading] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.templates.remove(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
      toast.success('模板已删除')
      setDeleteOpen(false)
      onOpenChange(false)
    },
    onError: () => toast.error('删除失败，请重试'),
  })

  useEffect(() => {
    if (!open || !template) {
      setFullTemplate(null)
      return
    }

    // Fetch full template details if we only have summary data
    if (template.structure && Object.keys(template.structure).length > 0) {
      setFullTemplate(template)
      return
    }

    setLoading(true)
    api.templates
      .get(template.id)
      .then(setFullTemplate)
      .catch(() => setFullTemplate(template))
      .finally(() => setLoading(false))
  }, [open, template])

  if (!template) return null

  const data = fullTemplate ?? template
  const typeInfo = typeBadgeMap[data.type]
  const isOwner = !!currentUserId && !!data.user_id && data.user_id === currentUserId

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {data.name}
            <Badge className={cn('text-[11px]', typeInfo.className)}>
              {typeInfo.label}
            </Badge>
          </DialogTitle>
          {data.category && (
            <DialogDescription>分类：{data.category}</DialogDescription>
          )}
        </DialogHeader>

        {/* Body: 左右两栏 */}
        <div className="flex flex-col gap-4 sm:flex-row">
          {/* Left: image */}
          <div className="sm:w-48 sm:flex-shrink-0">
            <div className="relative aspect-[3/4] w-full overflow-hidden rounded-lg border border-border bg-muted">
              {loading ? (
                <div className="flex h-full w-full items-center justify-center">
                  <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              ) : data.thumbnail_url ? (
                <SignedImage
                  src={data.thumbnail_url}
                  alt={data.name}
                  className="h-full w-full object-cover"
                  showLoading={false}
                />
              ) : (
                <div className="flex h-full w-full items-center justify-center">
                  <ImageIcon className="h-10 w-10 text-muted-foreground/40" />
                </div>
              )}
            </div>
          </div>

          {/* Right: text content */}
          <div className="flex-1 space-y-3">
            {/* Tags */}
            {(data.tags ?? []).length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {(data.tags ?? []).map((tag) => (
                  <Badge key={tag} variant="secondary" className="text-xs">
                    {tag}
                  </Badge>
                ))}
              </div>
            )}

            {/* Style prompt (视觉风格) */}
            {data.style_prompt && (
              <div className="rounded-lg border border-border px-3 py-2">
                <p className="text-xs font-medium text-muted-foreground mb-1">视觉风格</p>
                <p className="text-sm text-foreground whitespace-pre-wrap">{data.style_prompt}</p>
              </div>
            )}

            {/* 写作风格 (内容脚手架) */}
            <ScaffoldBlock label="写作风格" text={data.writing_style ?? ''} />

            {/* 内容结构 —— 优先渲染 .text，无 .text 时回退到 JSON 展开（poster 旧结构） */}
            {scaffoldText(data.structure) ? (
              <ScaffoldBlock label="内容结构" text={scaffoldText(data.structure)} />
            ) : (
              data.structure &&
              Object.keys(data.structure).length > 0 && (
                <JsonDisplay data={data.structure} title="模板结构" />
              )
            )}

            {/* 示例内容 —— 同上 */}
            {scaffoldText(data.example_content) ? (
              <ScaffoldBlock label="示例内容" text={scaffoldText(data.example_content)} />
            ) : (
              data.example_content &&
              Object.keys(data.example_content).length > 0 && (
                <JsonDisplay data={data.example_content} title="示例内容" />
              )
            )}

            {/* Action buttons */}
            <div className="flex flex-wrap gap-2 pt-2">
              {(data.type === 'poster' || data.type === 'article') && (
                <Button size="sm" onClick={() => { onOpenChange(false); navigate(`/tasks?create=true&type=article&template_id=${data.id}`) }}>
                  用于公众号
                </Button>
              )}
              {(data.type === 'seednote' || data.type === 'poster') && (
                <Button size="sm" variant="outline" onClick={() => { onOpenChange(false); navigate(`/tasks?create=true&type=seednote&template_id=${data.id}`) }}>
                  用于种草笔记
                </Button>
              )}
              {isOwner && onEdit && (
                <Button size="sm" variant="outline" onClick={() => onEdit(data)}>
                  <Pencil className="h-3.5 w-3.5" />
                  编辑
                </Button>
              )}
              {isOwner && (
                <Button size="sm" variant="outline" onClick={() => setDeleteOpen(true)} className="text-destructive hover:bg-destructive/5">
                  <Trash2 className="h-3.5 w-3.5" />
                  删除
                </Button>
              )}
            </div>
          </div>
        </div>
      </DialogContent>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除模板？</AlertDialogTitle>
            <AlertDialogDescription>
              确定删除"{data.name}"吗？此操作无法撤销。已使用该模板创建的任务不受影响。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                deleteMutation.mutate(data.id)
              }}
              disabled={deleteMutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleteMutation.isPending ? '删除中…' : '删除'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  )
}
