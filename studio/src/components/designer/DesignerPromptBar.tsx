import { useState, useRef, useEffect } from 'react'
import { Send, X, Paintbrush } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface DesignerPromptBarProps {
  onSubmit: (prompt: string) => void
  isGenerating: boolean
  onCancel?: () => void
  initialPrompt?: string
  initialPromptKey?: string
  onInitialPromptConsumed?: () => void
  editMode?: boolean
}

export default function DesignerPromptBar({
  onSubmit,
  isGenerating,
  onCancel,
  initialPrompt,
  initialPromptKey,
  onInitialPromptConsumed,
  editMode,
}: DesignerPromptBarProps) {
  const [prompt, setPrompt] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (initialPromptKey && initialPrompt) {
      setPrompt(initialPrompt)
      textareaRef.current?.focus()
      onInitialPromptConsumed?.()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialPromptKey])

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

  return (
    <div className="pointer-events-none absolute inset-x-4 bottom-4 z-10 md:inset-x-6 md:bottom-6">
      <div className="pointer-events-auto w-full rounded-2xl border border-border/75 bg-card/90 p-2 shadow-[0_4px_24px_-8px_rgba(0,0,0,0.24)] backdrop-blur">
        <div className="flex items-end gap-2">
          <textarea
            ref={textareaRef}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={editMode ? '描述你想修改的区域...' : '描述你想要生成的图片...'}
            rows={1}
            className="min-h-[36px] flex-1 resize-none bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
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
              {editMode ? <Paintbrush className="h-3.5 w-3.5" /> : <Send className="h-3.5 w-3.5" />}
              {editMode ? '编辑' : '生成'}
            </Button>
          )}
        </div>
      </div>
      {isGenerating && (
        <div className="pointer-events-auto mt-1.5 flex w-full items-center gap-2 px-2">
          <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse-dot" />
          <span className="text-[11px] text-muted-foreground">正在生成...</span>
        </div>
      )}
    </div>
  )
}
