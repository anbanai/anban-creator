import { Sparkles, ImageIcon } from 'lucide-react'
import type { Template } from '@/types'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface TemplateRecommendProps {
  templates: Template[]
  onClose: () => void
  onUseTemplate: (template: Template) => void
}

export function TemplateRecommend({ templates, onClose, onUseTemplate }: TemplateRecommendProps) {
  if (templates.length === 0) return null

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-2xl gap-0 overflow-hidden p-0">
        <div className="flex items-center gap-2 border-b border-border px-5 py-4">
          <Sparkles className="h-5 w-5 text-primary" />
          <DialogHeader className="gap-0">
            <DialogTitle className="text-base font-semibold">推荐模板</DialogTitle>
          </DialogHeader>
        </div>

        <DialogDescription className="px-5 py-3">
          根据你的账号风格，为你推荐以下模板。选择一个模板快速开始创作。
        </DialogDescription>

        <ScrollArea className="max-h-[400px]">
          <div className="grid grid-cols-2 gap-3 px-5 pb-3">
            {templates.map((template) => (
              <button
                key={template.id}
                type="button"
                onClick={() => onUseTemplate(template)}
                className="group overflow-hidden rounded-lg border border-border transition-all hover:border-primary/50 hover:shadow-sm"
              >
                <div className="aspect-[3/4] w-full overflow-hidden bg-muted">
                  {template.thumbnail_url ? (
                    <img
                      src={template.thumbnail_url}
                      alt={template.name}
                      className="h-full w-full object-cover transition-transform group-hover:scale-105"
                    />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center">
                      <ImageIcon className="h-8 w-8 text-muted-foreground" />
                    </div>
                  )}
                </div>
                <div className="px-2.5 py-2">
                  <p className="truncate text-xs font-medium text-foreground">{template.name}</p>
                  {template.category && (
                    <p className="mt-0.5 truncate text-[11px] text-muted-foreground">{template.category}</p>
                  )}
                </div>
              </button>
            ))}
          </div>
        </ScrollArea>

        <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">
          <Button variant="outline" size="sm" onClick={onClose}>
            跳过
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
