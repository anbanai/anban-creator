export type AgentPackSurface = 'plugin' | 'project' | 'task' | 'plan'

export interface AgentPackJSONSchema {
  type?: 'object' | 'array' | 'string' | 'boolean' | 'number' | 'integer'
  title?: string
  description?: string
  format?: string
  default?: unknown
  required?: string[]
  properties?: Record<string, AgentPackJSONSchema>
  additionalProperties?: boolean
  enum?: Array<string | number | boolean>
  items?: AgentPackJSONSchema
  minimum?: number
  maximum?: number
}

export interface AgentPack {
  id: string
  version: string
  kind: 'plugin' | 'managed'
  display_name: string
  description: string
  agent: {
    name: string
    skills?: string[]
    max_turns?: number
  }
  bindings: {
    project_platforms?: string[]
    task_types?: string[]
  }
  runtime: {
    profile?: string
    adapter?: 'standard' | 'openmontage'
  }
  surfaces: AgentPackSurface[]
  features?: string[]
  billing_operations?: Record<string, string>
  schemas?: {
    project_config?: AgentPackJSONSchema
    task_input?: AgentPackJSONSchema
    ui?: AgentPackJSONSchema
    output?: AgentPackJSONSchema
  }
  ui?: { renderer?: string }
  digest: string
}

export interface AgentPackCatalog {
  packs: AgentPack[]
}
