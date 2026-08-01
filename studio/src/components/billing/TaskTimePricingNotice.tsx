import type { AgentExecutionProfileID, BillingCatalog } from '@/types'
import { formatTaskTimeWindows, taskTimePriceInfo } from '@/lib/pricing'
import { cn } from '@/lib/utils'

interface Props {
  catalog?: BillingCatalog
  taskType: string
  executionProfile?: AgentExecutionProfileID
  selectedTime?: string
  recommendationUnavailable?: boolean
}

export function TaskTimePricingNotice({ catalog, taskType, executionProfile, selectedTime, recommendationUnavailable }: Props) {
  const rule = catalog?.task_time_pricing
  const price = taskTimePriceInfo(catalog, taskType, executionProfile, selectedTime)
  if (!rule) return null
  const offPeakWindows = formatTaskTimeWindows(catalog)
  return (
    <div className={cn('space-y-1 rounded-lg border px-3 py-2 text-xs', price?.period === 'peak' ? 'border-amber-500/50 bg-amber-500/10' : 'border-emerald-500/40 bg-emerald-500/10')}>
      <p className="font-medium">
        {price?.period === 'peak' ? '当前选择为高峰时段' : '当前选择为低峰时段'}
        {price ? ` · 预计 ${price.price} 积分/次` : ''}
      </p>
      <p className="text-muted-foreground">
        {rule.timezone} 低峰：{offPeakWindows}；低峰价格为高峰会员价的 {rule.off_peak_rate_percent}%
      </p>
      {price ? <p className="text-muted-foreground">高峰 {price.peakPrice} 积分 · 低峰 {price.offPeakPrice} 积分 · 低峰可节省 {price.peakPrice - price.offPeakPrice} 积分</p> : null}
      {recommendationUnavailable ? <p className="font-medium text-amber-700 dark:text-amber-300">智能分布暂不可用，已使用当前配置中的低峰时间。</p> : null}
    </div>
  )
}
