import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export function useAgentPacks() {
  return useQuery({
    queryKey: queryKeys.agentPacks.all,
    queryFn: async () => {
      const catalog = await api.agentPacks.list()
      return { packs: Array.isArray(catalog?.packs) ? catalog.packs : [] }
    },
    staleTime: Number.POSITIVE_INFINITY,
  })
}
