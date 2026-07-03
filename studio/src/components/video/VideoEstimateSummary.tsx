import { AlertCircle, CheckCircle2, Loader2 } from 'lucide-react'
import type { VideoEstimateResponse } from '@/types'

function formatCredits(value: number | undefined) {
  return typeof value === 'number' ? value.toLocaleString() : '—'
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
  return (
    <div className="space-y-2 rounded-lg border border-border bg-muted/20 px-3 py-2">
      <div className="grid gap-2 text-xs sm:grid-cols-3">
        <div>
          <p className="text-muted-foreground">预估费用</p>
          <p className="mt-0.5 text-sm font-semibold text-foreground">{formatCredits(estimate.estimated_credits)}</p>
        </div>
        <div>
          <p className="text-muted-foreground">当前余额</p>
          <p className="mt-0.5 text-sm font-semibold text-foreground">{formatCredits(estimate.balance)}</p>
        </div>
        <div>
          <p className="text-muted-foreground">创建后余额</p>
          <p className="mt-0.5 text-sm font-semibold text-foreground">{formatCredits(afterBalance)}</p>
        </div>
      </div>
      <div className={`flex items-start gap-2 rounded-md px-2 py-1.5 text-xs ${
        estimate.meets_min_balance ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'bg-destructive/10 text-destructive'
      }`}>
        {estimate.meets_min_balance ? <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" /> : <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
        <span>
          {estimate.meets_min_balance
            ? `余额满足视频创建门槛：至少 ${formatCredits(estimate.min_balance)} 积分。`
            : `视频任务需至少 ${formatCredits(estimate.min_balance)} 积分余额。`}
        </span>
      </div>
      {estimate.pricing_breakdown && (
        <p className="text-xs text-muted-foreground">
          {estimate.pricing_breakdown.model_key} · {estimate.pricing_breakdown.resolution} · {estimate.pricing_breakdown.ratio} · 输出 {estimate.pricing_breakdown.output_seconds}s
          {estimate.pricing_breakdown.input_video && typeof estimate.pricing_breakdown.input_seconds === 'number'
            ? ` · 输入视频 ${estimate.pricing_breakdown.input_seconds}s`
            : ''}
        </p>
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
