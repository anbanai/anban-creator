import { beforeEach, describe, expect, it, vi } from 'vitest'
import { topicRadarApi } from './topic-radar'

const { get, post, del } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), del: vi.fn() }))

vi.mock('@/lib/http-client', () => ({
  http: { get, post, delete: del },
  unwrap: async (request: Promise<{ data: { data: unknown } }>) => (await request).data.data,
}))

beforeEach(() => vi.clearAllMocks())

describe('topicRadarApi', () => {
  it('loads project subscriptions and digest history from the typed endpoints', async () => {
    get.mockResolvedValueOnce({ data: { data: { items: [{ id: 'sub-1' }] } } })
    get.mockResolvedValueOnce({ data: { data: { items: [{ id: 'digest-1' }] } } })

    await expect(topicRadarApi.subscriptions('project-1')).resolves.toEqual({ items: [{ id: 'sub-1' }] })
    await expect(topicRadarApi.digests('sub-1')).resolves.toEqual({ items: [{ id: 'digest-1' }] })
    expect(get).toHaveBeenNthCalledWith(1, '/projects/project-1/topic-radar/subscriptions')
    expect(get).toHaveBeenNthCalledWith(2, '/topic-radar/subscriptions/sub-1/digests')
  })

  it('creates a project subscription before starting a manual run', async () => {
    post.mockResolvedValueOnce({ data: { data: { id: 'sub-new' } } })
    await expect(topicRadarApi.createSubscription('project-1', { name: '选题雷达', enabled: false, timezone: 'Asia/Shanghai' })).resolves.toMatchObject({ id: 'sub-new' })
    expect(post).toHaveBeenCalledWith('/projects/project-1/topic-radar/subscriptions', { name: '选题雷达', enabled: false, timezone: 'Asia/Shanghai' })
  })

  it('deletes a subscription through the typed endpoint', async () => {
    del.mockResolvedValueOnce({ data: { data: { deleted: true } } })
    await expect(topicRadarApi.deleteSubscription('sub-1')).resolves.toEqual({ deleted: true })
    expect(del).toHaveBeenCalledWith('/topic-radar/subscriptions/sub-1')
  })

  it('starts a manual run and requests a script with explicit target settings', async () => {
    post.mockResolvedValueOnce({ data: { data: { id: 'digest-2', status: 'queued' } } })
    post.mockResolvedValueOnce({ data: { data: { id: 'job-1', status: 'queued' } } })

    await expect(topicRadarApi.run('sub-1', { request_key: 'request-1' })).resolves.toMatchObject({ id: 'digest-2' })
    await expect(topicRadarApi.requestScript('suggestion-1', {
      target_platform: 'douyin', duration_seconds: 45, aspect_ratio: '9:16',
    })).resolves.toMatchObject({ id: 'job-1' })
    expect(post).toHaveBeenNthCalledWith(1, '/topic-radar/subscriptions/sub-1/run', { request_key: 'request-1' })
    expect(post).toHaveBeenNthCalledWith(2, '/topic-radar/suggestions/suggestion-1/script', {
      target_platform: 'douyin', duration_seconds: 45, aspect_ratio: '9:16',
    })
  })
})
