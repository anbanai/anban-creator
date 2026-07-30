import { afterEach, describe, expect, it, vi } from 'vitest'

import { http } from '@/lib/http-client'
import { agentProfilesApi } from './agent-profiles'

describe('agentProfilesApi', () => {
  afterEach(() => vi.restoreAllMocks())

  it('loads the authenticated service-driven execution profile catalog', async () => {
    const get = vi.spyOn(http, 'get').mockResolvedValue({
      data: {
        data: [
          {
            id: 'cost_effective',
            display_name: '性价比',
            provider: 'deepseek',
            protocol: 'anthropic',
            models: {
              default: 'deepseek-v4-flash',
              opus: 'deepseek-v4-pro',
              fable: 'deepseek-v4-flash',
              sonnet: 'deepseek-v4-pro',
              haiku: 'deepseek-v4-flash',
            },
            claude: { effort_level: 'medium' },
            description: '适合日常创作和批量任务，成本最低',
            min_tier: 'free',
            available: true,
          },
        ],
      },
    } as never)

    await expect(agentProfilesApi.list()).resolves.toEqual([
      expect.objectContaining({
        id: 'cost_effective',
        provider: 'deepseek',
        models: expect.objectContaining({ default: 'deepseek-v4-flash', opus: 'deepseek-v4-pro' }),
      }),
    ])
    expect(get).toHaveBeenCalledWith('/agent/execution-profiles')
  })

  it('rejects the removed single-model capability contract', async () => {
    vi.spyOn(http, 'get').mockResolvedValue({
      data: {
        data: [{
          id: 'cost_effective',
          display_name: '性价比',
          model_name: 'Legacy model',
          model_id: 'legacy-model',
          description: 'obsolete',
          min_tier: 'free',
          available: true,
        }],
      },
    } as never)

    await expect(agentProfilesApi.list()).rejects.toThrow()
  })
})
