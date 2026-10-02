import { beforeEach, describe, expect, it, vi } from 'vitest'

import { http } from '@/lib/http-client'
import { channelsAnalyticsApi } from './channels-analytics'

vi.mock('@/lib/http-client', () => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
  },
  unwrap: async <T>(request: Promise<{ data: { data: T } }>) => (await request).data.data,
}))

describe('channelsAnalyticsApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses task-scoped analytics endpoints', async () => {
    vi.mocked(http.get).mockResolvedValueOnce({ data: { data: { series: [] } } })
    vi.mocked(http.post).mockResolvedValueOnce({ data: { data: { tracking: true } } })

    await expect(channelsAnalyticsApi.getByTask('task-1')).resolves.toEqual({ series: [] })
    await expect(channelsAnalyticsApi.bind('task-1', 'https://weixin.qq.com/sph/video')).resolves.toEqual({ tracking: true })

    expect(http.get).toHaveBeenCalledWith('/tasks/task-1/channels-analytics')
    expect(http.post).toHaveBeenCalledWith('/tasks/task-1/channels-analytics/bind', { video_url: 'https://weixin.qq.com/sph/video' })
  })
})
