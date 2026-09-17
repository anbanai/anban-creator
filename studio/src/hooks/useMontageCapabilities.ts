import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { MontagePipelineCapability } from '@/types'

export function useMontageCapabilities(enabled = true) {
  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.montageCapabilities.all,
    queryFn: () => api.montageCapabilities.list(),
    enabled,
    staleTime: 5 * 60 * 1000,
  })

  const items: MontagePipelineCapability[] = data?.items ?? []

  return {
    items,
    enabled: data?.enabled ?? false,
    defaultPipeline: data?.default_pipeline ?? '',
    maxDurationSeconds: data?.max_duration_seconds ?? 0,
    maxAssets: data?.max_assets ?? 0,
    isLoading,
    isError,
    capabilityByKey: (key: string) => items.find((item) => item.key === key),
  }
}
