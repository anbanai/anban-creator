import { useState, useRef, useEffect } from 'react'
import { Send, Loader2, ImageIcon } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { ScrollArea } from '@/components/ui/scroll-area'

interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  images?: string[]
  timestamp: Date
}

interface PosterChatProps {
  messages: ChatMessage[]
  isGenerating?: boolean
  onSend: (message: string) => void
  className?: string
}

export type { ChatMessage }

export default function PosterChat({ messages, isGenerating, onSend, className }: PosterChatProps) {
  const [input, setInput] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [messages])

  function handleSend() {
    const trimmed = input.trim()
    if (!trimmed || isGenerating) return
    onSend(trimmed)
    setInput('')
    if (inputRef.current) {
      inputRef.current.style.height = 'auto'
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  function handleTextareaInput(e: React.ChangeEvent<HTMLTextAreaElement>) {
    setInput(e.target.value)
    e.target.style.height = 'auto'
    e.target.style.height = Math.min(e.target.scrollHeight, 120) + 'px'
  }

  return (
    <div className={`flex flex-col rounded-lg border border-border bg-card ${className || ''}`}>
      <ScrollArea className="flex-1">
        <div ref={scrollRef} className="space-y-4 p-4">
          {messages.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              生成海报后可以在此对话，描述你想要的调整
            </p>
          ) : (
            messages.map((msg) => (
              <div
                key={msg.id}
                className={`flex gap-2.5 ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}
              >
                {msg.role === 'assistant' && (
                  <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary/10">
                    <ImageIcon className="h-3.5 w-3.5 text-primary" />
                  </div>
                )}
                <div
                  className={`max-w-[80%] rounded-lg px-3 py-2 text-sm ${
                    msg.role === 'user'
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted text-foreground'
                  }`}
                >
                  <p className="whitespace-pre-wrap leading-relaxed">{msg.content}</p>
                  {msg.images && msg.images.length > 0 && (
                    <div className="mt-2 grid grid-cols-2 gap-2">
                      {msg.images.map((img, i) => (
                        <img
                          key={i}
                          src={img}
                          alt=""
                          className="rounded-md object-cover"
                        />
                      ))}
                    </div>
                  )}
                </div>
              </div>
            ))
          )}
          {isGenerating && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              正在生成...
            </div>
          )}
        </div>
      </ScrollArea>

      {/* Input area */}
      <div className="border-t border-border p-3">
        <div className="flex items-end gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={handleTextareaInput}
            onKeyDown={handleKeyDown}
            placeholder="描述你想调整的地方..."
            rows={1}
            className="flex-1 resize-none rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
          <Button
            size="sm"
            onClick={handleSend}
            disabled={!input.trim() || isGenerating}
          >
            <Send className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  )
}
