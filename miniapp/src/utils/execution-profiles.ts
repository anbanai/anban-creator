import type {
  AgentExecutionProfileCapability,
  AgentExecutionProfileID,
  BillingCatalog,
} from '@/types'

export function taskPriceForExecutionProfile(
  catalog: BillingCatalog | null | undefined,
  taskType: string,
  executionProfile: AgentExecutionProfileID,
): number | undefined {
  return catalog?.skus.find((sku) =>
    sku.charge_policy === 'task_admission'
      && sku.operation === `task.${taskType}`
      && sku.execution_profile === executionProfile,
  )?.price_credits
}

export function cheapestAvailableExecutionProfile(
  profiles: Pick<AgentExecutionProfileCapability, 'id' | 'available'>[] | null | undefined,
  catalog: BillingCatalog | null | undefined,
  taskType: string,
): AgentExecutionProfileID | undefined {
  let cheapest: { id: AgentExecutionProfileID; price: number } | undefined

  for (const profile of profiles ?? []) {
    if (!profile.available) continue
    const price = taskPriceForExecutionProfile(catalog, taskType, profile.id)
    if (price === undefined) continue
    if (!cheapest || price < cheapest.price) {
      cheapest = { id: profile.id, price }
    }
  }

  return cheapest?.id
}

export function resolveExecutionProfileSelection(
  current: AgentExecutionProfileID | '',
  preserveCurrent: boolean,
  profiles: Pick<AgentExecutionProfileCapability, 'id' | 'available'>[] | null | undefined,
  catalog: BillingCatalog | null | undefined,
  taskType: string,
): AgentExecutionProfileID | '' {
  if (current && preserveCurrent) return current

  const currentCapability = profiles?.find((profile) => profile.id === current)
  if (
    current
    && currentCapability?.available
    && taskPriceForExecutionProfile(catalog, taskType, current) !== undefined
  ) {
    return current
  }

  return cheapestAvailableExecutionProfile(profiles, catalog, taskType) || ''
}
