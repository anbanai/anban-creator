import { get } from './request'
import type { AgentExecutionProfileCapability } from '@/types'

export const agentProfilesApi = {
  list: () => get<AgentExecutionProfileCapability[]>('/agent/execution-profiles'),
}
