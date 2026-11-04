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
  const availableItems = items.filter((item) => item.available !== false && item.key !== 'framework-smoke')

  return {
    items,
    availableItems,
    styles: data?.styles ?? [],
    outputProfiles: data?.output_profiles ?? [],
    defaultOutputProfile: data?.default_output_profile ?? 'tiktok',
    enabled: data?.enabled ?? false,
    defaultPipeline: data?.default_pipeline ?? '',
    maxDurationSeconds: data?.max_duration_seconds ?? 0,
    maxAssets: data?.max_assets ?? 0,
    isLoading,
    isError,
    capabilityByKey: (key: string) => items.find((item) => item.key === key),
  }
}
