import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { Button } from '@/components/common/button'
import { Textarea } from '@/components/ui/textarea'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import type { InputAttachment } from '@/types/input-attachment'
import type { TaskFile } from '@/types'

interface Props {
  taskId: string
  files: TaskFile[]
}

export function TalkingHeadEditorReviewPanel({ taskId, files }: Props) {
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState('')
  const [reference, setReference] = useState<InputAttachment[]>([])
  const [uploading, setUploading] = useState(false)
  const [uploadFailed, setUploadFailed] = useState(false)
  const sample = files.find((file) => file.file_name === 'motion-sample.mp4')
  const review = useMutation({
    mutationFn: (decision: 'approve' | 'revise') => api.tasks.reviewTalkingHeadEditor(taskId, {
      decision,
      feedback: feedback.trim() || undefined,
      reference_cut_asset_id: reference[0]?.asset_id || undefined,
      reference_cut_url: reference[0]?.url || undefined,
    }),
    onSuccess: () => {
      toast.success('审核意见已提交，任务将继续执行')
      void queryClient.invalidateQueries({ queryKey: ['task', taskId] })
      void queryClient.invalidateQueries({ queryKey: ['task-files', taskId] })
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '提交审核意见失败')),
  })

  return (
    <section aria-label="口播精剪样片审核" className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-4 sm:p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold">动效样片审核</h2>
          <p className="mt-1 text-sm text-muted-foreground">确认样片后才会继续添加音效、BGM 并输出最终成片。</p>
        </div>
        <RefreshCw className="size-5 text-amber-700" aria-hidden="true" />
      </div>
      {sample ? <video className="mt-4 max-h-[32rem] w-full rounded-md bg-black" controls preload="metadata" src={sample.preview_url || sample.url} aria-label="动效样片" /> : <p className="mt-4 text-sm text-muted-foreground">样片文件正在准备，稍后刷新本页。</p>}
      <div className="mt-4 space-y-3 border-t border-border/70 pt-4">
        <label className="text-sm font-medium" htmlFor="talking-head-review-feedback">修改意见</label>
        <Textarea id="talking-head-review-feedback" value={feedback} onChange={(event) => setFeedback(event.target.value)} rows={3} placeholder="需要修改时，说明镜头、节奏或风格问题" />
        <ReferenceMaterialInput value={reference} onChange={setReference} allowedTypes={['video']} maxCount={1} maxFileBytes={1024 * 1024 * 1024} compact uploadPurpose="talking_head_editor_asset" hint="可选：上传手动调整后的理想剪辑，作为本次节奏参考。" onUploadingChange={setUploading} onFailuresChange={setUploadFailed} />
        <div className="flex flex-wrap justify-end gap-2">
          <Button type="button" variant="outline" disabled={review.isPending || uploading || uploadFailed || !feedback.trim()} loading={review.isPending} onClick={() => review.mutate('revise')}>要求修改</Button>
          <Button type="button" disabled={review.isPending || uploading || uploadFailed} loading={review.isPending} onClick={() => review.mutate('approve')}><Check className="size-4" aria-hidden="true" />全部通过，继续成片</Button>
        </div>
      </div>
    </section>
  )
}
