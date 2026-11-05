import { useState } from 'react'
import { ArrowUpLeft, ChevronDown, Library, Search, X } from 'lucide-react'
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { DESIGNER_PROMPTS, DESIGNER_PROMPT_TAGS, type DesignerPromptTemplate } from '@/lib/designer-prompts'

interface DesignerPromptLibraryProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (template: DesignerPromptTemplate) => void
  disabled?: boolean
}

function PromptTagFilter({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const [open, setOpen] = useState(false)
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={<button type="button" role="combobox" aria-label="提示词标签" aria-expanded={open} className="flex h-8 min-w-0 flex-1 items-center justify-between gap-2 rounded-lg border border-input bg-muted/30 px-2.5 text-left text-xs hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring" />}>
        <span className="truncate">{value === '全部' ? '全部标签' : value}</span>
        <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-(--anchor-width) min-w-52 max-w-[calc(100vw-2rem)] p-0">
        <Command>
          <CommandInput aria-label="搜索标签" placeholder="搜索标签..." />
          <CommandList>
            <CommandEmpty>没有找到标签</CommandEmpty>
            {['全部', ...DESIGNER_PROMPT_TAGS].map((item) => (
              <CommandItem key={item} value={item} data-checked={value === item} onSelect={() => { onChange(item); setOpen(false) }} className="text-left text-xs">
                <span className="min-w-0 flex-1 truncate">{item === '全部' ? '全部标签' : item}</span>
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

export default function DesignerPromptLibrary({ open, onOpenChange, onSelect, disabled }: DesignerPromptLibraryProps) {
  const [tag, setTag] = useState<string>('全部')
  const [query, setQuery] = useState('')
  const search = query.trim().toLocaleLowerCase()
  const filtered = tag !== '全部' || search.length > 0
  const templates = DESIGNER_PROMPTS.filter((template) =>
    (tag === '全部' || template.tags.some((item) => item === tag)) &&
    `${template.title} ${template.tags.join(' ')} ${template.prompt}`.toLocaleLowerCase().includes(search),
  )
  const content = (
    <>
      <div className="shrink-0 space-y-2 border-b border-border/60 px-4 pb-3">
        <div className="relative">
          <Search aria-hidden className="pointer-events-none absolute left-2.5 top-2.5 size-3.5 text-muted-foreground" />
          <Input aria-label="搜索提示词" placeholder="搜索提示词、风格、关键词..." value={query} onChange={(event) => setQuery(event.target.value)} className="h-8 pr-8 pl-8 text-xs" />
          {query && <button type="button" aria-label="清除搜索" onClick={() => setQuery('')} className="absolute right-1 top-1 rounded p-1 text-muted-foreground hover:bg-muted"><X className="size-4" /></button>}
        </div>
        <div className="flex items-center gap-2">
          <PromptTagFilter value={tag} onChange={setTag} />
          {filtered && <button type="button" aria-label="重置筛选" onClick={() => { setTag('全部'); setQuery('') }} className="shrink-0 rounded px-1 py-1.5 text-xs text-muted-foreground hover:text-foreground">重置</button>}
        </div>
        <p aria-live="polite" className="text-[11px] text-muted-foreground">{filtered ? `找到 ${templates.length} 个提示词` : `共 ${templates.length} 个提示词`}</p>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {templates.length === 0 && <p className="py-8 text-center text-xs text-muted-foreground">没有找到匹配的提示词</p>}
        <div className="grid grid-cols-2 gap-3">
          {templates.map((template) => (
            <button
              key={template.id}
              type="button"
              aria-label={`使用${template.title}提示词`}
              disabled={disabled}
              onClick={() => { onSelect(template); onOpenChange(false) }}
              className="group min-w-0 overflow-hidden rounded-xl border border-border/60 bg-background text-left transition-colors hover:border-primary/40 focus-visible:outline-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50"
            >
              <div className="relative aspect-[4/5] overflow-hidden bg-muted">
                <img src={template.image} alt={`${template.title}参考样图`} loading="lazy" className="h-full w-full object-cover" />
                <span className="absolute right-2 bottom-2 rounded-full bg-background/90 p-1.5 text-foreground opacity-0 shadow-sm transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
                  <ArrowUpLeft className="size-3.5" />
                </span>
              </div>
              <div className="px-2.5 py-2.5">
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
