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
