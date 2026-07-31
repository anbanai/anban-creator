import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { ImageCapabilityOption } from '@/types'

export function useImageCapabilities() {
  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.imageModels.all,
    queryFn: () => api.imageModels.list(),
    staleTime: 5 * 60 * 1000,
  })

  const items: ImageCapabilityOption[] = data?.items ?? []
  const tier = data?.tier ?? ''
  const nameByKey = (key: string): string => {
    const found = items.find((opt) => opt.key === key)
    return found?.display_name ?? key
  }

  const descriptionByKey = (key: string): string | undefined => items.find((opt) => opt.key === key)?.description
  const priceByKey = (key: string): number | undefined => items.find((opt) => opt.key === key)?.price_credits

  return { items, tier, isLoading, isError, nameByKey, descriptionByKey, priceByKey }
}

export const useImageModels = useImageCapabilities
