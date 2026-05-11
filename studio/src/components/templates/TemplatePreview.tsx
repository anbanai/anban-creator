import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import type { Template, TemplateType } from '@/types'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Loader2, ImageIcon } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

const typeBadgeMap: Record<TemplateType, { label: string; className: string }> = {
  poster: { label: '海报', className: 'bg-blue-500/10 text-blue-400 ring-1 ring-blue-500/20' },
  rednote: { label: '小红书', className: 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20' },
  article: { label: '公众号', className: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20' },
  xls: { label: '小绿书', className: 'bg-purple-500/10 text-purple-400 ring-1 ring-purple-500/20' },
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

interface TemplatePreviewProps {
  template: Template | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TemplatePreview({ template, open, onOpenChange }: TemplatePreviewProps) {
  const navigate = useNavigate()
  const [fullTemplate, setFullTemplate] = useState<Template | null>(null)
  const [loading, setLoading] = useState(false)

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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg max-h-[85vh] overflow-y-auto">
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

        {/* Preview image */}
        <div className="relative aspect-[3/4] w-full max-w-xs mx-auto overflow-hidden rounded-lg border border-border bg-muted">
          {loading ? (
            <div className="flex h-full w-full items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : data.thumbnail_url ? (
            <img
              src={data.thumbnail_url}
              alt={data.name}
              className="h-full w-full object-cover"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center">
              <ImageIcon className="h-10 w-10 text-muted-foreground/40" />
            </div>
          )}
        </div>

        {/* Tags */}
        {data.tags.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {data.tags.map((tag) => (
              <Badge key={tag} variant="secondary" className="text-xs">
                {tag}
              </Badge>
            ))}
          </div>
        )}

        {/* Style prompt */}
        {data.style_prompt && (
          <div className="rounded-lg border border-border px-3 py-2">
            <p className="text-xs font-medium text-muted-foreground mb-1">风格提示</p>
            <p className="text-sm text-foreground whitespace-pre-wrap">{data.style_prompt}</p>
          </div>
        )}

        {/* Structure */}
        {data.structure && Object.keys(data.structure).length > 0 && (
          <JsonDisplay data={data.structure} title="模板结构" />
        )}

        {/* Example content */}
        {data.example_content && Object.keys(data.example_content).length > 0 && (
          <JsonDisplay data={data.example_content} title="示例内容" />
        )}

        {/* Action buttons */}
        <div className="flex gap-2 pt-2">
          {(data.type === 'poster' || data.type === 'article' || data.type === 'xls') && (
            <Button size="sm" onClick={() => { onOpenChange(false); navigate('/tasks?create=true&type=article') }}>
              用于公众号
            </Button>
          )}
          {(data.type === 'rednote' || data.type === 'poster') && (
            <Button size="sm" variant="outline" onClick={() => { onOpenChange(false); navigate('/tasks?create=true&type=rednote') }}>
              用于小红书
            </Button>
          )}
          {data.type === 'xls' && (
            <Button size="sm" variant="outline" onClick={() => { onOpenChange(false); navigate('/tasks?create=true&type=xls') }}>
              用于小绿书
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
