import { useState } from 'react'
import { ArrowUpLeft, Library } from 'lucide-react'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { DESIGNER_PROMPTS, DESIGNER_PROMPT_TAGS, type DesignerPromptTemplate } from '@/lib/designer-prompts'
import { cn } from '@/lib/utils'

interface DesignerPromptLibraryProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (template: DesignerPromptTemplate) => void
  disabled?: boolean
}

export default function DesignerPromptLibrary({ open, onOpenChange, onSelect, disabled }: DesignerPromptLibraryProps) {
  const [tag, setTag] = useState<string>('全部')
  const templates = DESIGNER_PROMPTS.filter((template) => tag === '全部' || template.tags.some((item) => item === tag))
  const content = (
    <>
      <div className="flex shrink-0 flex-wrap gap-1.5 px-4 pb-4" aria-label="提示词标签">
        {['全部', ...DESIGNER_PROMPT_TAGS].map((item) => (
          <button
            key={item}
            type="button"
            aria-pressed={tag === item}
            onClick={() => setTag(item)}
            className={cn('rounded-full border px-2.5 py-1.5 text-xs transition-colors focus-visible:outline-2 focus-visible:outline-ring',
              tag === item ? 'border-primary/20 bg-primary/8 text-primary' : 'border-border/70 text-muted-foreground hover:bg-muted')}
          >
            {item}
          </button>
        ))}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
        <div className="grid grid-cols-2 gap-2">
          {templates.map((template) => (
            <button
              key={template.id}
              type="button"
              aria-label={`使用${template.title}提示词`}
              disabled={disabled}
              onClick={() => { onSelect(template); onOpenChange(false) }}
              className="group min-w-0 overflow-hidden rounded-xl border border-border/60 bg-background text-left transition-colors hover:border-primary/40 focus-visible:outline-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50"
            >
              <div className="relative h-18 overflow-hidden bg-muted">
                <img src={template.image} alt={`${template.title}参考样图`} loading="lazy" className="h-full w-full object-contain" />
                <span className="absolute right-2 bottom-2 rounded-full bg-background/90 p-1.5 text-foreground opacity-0 shadow-sm transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
                  <ArrowUpLeft className="size-3.5" />
                </span>
              </div>
              <div className="px-2 py-1.5">
                <p className="truncate text-xs font-medium">{template.title}</p>
                <p className="mt-1 truncate text-[10px] text-muted-foreground">{template.tags.join(' · ')}</p>
              </div>
            </button>
          ))}
        </div>
      </div>
      <p className="shrink-0 border-t border-border/60 px-4 py-3 text-[11px] text-muted-foreground">样图仅作风格参考 · 点击填入提示词</p>
    </>
  )

  return (
    <>
      <div className="hidden w-[300px] shrink-0 py-3 pr-3 xl:block">
        <aside aria-label="提示词库" className="flex h-full min-h-0 flex-col overflow-hidden rounded-2xl border border-border/70 bg-card shadow-sm">
          <header className="flex shrink-0 items-center gap-2.5 p-4">
            <Library className="size-4 text-primary" />
            <h2 className="text-sm font-semibold">提示词库</h2>
            <span className="ml-auto text-xs text-muted-foreground">{templates.length} 个</span>
          </header>
          {content}
        </aside>
      </div>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent className="gap-0 data-[side=right]:w-[min(360px,100vw)]" aria-describedby={undefined}>
          <SheetHeader><SheetTitle>提示词库</SheetTitle></SheetHeader>
          {content}
        </SheetContent>
      </Sheet>
    </>
  )
}
