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
            model_name: 'DeepSeek 4 Pro',
            model_id: 'deepseek-v4-pro',
            description: '适合日常创作和批量任务，成本最低',
            min_tier: 'free',
            available: true,
          },
        ],
      },
    } as never)

    await expect(agentProfilesApi.list()).resolves.toEqual([
      expect.objectContaining({ id: 'cost_effective', available: true }),
    ])
    expect(get).toHaveBeenCalledWith('/agent/execution-profiles')
  })
})
