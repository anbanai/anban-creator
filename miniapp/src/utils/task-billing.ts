import type { TaskBillingChargeDetail } from '../types/task'

interface TaskBillingSource {
  billing_charge_id?: string | null
  billing_sku_id?: string
  billing_price_credits?: number
  billing_total_credits?: number
  billing_pricing_tier?: 'free' | 'pro' | 'enterprise'
  billing_charge_details?: TaskBillingChargeDetail[]
  created_at?: string
}

export function taskBillingTotal(task: TaskBillingSource): number | undefined {
  return task.billing_total_credits ?? task.billing_price_credits
}

export function taskBillingDetails(task: TaskBillingSource): TaskBillingChargeDetail[] {
  if (task.billing_charge_details?.length) return task.billing_charge_details
  if (task.billing_price_credits === undefined) return []
  return [{
    id: task.billing_charge_id ?? undefined,
    charge_kind: 'task',
    policy: 'task_admission',
    sku_id: task.billing_sku_id,
    credits: task.billing_price_credits,
    pricing_tier: task.billing_pricing_tier,
    list_price_credits: task.billing_price_credits,
    resource_type: 'task',
    created_at: task.created_at,
  }]
}

export function taskBillingChargeLabel(detail: TaskBillingChargeDetail): string {
  const sku = detail.sku_id?.toLowerCase() ?? ''
  const resourceType = detail.resource_type?.toLowerCase() ?? ''
  let label = '增值操作费'

  if (detail.charge_kind === 'task' || detail.policy === 'task_admission' || resourceType === 'task') {
    label = '任务固定费'
  } else if (sku.includes('image.seedream.cover') || sku.includes('image.cover')) {
    label = '封面图生成费'
  } else if (sku.includes('image.seedream.content') || sku.includes('image.content')) {
    label = '内容图生成费'
  } else if (sku.includes('image') || resourceType === 'image') {
    label = '图片生成费'
  } else if (sku.includes('analysis') || resourceType === 'analysis') {
    label = '内容分析费'
  } else if (sku.includes('video') || resourceType === 'video') {
    label = '视频处理费'
  } else if (sku.includes('audio') || resourceType === 'audio') {
    label = '音频处理费'
  }

  return detail.charge_kind === 'reversal' ? `${label}退回` : label
}

export function taskBillingIdentity(detail: TaskBillingChargeDetail): string {
  return [detail.sku_id, detail.tool_call_id].filter(Boolean).join(' · ') || '固定 SKU'
}

export function taskBillingPricingEvidence(detail: TaskBillingChargeDetail): string {
  const tierLabels = { free: '免费版', pro: '专业版', enterprise: '企业版' } as const
  const tier = detail.pricing_tier ? tierLabels[detail.pricing_tier] : ''
  const discount = detail.discount_credits ?? 0
  const pricing = discount > 0
    ? `标准价 ${(detail.list_price_credits ?? Math.abs(detail.credits)).toLocaleString()}，优惠 ${discount.toLocaleString()}`
    : ''
  return [tier, pricing].filter(Boolean).join(' · ')
}

export function taskBillingAmountLabel(detail: TaskBillingChargeDetail): string {
  if (detail.charge_kind === 'reversal' || detail.credits < 0) {
    return `退回 ${Math.abs(detail.credits).toLocaleString()} 积分`
  }
  return `${detail.credits.toLocaleString()} 积分`
}
