import { useRef, useState } from 'react'
import { Clipboard, ImagePlus, LoaderCircle, RotateCcw, Upload, X } from 'lucide-react'
import { toast } from 'sonner'
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

  async function pasteImages() {
    try {
      const items = await navigator.clipboard.read()
      const files: File[] = []
      for (const item of items) {
        const type = item.types.find((value) => value.startsWith('image/'))
        if (type) files.push(new File([await item.getType(type)], `剪贴板-${Date.now()}-${files.length + 1}.${type.split('/')[1]}`, { type }))
      }
      if (files.length) addFiles(files)
      else toast.error('剪贴板中没有图片')
    } catch {
      toast.error('无法读取剪贴板，请粘贴图片或使用上传')
    }
  }

  return (
    <section aria-label="参考图" className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-medium text-muted-foreground">参考图 <span className="ml-1 text-[10px] tabular-nums">{controller.attachments.length}/{capacity}</span></h3>
        <input ref={inputRef} type="file" accept="image/*" multiple aria-label="选择参考图" className="hidden" disabled={disabled || controller.attachments.length >= capacity} onChange={(event) => {
          addFiles(Array.from(event.currentTarget.files ?? []))
          event.currentTarget.value = ''
        }} />
        <div className="flex gap-1">
          <button type="button" aria-label="从剪贴板添加参考图" disabled={full} onClick={() => void pasteImages()} className="flex h-7 items-center gap-1 rounded-md border border-border/70 px-1.5 text-[11px] hover:bg-muted disabled:opacity-40"><Clipboard className="size-3" />剪贴板</button>
          <button type="button" aria-label="添加参考图" disabled={full} onClick={() => inputRef.current?.click()} className="flex h-7 items-center gap-1 rounded-md border border-border/70 px-1.5 text-[11px] hover:bg-muted disabled:opacity-40"><Upload className="size-3" />上传</button>
        </div>
      </div>
      <div aria-label="参考图列表" className="flex min-h-20 max-h-44 flex-wrap content-start gap-2 overflow-y-auto rounded-xl border border-dashed border-border p-2" onDragOver={(event) => { event.preventDefault(); event.stopPropagation() }} onDrop={(event) => {
        event.preventDefault()
        event.stopPropagation()
        if (!full) addFiles(Array.from(event.dataTransfer.files))
      }} onPaste={(event) => {
        if (!full && event.clipboardData.files.length) { event.preventDefault(); event.stopPropagation(); addFiles(Array.from(event.clipboardData.files)) }
      }}>
        {controller.attachments.length === 0 && <button type="button" aria-label="上传或拖入参考图" disabled={full} onClick={() => inputRef.current?.click()} className="flex min-h-16 w-full flex-col items-center justify-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground disabled:opacity-40"><ImagePlus className="size-5" />上传或拖入参考图</button>}
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
      </div>
      <AttachmentPreviewDialog open={!!previewId} onOpenChange={(open) => { if (!open) setPreviewId(undefined) }} attachments={controller.attachments} selectedId={previewId} onSelectedChange={setPreviewId} previewSource={controller.previewSource} sourceAttachment={controller.sourceAttachment} />
    </section>
  )
}
