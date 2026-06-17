import { cn } from '@/lib/utils'
import type { Template, TemplateType } from '@/types'
import { Badge } from '@/components/ui/badge'
import { ImageIcon } from 'lucide-react'
import { SignedImage } from '@/components/ui/SignedImage'

const typeBadgeMap: Record<TemplateType, { label: string; className: string }> = {
  poster: { label: '海报', className: 'bg-blue-500/10 text-blue-400 ring-1 ring-blue-500/20' },
  seednote: { label: '种草笔记', className: 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20' },
  article: { label: '公众号', className: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20' },
}

interface TemplateCardProps {
  template: Template
  onClick: (template: Template) => void
}

export function TemplateCard({ template, onClick }: TemplateCardProps) {
  const typeInfo = typeBadgeMap[template.type]

  return (
    <button
      type="button"
      onClick={() => onClick(template)}
      className="group flex flex-col overflow-hidden rounded-lg border border-border bg-card text-left transition-all hover:border-primary/40 hover:shadow-md focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
    >
      {/* Thumbnail */}
      <div className="relative h-44 w-full overflow-hidden bg-muted sm:h-48">
        {template.thumbnail_url ? (
          <SignedImage
            src={template.thumbnail_url}
            alt={template.name}
            className="h-full w-full object-cover transition-transform duration-200 group-hover:scale-105"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center">
            <ImageIcon className="h-10 w-10 text-muted-foreground/40" />
          </div>
        )}
        {/* Type badge overlay */}
        <div className="absolute top-2 left-2">
          <Badge className={cn('text-[11px]', typeInfo.className)}>
            {typeInfo.label}
          </Badge>
        </div>
      </div>

      {/* Info */}
      <div className="flex flex-col gap-2 p-3">
        <h3 className="truncate text-sm font-medium text-foreground group-hover:text-primary transition-colors">
          {template.name}
        </h3>

        {template.style_prompt && (
          <p className="line-clamp-2 text-xs text-muted-foreground/80">
            {template.style_prompt}
          </p>
        )}

        {template.category && (
          <span className="text-xs text-muted-foreground">{template.category}</span>
        )}

        {/* Tags */}
        {(template.tags ?? []).length > 0 && (
          <div className="flex flex-wrap gap-1">
            {(template.tags ?? []).slice(0, 3).map((tag) => (
              <Badge key={tag} variant="secondary" className="text-[10px] px-1.5">
                {tag}
              </Badge>
            ))}
            {(template.tags ?? []).length > 3 && (
              <Badge variant="secondary" className="text-[10px] px-1.5">
                +{(template.tags ?? []).length - 3}
              </Badge>
            )}
          </div>
        )}
      </div>
    </button>
  )
}
