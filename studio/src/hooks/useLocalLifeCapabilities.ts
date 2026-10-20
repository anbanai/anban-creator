import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { LocalLifeMixType } from '@/types'

export function useLocalLifeCapabilities(enabled = true) {
  const query = useQuery({
    queryKey: queryKeys.localLifeCapabilities.all,
    queryFn: () => api.localLifeCapabilities.list(),
    enabled,
    staleTime: 5 * 60 * 1000,
  })
  const types: LocalLifeMixType[] = query.data?.types ?? []
  return {
    ...query,
    types,
    enabled: query.data?.enabled ?? false,
    defaultMixType: query.data?.default_mix_type ?? 'general-store',
    defaultVariantCount: query.data?.default_variant_count ?? 3,
    maxVariantCount: query.data?.max_variant_count ?? 10,
    defaultDurationSeconds: query.data?.default_duration_seconds ?? 30,
    maxDurationSeconds: query.data?.max_duration_seconds ?? 60,
    aspectRatios: query.data?.aspect_ratios ?? ['9:16', '16:9', '1:1'],
    typeByID: (id: string) => types.find((item) => item.id === id),
  }
}
