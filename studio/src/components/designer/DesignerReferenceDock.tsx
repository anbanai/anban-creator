import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import { ImagePlus, Plus, X } from 'lucide-react'

import { cn } from '@/lib/utils'

export interface DesignerReferenceDockProps {
  files: File[]
  maxFiles: number
  onFilesAdded: (files: File[]) => void
  onFileRemove: (index: number) => void
  compact?: boolean
  dropActive?: boolean
}

interface FilePreview {
  file: File
  url: string
}

export default function DesignerReferenceDock({
  files,
  maxFiles,
  onFilesAdded,
  onFileRemove,
  compact = false,
  dropActive = false,
}: DesignerReferenceDockProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [previews, setPreviews] = useState<FilePreview[]>([])
  const hasCapacity = files.length < maxFiles

  useEffect(() => {
    const nextPreviews = files.map((file) => ({
      file,
      url: URL.createObjectURL(file),
    }))

    setPreviews(nextPreviews)

    return () => {
      nextPreviews.forEach(({ url }) => URL.revokeObjectURL(url))
    }
  }, [files])

  function openPicker() {
    if (hasCapacity) {
      inputRef.current?.click()
    }
  }

  function handleFilesSelected(event: ChangeEvent<HTMLInputElement>) {
    const selectedFiles = Array.from(event.target.files ?? [])

    if (selectedFiles.length > 0) {
      onFilesAdded(selectedFiles)
    }

    event.target.value = ''
  }

  return (
    <div
      data-testid="designer-reference-dock"
      data-compact={compact ? 'true' : 'false'}
      data-drop-active={dropActive ? 'true' : 'false'}
      className={cn(
        'rounded-xl transition-[background-color,box-shadow] duration-200 motion-reduce:transition-none',
        dropActive && 'bg-primary/10 ring-1 ring-primary/40 shadow-[0_0_24px_-10px_var(--color-primary)]',
      )}
    >
      <input
        ref={inputRef}
        data-testid="designer-reference-input"
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={handleFilesSelected}
      />

      {files.length === 0 ? (
        <button
          type="button"
          aria-label="添加参考图"
          onClick={openPicker}
          className="group flex w-full flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed border-border/70 bg-muted/20 px-3 py-5 text-center transition-all hover:border-primary/50 hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 motion-reduce:transition-none"
        >
          <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary/10 text-primary transition-transform group-hover:scale-105 motion-reduce:transition-none">
            <ImagePlus className="h-4 w-4" aria-hidden="true" />
          </span>
          <span className="text-xs font-semibold text-foreground">拖入参考图</span>
          <span className="text-[10px] text-muted-foreground">或点按浏览 · 最多 {maxFiles} 张</span>
        </button>
      ) : (
        <div
          className={cn(
            compact
              ? 'flex gap-2 overflow-x-auto pb-1'
              : 'grid grid-cols-3 gap-1.5',
          )}
        >
          {files.map((file, index) => {
            const preview = previews[index]
            const previewUrl = preview?.file === file ? preview.url : undefined

            return (
              <div
                key={`${file.name}-${file.size}-${file.lastModified}-${file.type}-${index}`}
                className="group relative aspect-square min-w-14 overflow-hidden rounded-lg bg-muted/30 ring-1 ring-border/70"
              >
                {previewUrl ? (
                  <img
                    src={previewUrl}
                    alt={file.name}
                    className="h-full w-full object-cover"
                  />
                ) : null}
                <button
                  type="button"
                  aria-label={`移除参考图：${file.name}`}
                  onClick={() => onFileRemove(index)}
                  className="absolute right-1 top-1 flex h-5 w-5 items-center justify-center rounded-full bg-background/85 text-foreground opacity-0 shadow-sm ring-1 ring-border/70 backdrop-blur transition-opacity group-hover:opacity-100 group-focus-within:opacity-100 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 motion-reduce:transition-none"
                >
                  <X className="h-3 w-3" aria-hidden="true" />
                </button>
              </div>
            )
          })}

          {hasCapacity ? (
            <button
              type="button"
              aria-label="添加更多参考图"
              onClick={openPicker}
              className="flex aspect-square min-w-14 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-border/70 bg-muted/20 text-muted-foreground transition-all hover:border-primary/50 hover:bg-primary/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 motion-reduce:transition-none"
            >
              <Plus className="h-4 w-4" aria-hidden="true" />
              <span className="text-[9px]">添加</span>
            </button>
          ) : (
            <div className="flex aspect-square min-w-14 items-center justify-center rounded-lg border border-border/70 bg-muted/30 px-1 text-center text-[9px] font-medium text-muted-foreground">
              已达上限
            </div>
          )}
        </div>
      )}
    </div>
  )
}
