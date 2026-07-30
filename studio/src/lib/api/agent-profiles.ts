import { http, unwrap } from '@/lib/http-client'
import { agentExecutionProfileCapabilitiesSchema } from '@/lib/schemas'
import type { AgentExecutionProfileCapability } from '@/types'

export const agentProfilesApi = {
  list: async (): Promise<AgentExecutionProfileCapability[]> => {
    const response = await unwrap<unknown>(http.get('/agent/execution-profiles'))
    return agentExecutionProfileCapabilitiesSchema.parse(response)
  },
}
