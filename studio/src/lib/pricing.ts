import type { AgentExecutionProfileCapability, AgentExecutionProfileID, BillingCatalog } from '@/types'

export function taskCostFor(
  catalog: BillingCatalog | undefined,
  type: string,
  executionProfile?: AgentExecutionProfileID,
) {
  return catalog?.skus.find((sku) =>
    sku.charge_policy === 'task_admission'
      && sku.operation === `task.${type}`
      && (executionProfile === undefined || sku.execution_profile === executionProfile),
  )?.price_credits
}

function clockMinute(value: string) {
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  if (!match) return undefined
  const hour = Number(match[1])
  const minute = Number(match[2])
  if (hour > 24 || minute > 59 || (hour === 24 && minute !== 0)) return undefined
  return hour * 60 + minute
}

export function taskPeriodForTime(catalog: BillingCatalog | undefined, selectedTime?: string) {
  const rule = catalog?.task_time_pricing
  if (!rule) return undefined
  if (!selectedTime) return rule.current_period
  const minute = clockMinute(selectedTime)
  if (minute === undefined) return undefined
  return rule.peak_windows.some((window) => {
    const start = clockMinute(window.start)
    const end = clockMinute(window.end)
    return start !== undefined && end !== undefined && minute >= start && minute < end
  }) ? 'peak' : 'off_peak'
}

export function taskTimePriceInfo(
  catalog: BillingCatalog | undefined,
  type: string,
  executionProfile?: AgentExecutionProfileID,
  selectedTime?: string,
) {
  const sku = catalog?.skus.find((item) => item.charge_policy === 'task_admission'
    && item.operation === `task.${type}`
    && (executionProfile === undefined || item.execution_profile === executionProfile))
  const period = taskPeriodForTime(catalog, selectedTime)
  if (!sku || !period || sku.peak_price_credits === undefined || sku.off_peak_price_credits === undefined) return undefined
  const price = period === 'peak' ? sku.peak_price_credits : sku.off_peak_price_credits
  return { period, price, peakPrice: sku.peak_price_credits, offPeakPrice: sku.off_peak_price_credits, savings: sku.peak_price_credits - price }
}

export function formatTaskTimeWindows(catalog: BillingCatalog | undefined) {
  return catalog?.task_time_pricing?.off_peak_windows.map((window) => `${window.start}–${window.end}`).join('、')
}

export function executionProfilePriceInfo(
  catalog: BillingCatalog | undefined,
  type: string,
  profile: AgentExecutionProfileID,
  priceUnit: 'task' | 'run' = 'task',
) {
  const chargePolicy = priceUnit === 'run' ? 'task_admission' : 'task_admission'
  const price = catalog?.skus.find((sku) => sku.charge_policy === chargePolicy && sku.operation === `task.${type}` && sku.execution_profile === profile)?.price_credits
  const prices = catalog?.skus.filter((sku) => sku.charge_policy === chargePolicy && sku.operation === `task.${type}` && sku.price_credits > 0).map((sku) => sku.price_credits) ?? []
  const baseline = prices.length ? Math.min(...prices) : undefined
  return { price, multiplier: price !== undefined && baseline ? price / baseline : undefined }
}

export function cheapestAvailableExecutionProfile(
  profiles: AgentExecutionProfileCapability[] | undefined,
  catalog: BillingCatalog | undefined,
  type: string,
): AgentExecutionProfileID | undefined {
  let cheapest: { id: AgentExecutionProfileID; price: number } | undefined

  for (const profile of profiles ?? []) {
    if (!profile.available) continue
    const price = taskCostFor(catalog, type, profile.id)
    if (price === undefined) continue
    if (!cheapest || price < cheapest.price) cheapest = { id: profile.id, price }
  }

  return cheapest?.id
}

export function taskCostTotalFor(
  catalog: BillingCatalog | undefined,
  taskTypes: readonly string[],
  executionProfile: AgentExecutionProfileID,
): number | undefined {
  if (taskTypes.length === 0) return undefined
  let total = 0
  for (const taskType of taskTypes) {
    const price = taskCostFor(catalog, taskType, executionProfile)
    if (price === undefined) return undefined
    total += price
  }
  return total
}

export function cheapestAvailableExecutionProfileForTasks(
  profiles: AgentExecutionProfileCapability[] | undefined,
  catalog: BillingCatalog | undefined,
  taskTypes: readonly string[],
): AgentExecutionProfileID | undefined {
  let cheapest: { id: AgentExecutionProfileID; price: number } | undefined

  for (const profile of profiles ?? []) {
    if (!profile.available) continue
    const price = taskCostTotalFor(catalog, taskTypes, profile.id)
    if (price === undefined) continue
    if (!cheapest || price < cheapest.price) cheapest = { id: profile.id, price }
  }

  return cheapest?.id
}
