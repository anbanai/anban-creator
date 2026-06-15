export const queryKeys = {
  auth: {
    me: ['auth', 'me'] as const,
  },
  channels: {
    all: ['channels'] as const,
    list: (filters?: { status?: string; platform?: string }) => ['channels', filters] as const,
    details: (filter?: string) => ['channel-details', filter] as const,
    detail: (id: string) => ['channel', id] as const,
    platformConfigs: ['platform-configs'] as const,
    seednoteLoginStatus: ['seednote', 'login-status'] as const,
  },
  plans: {
    all: ['plans'] as const,
    list: (filters?: { offset?: number; limit?: number; channel_id?: string }) => ['plans', filters] as const,
    detail: (id: string) => ['plan', id] as const,
  },
  tasks: {
    all: ['tasks'] as const,
    list: (filters?: { status?: string; channel_id?: string }) => ['tasks', filters] as const,
    detail: (id: string) => ['task', id] as const,
    files: (id: string) => ['task-files', id] as const,
    seednoteAnalytics: (id: string) => ['task', id, 'seednote-analytics'] as const,
  },
  timeline: {
    range: (from: string, to: string, filters?: Record<string, string>) =>
      ['timeline', from, to, filters] as const,
  },
  credits: {
    all: ['credits'] as const,
    balance: ['credits', 'balance'] as const,
    signInStatus: ['credits', 'signInStatus'] as const,
    transactions: (page: number) => ['credits', 'transactions', page] as const,
  },
  apiKeys: {
    all: ['api-keys'] as const,
  },
  modelConfig: {
    all: ['model-config'] as const,
  },
  resources: {
    themes: ['resources', 'themes'] as const,
    writers: ['resources', 'writers'] as const,
    layouts: ['resources', 'layouts'] as const,
    imagePresets: ['resources', 'image_presets'] as const,
  },
  usage: {
    stats: (params?: { from?: string; to?: string; channel_id?: string }) =>
      ['usage', 'stats', params] as const,
  },
  topicPool: {
    all: (channelId: string) => ['topic-pool', channelId] as const,
    list: (channelId: string, status?: string) => ['topic-pool', channelId, status] as const,
  },
  designer: {
    all: ['designer'] as const,
    history: (params?: { channel_id?: string; page?: number; page_size?: number }) =>
      ['designer', 'history', params] as const,
    generation: (id: string) => ['designer', 'generation', id] as const,
  },
  imageModels: {
    all: ['image-models'] as const,
  },
} as const
