import * as React from "react"
import { XIcon } from "lucide-react"
import { cn } from "@/lib/utils"

interface TagInputProps {
  value?: string
  onChange?: (value: string) => void
  placeholder?: string
  maxTags?: number
  disabled?: boolean
  className?: string
}

function parseTags(value: string): string[] {
  return value
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean)
}

function serializeTags(tags: string[]): string {
  return tags.join(", ")
}

export function TagInput({
  value = "",
  onChange,
  placeholder = "输入后按回车添加",
  maxTags = 20,
  disabled = false,
  className,
}: TagInputProps) {
  const tags = parseTags(value)
  const [inputValue, setInputValue] = React.useState("")
  const inputRef = React.useRef<HTMLInputElement>(null)

  const addTag = (text: string) => {
    const newTag = text.trim()
    if (!newTag || tags.includes(newTag) || tags.length >= maxTags) return

    const newTags = [...tags, newTag]
    onChange?.(serializeTags(newTags))
    setInputValue("")
  }

  const removeTag = (index: number) => {
    const newTags = tags.filter((_, i) => i !== index)
    onChange?.(serializeTags(newTags))
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" || e.key === "," || e.key === "Tab") {
      e.preventDefault()
      addTag(inputValue)
    }
    if (e.key === "Backspace" && inputValue === "" && tags.length > 0) {
      removeTag(tags.length - 1)
    }
  }

  const handlePaste = (e: React.ClipboardEvent) => {
    e.preventDefault()
    const pasted = e.clipboardData.getData("text")
    const pastedTags = pasted.split(/[,，]/).map((t) => t.trim()).filter(Boolean)
    let newTags = [...tags]
    for (const tag of pastedTags) {
      if (newTags.includes(tag) || newTags.length >= maxTags) break
      newTags.push(tag)
    }
    onChange?.(serializeTags(newTags))
    setInputValue("")
  }

  return (
    <div
      className={cn(
        "flex min-h-[2rem] w-full flex-wrap items-center gap-1.5 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none",
        "focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
        "disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50",
        "aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20",
        "md:text-sm",
        "dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40",
        className
      )}
      onClick={() => inputRef.current?.focus()}
    >
      {tags.map((tag, i) => (
        <span
          key={`${tag}-${i}`}
          className="inline-flex h-6 shrink-0 items-center gap-0.5 rounded-md bg-secondary px-1.5 text-xs font-medium text-secondary-foreground"
        >
          <span className="max-w-[120px] truncate">{tag}</span>
          {!disabled && (
            <button
              type="button"
              className="ml-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-sm opacity-60 transition-opacity hover:opacity-100"
              onClick={(e) => {
                e.stopPropagation()
                removeTag(i)
              }}
            >
              <XIcon className="h-3 w-3" />
            </button>
          )}
        </span>
      ))}
      <input
        ref={inputRef}
        type="text"
        value={inputValue}
        onChange={(e) => setInputValue(e.target.value)}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        placeholder={tags.length === 0 ? placeholder : ""}
        disabled={disabled || tags.length >= maxTags}
        className="h-6 min-w-[80px] flex-1 border-0 bg-transparent px-0 py-0 text-sm outline-none placeholder:text-muted-foreground"
      />
    </div>
  )
}
