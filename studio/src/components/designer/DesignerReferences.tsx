import { useRef } from 'react'
import { LoaderCircle, Plus, RotateCcw, X } from 'lucide-react'
import type { PromptAttachmentsController } from '@/components/agent-prompt/usePromptAttachments'
import type { AttachmentRejection } from '@/types/input-attachment'

interface DesignerReferencesProps {
  controller: PromptAttachmentsController
  capacity: number
  disabled: boolean
  onRejected: (rejections: AttachmentRejection[]) => void
  onPreview: (url: string) => void
}

export default function DesignerReferences({ controller, capacity, disabled, onRejected, onPreview }: DesignerReferencesProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  return (
    <section aria-label="参考图" className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-medium text-muted-foreground">参考图 <span className="ml-1 text-[10px] tabular-nums">{controller.attachments.length}/{capacity}</span></h3>
        <input ref={inputRef} type="file" accept="image/*" multiple aria-label="选择参考图" className="hidden" disabled={disabled || controller.attachments.length >= capacity} onChange={(event) => {
          const result = controller.addFiles(Array.from(event.currentTarget.files ?? []))
          if (result.rejected.length) onRejected(result.rejected)
          event.currentTarget.value = ''
        }} />
        <button type="button" aria-label="添加参考图" disabled={disabled || controller.attachments.length >= capacity} onClick={() => inputRef.current?.click()} className="flex h-7 items-center gap-1 rounded-md border border-border/70 px-2 text-[11px] hover:bg-muted disabled:opacity-40">
          <Plus className="size-3" />添加
        </button>
      </div>
      {controller.attachments.length > 0 && <div className="flex flex-wrap gap-2">
        {controller.attachments.map((attachment) => {
          const source = controller.previewSource(attachment.id)
          return <div key={attachment.id} className="relative size-12 rounded-md border border-border bg-muted/40">
            <button type="button" aria-label={`预览 ${attachment.fileName}`} disabled={!source} onClick={() => source && onPreview(source)} className="h-full w-full overflow-hidden rounded-md">
              {source && <img src={source} alt={attachment.fileName} className="h-full w-full object-cover" />}
              <span className="sr-only">{attachment.fileName}</span>
            </button>
            <span role="status" aria-label={`${attachment.fileName} 状态`} className="sr-only">{attachment.status === 'uploaded' ? '已上传' : attachment.status === 'failed' ? '上传失败' : '上传中'}</span>
            {(attachment.status === 'uploading' || attachment.status === 'queued') && <LoaderCircle aria-hidden className="pointer-events-none absolute bottom-1 left-1 size-3 animate-spin bg-background/80" />}
            {attachment.status === 'failed' && <button type="button" aria-label={`重试 ${attachment.fileName}`} title={attachment.error} disabled={disabled} onClick={() => controller.retry(attachment.id)} className="absolute bottom-0 left-0 rounded bg-background/90 p-1 text-destructive"><RotateCcw className="size-3" /></button>}
            <button type="button" aria-label={`移除 ${attachment.fileName}`} disabled={disabled} onClick={() => controller.remove(attachment.id)} className="absolute -right-1 -top-1 rounded-full border bg-background p-0.5 shadow-sm"><X className="size-3" /></button>
          </div>
        })}
      </div>}
    </section>
  )
}
