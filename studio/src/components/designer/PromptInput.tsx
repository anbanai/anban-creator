import { useState, useRef, useEffect } from 'react'
import { Send, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface PromptInputProps {
  onSubmit: (prompt: string) => void
  isGenerating: boolean
}

export default function PromptInput({ onSubmit, isGenerating }: PromptInputProps) {
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

  return (
    <div className="flex items-end gap-2 rounded-lg border border-border bg-card px-3 py-2">
      <textarea
        ref={textareaRef}
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="描述你想要生成的图片... (Ctrl+Enter 发送)"
        rows={1}
        className="flex-1 resize-none bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
      />
      <Button
        size="sm"
        onClick={handleSubmit}
        disabled={!prompt.trim() || isGenerating}
      >
        {isGenerating ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <Send className="h-4 w-4" />
        )}
        生成
      </Button>
    </div>
  )
}
