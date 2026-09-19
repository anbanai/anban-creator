import { CircleAlert, CircleCheck, Loader2, PauseCircle } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import type { ImageAnalysis } from '@/types'

export function ImageAnalysisBadge({ analysis }: { analysis?: ImageAnalysis | null }) {
  if (!analysis) return null
  if (analysis.status === 'queued' || analysis.status === 'running') {
    return (
      <Badge variant="secondary" className="gap-1">
        <Loader2 className="size-3 animate-spin" />
        {analysis.status === 'queued' ? '等待识别' : '识别中'}
      </Badge>
    )
  }
  if (analysis.status === 'failed') {
    return <Badge variant="destructive" className="gap-1"><CircleAlert className="size-3" />识别失败</Badge>
  }
  if (analysis.status === 'cancelled' || analysis.status === 'superseded') {
    return <Badge variant="outline" className="gap-1"><PauseCircle className="size-3" />已停止</Badge>
  }
  return <Badge variant="outline" className="gap-1"><CircleCheck className="size-3" />识别完成</Badge>
}
