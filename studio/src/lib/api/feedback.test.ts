import { beforeEach, describe, expect, it, vi } from 'vitest'

import { http } from '@/lib/http-client'
import { feedbackApi } from './feedback'

vi.mock('@/lib/http-client', () => ({
  http: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
  unwrap: async <T>(request: Promise<{ data: { data: T } }>) => (await request).data.data,
}))

describe('feedbackApi task feedback', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses task-scoped GET and PUT endpoints', async () => {
    vi.mocked(http.get).mockResolvedValueOnce({ data: { data: null } })
    vi.mocked(http.put).mockResolvedValueOnce({ data: { data: { id: 'feedback-1', rating: 5 } } })

    await expect(feedbackApi.getTask('task-1')).resolves.toBeNull()
    await expect(feedbackApi.saveTask('task-1', { rating: 5, content: '很好' })).resolves.toEqual({ id: 'feedback-1', rating: 5 })
    expect(http.get).toHaveBeenCalledWith('/tasks/task-1/feedback')
    expect(http.put).toHaveBeenCalledWith('/tasks/task-1/feedback', { rating: 5, content: '很好' })
  })
})
