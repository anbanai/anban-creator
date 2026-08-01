import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { ImageCapabilityOption } from '@/types'

export function useImageCapabilities(enabled = true) {
  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.imageCapabilities.all,
    queryFn: () => api.imageCapabilities.list(),
    enabled,
    staleTime: 5 * 60 * 1000,
  })

  const items: ImageCapabilityOption[] = data?.items ?? []
  const tier = data?.tier ?? ''
  const defaultCapability = data?.default_capability ?? ''
  const nameByKey = (key: string): string => items.find((option) => option.key === key)?.display_name ?? key
  const descriptionByKey = (key: string): string | undefined => items.find((option) => option.key === key)?.description
  const priceByKey = (key: string): number | undefined => items.find((option) => option.key === key)?.price_credits

  return { items, tier, defaultCapability, isLoading, isError, nameByKey, descriptionByKey, priceByKey }
}
