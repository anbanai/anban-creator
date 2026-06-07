import { useState, useRef, useEffect } from 'react'
import { Send, X, ChevronDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@/components/ui/dropdown-menu'
import type { DesignerProvider } from '@/types/designer'

interface DesignerPromptBarProps {
  onSubmit: (prompt: string) => void
  isGenerating: boolean
  onCancel?: () => void
  providers: DesignerProvider[]
  selectedProviderId: string
  onModelChange: (id: string) => void
}

export default function DesignerPromptBar({
  onSubmit,
  isGenerating,
  onCancel,
  providers,
  selectedProviderId,
  onModelChange,
}: DesignerPromptBarProps) {
  const [prompt, setPrompt] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    const el = textareaRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = `${Math.min(el.scrollHeight, 200)}px`
    }
  }, [prompt])

  function handleSubmit() {
    const trimmed = prompt.trim()
    if (!trimmed || isGenerating) return
    onSubmit(trimmed)
    setPrompt('')
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const selected = providers.find((p) => p.id === selectedProviderId) ?? providers[0]

  return (
    <div className="shrink-0 bg-muted/30 px-3 pb-4 pt-2 md:px-4">
      <div className="mx-auto max-w-2xl rounded-2xl border border-border bg-card p-2 shadow-xl shadow-black/10">
        <div className="flex items-end gap-2">
          {providers.length > 1 && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="sm" className="shrink-0 gap-1 text-xs text-muted-foreground hover:text-foreground" />
                }
              >
                {selected?.name ?? '模型'}
                <ChevronDown className="h-3 w-3" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                {providers.map((p) => (
                  <DropdownMenuItem
                    key={p.id}
                    onClick={() => onModelChange(p.id)}
                    className={selectedProviderId === p.id ? 'bg-primary/10' : ''}
                  >
                    {p.name}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}

          <textarea
            ref={textareaRef}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="描述你想要生成的图片..."
            rows={1}
            className="min-h-[36px] flex-1 resize-none bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground/50"
          />

          {isGenerating && onCancel ? (
            <Button
              size="sm"
              variant="ghost"
              onClick={onCancel}
              className="shrink-0 rounded-xl text-destructive/80 hover:bg-destructive/10 hover:text-destructive"
            >
              <X className="h-3.5 w-3.5" />
              取消
            </Button>
          ) : (
            <Button
              size="sm"
              onClick={handleSubmit}
              disabled={!prompt.trim() || isGenerating}
              className="rounded-xl bg-primary px-4 font-medium shadow-md shadow-primary/20 transition-all hover:shadow-lg hover:shadow-primary/30 disabled:opacity-40"
            >
              <Send className="h-3.5 w-3.5" />
              生成
            </Button>
          )}
        </div>
      </div>
      {isGenerating && (
        <div className="mx-auto mt-1.5 flex max-w-2xl items-center gap-2 px-2">
          <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse-dot" />
          <span className="text-[11px] text-muted-foreground/60">正在生成...</span>
        </div>
      )}
    </div>
  )
}
