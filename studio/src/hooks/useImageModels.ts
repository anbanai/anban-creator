import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { ImageModelOption } from '@/types'

export function useImageModels() {
  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.imageModels.all,
    queryFn: () => api.imageModels.list(),
    staleTime: 5 * 60 * 1000,
  })

  const items: ImageModelOption[] = data?.items ?? []
  const tier = data?.tier ?? ''
  const nameByKey = (key: string): string => {
    const found = items.find((opt) => opt.key === key)
    return found?.display_name ?? key
  }

  return { items, tier, isLoading, isError, nameByKey }
}
