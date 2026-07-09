import type { CreditPricing } from '@/types/credits'

export const DEFAULT_TASK_COSTS: Record<string, number> = {
  article: 4000,
  seednote: 3600,
  moments: 3000,
  ecommerce: 3000,
  montage: 2000,
  videocreator: 2000,
  videoeditor: 2000,
  viral_analysis: 1200,
}

export function taskCostFor(pricing: CreditPricing | undefined, type: string) {
  return pricing?.task_costs[type] ?? DEFAULT_TASK_COSTS[type] ?? 3600
}
