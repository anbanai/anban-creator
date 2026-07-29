import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export function useAgentExecutionProfiles() {
  return useQuery({
    queryKey: queryKeys.agentProfiles.all,
    queryFn: () => api.agentProfiles.list(),
    staleTime: 60_000,
  })
}
