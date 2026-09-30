import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import { contentAnalyticsApi, parseTargetKey, taskContentAnalyticsHref } from './content-analytics'
vi.mock('@/lib/http-client', () => ({ http: { get: vi.fn(), post: vi.fn() }, unwrap: async (request: Promise<{ data: { data: unknown } }>) => (await request).data.data }))
const params = { from: '2026-09-01', to: '2026-09-30', granularity: 'month' as const, metric_basis: 'cumulative' as const }
beforeEach(() => { vi.clearAllMocks(); vi.mocked(http.get).mockResolvedValue({ data: { data: { revision: 7, totals: { views: null }, items: [], total: 0 } } }) })
describe('bounded analytics API', () => {
  it('reads a detail alias directly without downloading account content', async () => {
    const signal = new AbortController().signal
    const result = await contentAnalyticsApi.detail('p1', 'task:t1', params, signal)
    expect(result.totals.views).toBeNull()
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/projects/p1/content-analytics/contents/task%3At1', { params, signal })
  })
  it('forwards pagination, filters, revision and cancellation to the server', async () => {
    const signal = new AbortController().signal
    const request = { ...params, search: '标题', offset: 25, limit: 25, expected_revision: 7, sort: 'view_count', direction: 'desc' as const }
    await contentAnalyticsApi.contents('p1', request, signal)
    expect(http.get).toHaveBeenCalledWith('/projects/p1/content-analytics/contents', { params: request, signal })
  })
  it('requests available dates by year without fetching batches', async () => {
    await contentAnalyticsApi.dates('p1', { year: 2026, metric_basis: 'cumulative' })
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/projects/p1/content-analytics/dates', { params: { year: 2026, metric_basis: 'cumulative' }, signal: undefined })
  })
  it('retains the business task deep-link contract', () => {
    expect(taskContentAnalyticsHref('p1', 't1')).toBe('/content-analytics?account=p1&content=task%3At1')
    expect(parseTargetKey('task:t1')).toEqual({ kind: 'task', id: 't1' })
    expect(parseTargetKey('unknown:t1')).toBeUndefined()
  })
})
