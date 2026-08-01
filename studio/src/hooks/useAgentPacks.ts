import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export function useAgentPacks() {
  return useQuery({
    queryKey: queryKeys.agentPacks.all,
    queryFn: () => api.agentPacks.list(),
    staleTime: Number.POSITIVE_INFINITY,
  })
}
