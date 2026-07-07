import { AlertCircle, CheckCircle2, Loader2 } from 'lucide-react'
import { videoModelDisplayName } from '@/lib/video-display'
import type { VideoEstimateResponse } from '@/types'

function formatCredits(value: number | undefined) {
  return typeof value === 'number' ? value.toLocaleString() : '—'
}

function joinList(values: string[] | undefined) {
  return values && values.length > 0 ? values.join('、') : '—'
}

export function VideoEstimateSummary({
  estimate,
  isLoading,
  error,
}: {
  estimate?: VideoEstimateResponse
  isLoading?: boolean
  error?: string
}) {
  if (isLoading) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        正在创建前自动校验参数和费用...
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
        <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        <span>{error}</span>
      </div>
    )
  }

  if (!estimate) {
    return (
      <div className="rounded-lg border border-dashed border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
        选择视频项目后会自动估算费用、余额门槛和模型可用性。
      </div>
    )
  }

  const afterBalance = estimate.balance - estimate.estimated_credits
  const canCoverEstimate = estimate.balance >= estimate.estimated_credits
  return (
    <div className="space-y-2 rounded-lg border border-border bg-muted/20 px-3 py-2">
      <div className="grid gap-2 text-xs sm:grid-cols-3">
        <div>
          <p className="text-muted-foreground">后续视频生成操作费</p>
          <p className="mt-0.5 text-sm font-semibold text-foreground">{formatCredits(estimate.estimated_credits)}</p>
        </div>
        <div>
          <p className="text-muted-foreground">当前余额</p>
          <p className="mt-0.5 text-sm font-semibold text-foreground">{formatCredits(estimate.balance)}</p>
        </div>
        <div>
          <p className="text-muted-foreground">预计生成后余额</p>
          <p className="mt-0.5 text-sm font-semibold text-foreground">{formatCredits(afterBalance)}</p>
        </div>
      </div>
      <div className={`flex items-start gap-2 rounded-md px-2 py-1.5 text-xs ${
        canCoverEstimate ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'bg-destructive/10 text-destructive'
      }`}>
        {canCoverEstimate ? <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" /> : <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
        <span>
          {canCoverEstimate
            ? '余额可覆盖当前视频生成预估；实际在 MCP video_gen 提交时扣除。'
            : '当前余额不足以覆盖当前视频生成预估；任务可创建，执行到 video_gen 时会提示充值。'}
        </span>
      </div>
      {estimate.pricing_breakdown && (
        <p className="text-xs text-muted-foreground">
          {videoModelDisplayName(estimate.pricing_breakdown.model_key)} · {estimate.pricing_breakdown.resolution} · {estimate.pricing_breakdown.ratio} · 输出 {estimate.pricing_breakdown.output_seconds}s
          {estimate.pricing_breakdown.input_video && typeof estimate.pricing_breakdown.input_seconds === 'number'
            ? ` · 输入视频 ${estimate.pricing_breakdown.input_seconds}s`
            : ''}
        </p>
      )}
      {(estimate.missing_reference_roles?.length || estimate.segment_plan?.length || estimate.expected_artifacts?.length || typeof estimate.affordable_takes === 'number') && (
        <div className="grid gap-2 border-t border-border pt-2 text-xs sm:grid-cols-2">
          {estimate.missing_reference_roles && estimate.missing_reference_roles.length > 0 && (
            <div>
              <p className="font-medium text-foreground">缺少参考角色</p>
              <p className="mt-0.5 text-muted-foreground">{joinList(estimate.missing_reference_roles)}</p>
            </div>
          )}
          {estimate.segment_plan && estimate.segment_plan.length > 0 && (
            <div>
              <p className="font-medium text-foreground">{estimate.segment_plan.length} 段生成计划</p>
              <p className="mt-0.5 text-muted-foreground">
                {estimate.segment_plan.map((segment) => `${segment.index}:${segment.duration}s`).join('、')}
              </p>
            </div>
          )}
          {typeof estimate.affordable_takes === 'number' && estimate.affordable_takes > 0 && (
            <div>
              <p className="font-medium text-foreground">预算内约 {estimate.affordable_takes} 次 take</p>
              <p className="mt-0.5 text-muted-foreground">包含首轮生成和可承受返修试拍。</p>
            </div>
          )}
          {estimate.expected_artifacts && estimate.expected_artifacts.length > 0 && (
            <div>
              <p className="font-medium text-foreground">预期产物</p>
              <p className="mt-0.5 line-clamp-2 text-muted-foreground">{joinList(estimate.expected_artifacts)}</p>
            </div>
          )}
        </div>
      )}
      {estimate.warnings && estimate.warnings.length > 0 && (
        <div className="space-y-1 text-xs text-amber-600 dark:text-amber-300">
          {estimate.warnings.map((warning) => (
            <p key={warning}>{warning}</p>
          ))}
        </div>
      )}
    </div>
  )
}
