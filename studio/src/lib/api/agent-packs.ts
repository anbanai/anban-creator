import type { AgentPackCatalog } from '@/types'
import { http, unwrap } from '../http-client'

export const agentPacksApi = {
  list: () => unwrap<AgentPackCatalog>(http.get('/agent-packs')),
}
