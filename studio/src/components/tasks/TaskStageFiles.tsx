import { useState } from 'react'
import { Download } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import type { TaskFile } from '@/types'
import { Button } from '@/components/common/button'
import { FilePreviewGallery } from '@/components/FilePreview'
import { EcommerceFilesGallery } from '@/components/tasks/EcommerceFilesGallery'

export function TaskFileDownloads({ taskId, files }: { taskId: string; files: TaskFile[] }) {
  const [downloading, setDownloading] = useState<'delivered' | 'retained' | null>(null)
  const hasDelivery = files.some((file) => file.state === 'delivered' && file.is_deliverable)
  const hasRetained = files.some((file) => file.state === 'retained')

  async function download(scope: 'delivered' | 'retained') {
    if (downloading) return
    setDownloading(scope)
    try {
      const blob = await (scope === 'delivered' ? api.tasks.downloadZipBlob(taskId) : api.tasks.downloadRetainedZipBlob(taskId))
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = `task_${taskId}_${scope === 'retained' ? 'retained_' : ''}files.zip`
      anchor.click()
      URL.revokeObjectURL(url)
    } catch {
      toast.error(scope === 'delivered' ? '下载 ZIP 失败，请稍后重试' : '下载已保留产物失败，请稍后重试')
    } finally {
      setDownloading(null)
    }
  }

  return <>
    {hasDelivery && <Button size="xs" variant="outline" loading={downloading === 'delivered'} disabled={!!downloading} onClick={() => void download('delivered')} aria-label="下载交付成果 (ZIP)"><Download className="size-3.5" />下载成果</Button>}
    {hasRetained && <Button size="xs" variant="outline" loading={downloading === 'retained'} disabled={!!downloading} onClick={() => void download('retained')} aria-label="下载已保留产物 (ZIP)"><Download className="size-3.5" />下载已保留产物</Button>}
  </>
}

export function TaskStageFiles({ files, taskId, taskType }: { files: TaskFile[]; taskId: string; taskType?: string }) {
  const groups = [
    { label: '交付成果', files: files.filter((file) => file.state === 'delivered' && file.is_deliverable) },
    { label: '阶段产物', files: files.filter((file) => file.state === 'delivered' && !file.is_deliverable) },
    { label: '已保留产物', files: files.filter((file) => file.state === 'retained') },
  ]
  return <div className="space-y-3">
    {groups.filter((group) => group.files.length > 0).map((group) => (
      <section key={group.label} aria-label={group.label} className="min-w-0 space-y-2">
        <h3 className="text-xs font-medium text-muted-foreground">{group.label} ({group.files.length})</h3>
        {taskType === 'ecommerce' ? <EcommerceFilesGallery files={group.files} taskId={taskId} compact /> : <>
          {group.files.some((file) => file.mime_type?.startsWith('image/')) && (
            <div className="flex gap-3 overflow-x-auto pb-2 snap-x snap-mandatory">
              <FilePreviewGallery files={group.files.filter((file) => file.mime_type?.startsWith('image/'))} taskId={taskId} taskType={taskType} compact inlineItemClassName="shrink-0 snap-start" />
            </div>
          )}
          {group.files.some((file) => !file.mime_type?.startsWith('image/')) && <div className="overflow-hidden rounded-lg border border-border/70 bg-card divide-y divide-border/60">
            <FilePreviewGallery files={group.files.filter((file) => !file.mime_type?.startsWith('image/'))} taskId={taskId} taskType={taskType} compact />
          </div>}
        </>}
      </section>
    ))}
  </div>
}
