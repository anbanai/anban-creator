import { http, unwrap } from '@/lib/http-client'
import type { AgentExecutionProfileCapability } from '@/types'

export const agentProfilesApi = {
  list: () =>
    unwrap<AgentExecutionProfileCapability[]>(http.get('/agent/execution-profiles')),
}
