import { useRef, useState } from 'react'
import { LoaderCircle, Plus, RotateCcw, X } from 'lucide-react'
import { AttachmentPreviewDialog } from '@/components/agent-prompt/AttachmentPreviewDialog'
import type { PromptAttachmentsController } from '@/components/agent-prompt/usePromptAttachments'
import type { AttachmentRejection } from '@/types/input-attachment'

interface DesignerReferencesProps {
  controller: PromptAttachmentsController
  capacity: number
  disabled: boolean
  onRejected: (rejections: AttachmentRejection[]) => void
}

export default function DesignerReferences({ controller, capacity, disabled, onRejected }: DesignerReferencesProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [previewId, setPreviewId] = useState<string>()
  const full = disabled || controller.attachments.length >= capacity

  function addFiles(files: File[]) {
    const result = controller.addFiles(files)
    if (result.rejected.length) onRejected(result.rejected)
  }

  return (
    <section aria-label="参考图" className="space-y-2">
      <h3 className="text-xs font-semibold text-foreground">参考图 <span className="ml-1 text-[10px] font-normal text-muted-foreground tabular-nums">{controller.attachments.length}/{capacity}</span></h3>
      <input ref={inputRef} type="file" accept="image/*" multiple aria-label="选择参考图" className="hidden" disabled={full} onChange={(event) => {
        addFiles(Array.from(event.currentTarget.files ?? []))
        event.currentTarget.value = ''
      }} />
      <div aria-label="参考图列表" className="flex min-h-20 max-h-44 flex-wrap content-start gap-2 overflow-y-auto rounded-xl border border-dashed border-border p-2" onDragOver={(event) => { event.preventDefault(); event.stopPropagation() }} onDrop={(event) => {
        event.preventDefault()
        event.stopPropagation()
        if (!full) addFiles(Array.from(event.dataTransfer.files))
      }} onPaste={(event) => {
        if (!full && event.clipboardData.files.length) { event.preventDefault(); event.stopPropagation(); addFiles(Array.from(event.clipboardData.files)) }
      }}>
        {controller.attachments.map((attachment, index) => {
          const source = controller.previewSource(attachment.id)
          return <div key={attachment.id} className="relative size-16 shrink-0 rounded-lg border border-border bg-muted/40">
            <button type="button" aria-label={`预览 ${attachment.fileName}`} disabled={!source} onClick={() => setPreviewId(attachment.id)} className="h-full w-full overflow-hidden rounded-lg">
              {source && <img src={source} alt={attachment.fileName} className="h-full w-full object-contain" />}
              <span className="absolute left-1 top-1 rounded bg-black/55 px-1 text-[9px] leading-4 text-white">图片{index + 1}</span>
              <span className="sr-only">{attachment.fileName}</span>
            </button>
            <span role="status" aria-label={`${attachment.fileName} 状态`} className="sr-only">{attachment.status === 'uploaded' ? '已上传' : attachment.status === 'failed' ? '上传失败' : '上传中'}</span>
            {(attachment.status === 'uploading' || attachment.status === 'queued') && <LoaderCircle aria-hidden className="pointer-events-none absolute bottom-1 left-1 size-3 animate-spin bg-background/80" />}
            {attachment.status === 'failed' && <button type="button" aria-label={`重试 ${attachment.fileName}`} title={attachment.error} disabled={disabled} onClick={() => controller.retry(attachment.id)} className="absolute bottom-0 left-0 rounded bg-background/90 p-1 text-destructive"><RotateCcw className="size-3" /></button>}
            <button type="button" aria-label={`移除 ${attachment.fileName}`} disabled={disabled} onClick={() => controller.remove(attachment.id)} className="absolute -right-1 -top-1 rounded-full border bg-background p-0.5 shadow-sm"><X className="size-3" /></button>
          </div>
        })}
        <button type="button" aria-label="添加参考图" title="添加参考图" disabled={full} onClick={() => inputRef.current?.click()} className="flex size-16 shrink-0 items-center justify-center rounded-lg bg-muted/40 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-40"><Plus className="size-5" /></button>
      </div>
      <AttachmentPreviewDialog open={!!previewId} onOpenChange={(open) => { if (!open) setPreviewId(undefined) }} attachments={controller.attachments} selectedId={previewId} onSelectedChange={setPreviewId} previewSource={controller.previewSource} sourceAttachment={controller.sourceAttachment} />
    </section>
  )
}
