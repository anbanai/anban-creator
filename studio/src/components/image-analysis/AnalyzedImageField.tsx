import { useMutation } from '@tanstack/react-query'
import { Loader2, RefreshCw, Square } from 'lucide-react'
import { toast } from 'sonner'

import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { ImageAnalysis, ReferenceImageSelection, ReferenceImageValue } from '@/types'
import { isImageAnalysisActive } from '@/types'

interface AnalyzedImageFieldProps {
  asset: ReferenceImageValue | null
  onAssetChange: (value: ReferenceImageSelection | null) => void
  purpose: 'project_reference' | 'template_thumbnail'
  text: string
  onTextChange: (value: string) => void
  analysis?: ImageAnalysis | null
  placeholder: string
  textId?: string
  disabled?: boolean
  onUploadingChange?: (uploading: boolean) => void
  onBeforeAnalysisAction?: () => void | Promise<void>
  onAnalysisAction?: () => void | Promise<void>
}

export function AnalyzedImageField({
  asset,
  onAssetChange,
  purpose,
  text,
  onTextChange,
  analysis,
  placeholder,
  textId,
  disabled = false,
  onUploadingChange,
  onBeforeAnalysisAction,
  onAnalysisAction,
}: AnalyzedImageFieldProps) {
  const active = isImageAnalysisActive(analysis)
  const locked = disabled || active
  const retryMutation = useMutation({ mutationFn: (id: string) => api.imageAnalyses.retry(id) })
  const cancelMutation = useMutation({ mutationFn: (id: string) => api.imageAnalyses.cancel(id) })

  const retry = async () => {
    if (!analysis) return
    try {
      await onBeforeAnalysisAction?.()
      await retryMutation.mutateAsync(analysis.id)
      await onAnalysisAction?.()
    } catch (error) {
      toast.error(getApiErrorMessage(error, '重试识别失败'))
    }
  }

  const cancel = async () => {
    if (!analysis) return
    try {
      await onBeforeAnalysisAction?.()
      await cancelMutation.mutateAsync(analysis.id)
      await onAnalysisAction?.()
    } catch (error) {
      toast.error(getApiErrorMessage(error, '停止识别失败'))
    }
  }

  return (
    <div className="grid gap-3 sm:grid-cols-[8rem_1fr]">
      <div className="relative overflow-hidden rounded-lg">
        <ReferenceAssetUpload
          value={asset}
          onChange={onAssetChange}
          purpose={purpose}
          disabled={locked}
          onUploadingChange={onUploadingChange}
        />
        {active ? (
          <div className="pointer-events-none absolute inset-x-0 top-0 h-0.5 animate-pulse bg-primary shadow-[0_0_12px_var(--color-primary)]" />
        ) : null}
      </div>
      <div className="space-y-2">
        <Textarea
          id={textId}
          value={text}
          onChange={(event) => onTextChange(event.target.value)}
          placeholder={placeholder}
          maxLength={4000}
          disabled={locked}
          className="min-h-32 resize-y"
        />
        <div className="flex min-h-8 items-center justify-between gap-3 text-xs text-muted-foreground" aria-live="polite">
          <span className="min-w-0 truncate">
            {active ? (
              <span className="inline-flex items-center gap-1.5"><Loader2 className="size-3.5 animate-spin" />正在后台识别图片</span>
            ) : analysis?.status === 'failed' ? (analysis.error_message || '识别失败，可重试或手动填写')
              : analysis?.status === 'succeeded' ? '识别结果已回填'
                : '留空时将在创建后自动识别'}
          </span>
          {active ? (
            <Button type="button" variant="ghost" size="xs" onClick={() => void cancel()} disabled={cancelMutation.isPending}>
              <Square className="size-3" />停止识别并手动填写
            </Button>
          ) : analysis?.can_retry ? (
            <Button type="button" variant="outline" size="xs" onClick={() => void retry()} disabled={retryMutation.isPending}>
              <RefreshCw className={retryMutation.isPending ? 'animate-spin' : ''} />重试
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}
