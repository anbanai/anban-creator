export type AgentExecutionProfileID = 'effective' | 'balanced' | 'quality'

export interface AgentExecutionProfileCapability {
  id: AgentExecutionProfileID
  display_name: string
  description: string
  provider: string
  model_name: string
  min_tier: 'free' | 'pro' | 'enterprise'
  available: boolean
  unavailable_reason?: string
}

export interface AgentProfileSnapshot {
  schema_version: 3
  profile_id: AgentExecutionProfileID
  display_name: string
  provider: string
  protocol: 'anthropic'
  envs: Record<string, string>
  model_usage_aliases: Record<string, string>
}
