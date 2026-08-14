export const queryKeys = {
  agentPacks: {
    all: ['agent-packs'] as const,
  },
  auth: {
    me: ['auth', 'me'] as const,
  },
  projects: {
    all: ['projects'] as const,
    list: (filters?: { status?: string; platform?: string }) => ['projects', filters] as const,
    details: (filter?: string) => ['project-details', filter] as const,
    detail: (id: string) => ['project', id] as const,
    platformConfigs: ['platform-configs'] as const,
  },
  plans: {
    all: ['plans'] as const,
    list: (filters?: { offset?: number; limit?: number; project_id?: string }) => ['plans', filters] as const,
    detail: (id: string) => ['plan', id] as const,
  },
  tasks: {
    all: ['tasks'] as const,
    list: (filters?: { status?: string; project_id?: string }) => ['tasks', filters] as const,
    detail: (id: string) => ['task', id] as const,
    files: (id: string) => ['task-files', id] as const,
    seednoteAnalytics: (id: string) => ['task', id, 'seednote-analytics'] as const,
  },
  timeline: {
    range: (from: string, to: string, filters?: Record<string, string>) =>
      ['timeline', from, to, filters] as const,
  },
  billing: {
    all: ['billing'] as const,
    wallet: ['billing', 'wallet'] as const,
    catalog: ['billing', 'catalog'] as const,
    transactions: (page: number) => ['billing', 'transactions', page] as const,
  },
  apiKeys: {
    all: ['api-keys'] as const,
  },
  resources: {
    themes: ['resources', 'themes'] as const,
    writers: ['resources', 'writers'] as const,
    layouts: ['resources', 'layouts'] as const,
  },
  usage: {
    stats: (params?: { from?: string; to?: string; project_id?: string }) =>
      ['usage', 'stats', params] as const,
  },
  topicPool: {
    all: (projectId: string) => ['topic-pool', projectId] as const,
    list: (projectId: string, status?: string) => ['topic-pool', projectId, status] as const,
  },
  imageCapabilities: {
    all: ['image-capabilities'] as const,
  },
  agentProfiles: {
    all: ['agent', 'execution-profiles'] as const,
  },
  templates: {
    all: ['templates'] as const,
    list: (filters?: { type?: string; category?: string; scope?: string }) =>
      ['templates', filters] as const,
    detail: (id: string) => ['template', id] as const,
  },
} as const
