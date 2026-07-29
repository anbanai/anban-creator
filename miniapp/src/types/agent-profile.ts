export type AgentExecutionProfileID = 'cost_effective' | 'balanced' | 'maximum_quality'

export interface AgentExecutionProfileCapability {
  id: AgentExecutionProfileID
  display_name: string
  model_name: string
  model_id: string
  description: string
  min_tier: 'free' | 'pro' | 'enterprise'
  available: boolean
  unavailable_reason?: string
}

export interface AgentProfileSnapshot {
  profile_id: AgentExecutionProfileID
  provider: string
  model_id: string
  protocol: 'anthropic'
  context_window?: number
  reasoning_effort?: 'low' | 'medium' | 'high'
  thinking_required?: boolean
  display_name: string
}
