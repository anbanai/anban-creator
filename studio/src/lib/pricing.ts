import type { BillingCatalog } from '@/types'

export function taskCostFor(catalog: BillingCatalog | undefined, type: string) {
  return catalog?.skus.find((sku) =>
    sku.charge_policy === 'task_admission' && sku.operation === `task.${type}`,
  )?.price_credits
}
