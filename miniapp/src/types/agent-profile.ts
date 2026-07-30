export type AgentExecutionProfileID = 'cost_effective' | 'balanced' | 'maximum_quality'

export interface AgentModelMatrix {
  default: string
  opus: string
  fable: string
  sonnet: string
  haiku: string
}

export interface AgentClaudeControls {
  effort_level?: 'low' | 'medium' | 'high' | 'max'
  always_enable_effort?: boolean
  max_context_tokens?: number
  max_output_tokens?: number
  max_thinking_tokens?: number
  disable_adaptive_thinking?: boolean
  disable_thinking?: boolean
  auto_compact_window?: number
  autocompact_pct_override?: number
  disable_1m_context?: boolean
  subagent_model?: string
  enable_tool_search?: boolean
}

export interface AgentExecutionProfileCapability {
  id: AgentExecutionProfileID
  display_name: string
  description: string
  provider: string
  protocol: 'anthropic'
  models: AgentModelMatrix
  claude: AgentClaudeControls
  min_tier: 'free' | 'pro' | 'enterprise'
  available: boolean
  unavailable_reason?: string
}

export interface AgentProfileSnapshot {
  schema_version: 2
  profile_id: AgentExecutionProfileID
  display_name: string
  provider: string
  protocol: 'anthropic'
  models: AgentModelMatrix
  claude: AgentClaudeControls
  model_usage_aliases: Record<string, string>
}
