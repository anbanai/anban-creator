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
    <div className="flex shrink-0 items-end gap-2 border-t border-border bg-card px-4 py-3">
      {/* Model quick-select (only when multiple providers) */}
      {providers.length > 1 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="outline" size="sm" className="shrink-0 gap-1" />
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

      {/* Textarea */}
      <textarea
        ref={textareaRef}
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="描述你想要生成的图片... (Ctrl+Enter 发送)"
        rows={1}
        className="flex-1 resize-none bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
      />

      {/* Send / Cancel */}
      {isGenerating && onCancel ? (
        <Button size="sm" variant="destructive" onClick={onCancel} className="shrink-0">
          <X className="h-4 w-4" />
          取消
        </Button>
      ) : (
        <Button
          size="sm"
          onClick={handleSubmit}
          disabled={!prompt.trim() || isGenerating}
          className="shrink-0"
        >
          <Send className="h-4 w-4" />
          生成
        </Button>
      )}
    </div>
  )
}
