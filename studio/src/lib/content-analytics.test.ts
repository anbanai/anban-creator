import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { loadContentAnalytics } from './content-analytics'
import type { AnalyticsPeriod } from './analytics-period'
import type { Project } from '@/types'
vi.mock('@/lib/api', () => ({ api: { wechatAnalyticsImport: { articles: vi.fn() }, seednoteImport: { overview: vi.fn(), posts: vi.fn(), post: vi.fn(), listBatches: vi.fn() } } }))
const period: AnalyticsPeriod = { preset: 'custom', from: '2026-09-01', to: '2026-09-30', granularity: 'month' }
const article = { id: 'p1', platform: 'article' } as Project
beforeEach(() => vi.clearAllMocks())
describe('统一看板平台适配', () => {
  it('supports task-only Wechat records and uses latest snapshot instead of summing cumulative snapshots', async () => {
    vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [{ target: { kind: 'task', id: 't1' }, task: { id: 't1', title: '草稿内容', status: 'pending', created_at: '' }, publication: null, content_type: 'image', snapshots: [100, 250].map((n, i) => ({ id: `s${i}`, publication_id: '', data_as_of_at: `2026-09-0${i + 1}T12:00:00Z`, imported_at: '2026-09-24T12:00:00Z', read_users: n })) }] })
    const data = await loadContentAnalytics(article, period, 'task:t1')
    expect(data.selected?.title).toBe('草稿内容')
    expect(data.selected?.status).toBe('pending')
    expect(data.totals.read_users).toBe(250)
    expect(data.series[0].read_users).toBe(250)
    expect(data.totals.share_users).toBeNull()
  })
  it('missing selected content cannot fall back to whole-account metrics', async () => {
    vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [] })
    expect((await loadContentAnalytics(article, period, 'task:missing')).totals.read_users).toBeNull()
  })
  it('compares timestamp instants across offsets when choosing latest Wechat data', async () => {
    vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [{ target: { kind: 'task', id: 't1' }, task: { id: 't1', title: '内容', status: 'pending', created_at: '' }, publication: null, snapshots: [
      { id: 'older', publication_id: '', data_as_of_at: '2026-09-24T10:00:00+08:00', imported_at: '2026-09-24T10:00:00+08:00', read_users: 10 },
      { id: 'newer', publication_id: '', data_as_of_at: '2026-09-24T03:00:00Z', imported_at: '2026-09-24T03:00:00Z', read_users: 20 },
    ] }] })
    expect((await loadContentAnalytics(article, period)).totals.read_users).toBe(20)
  })
  it('deduplicates same-day Seednote versions, sums days and preserves missing fields', async () => {
    const project = { id: 'p2', platform: 'seednote' } as Project
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], posts: [], post_summaries: [], series: [] })
    vi.mocked(api.seednoteImport.posts).mockResolvedValue({ items: [{ id: 'n1', title: '笔记', genre: '图文' }], total: 1 })
    vi.mocked(api.seednoteImport.listBatches).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.seednoteImport.post).mockResolvedValue({ post: { id: 'n1', title: '笔记', genre: '图文' }, versions: [
      { id: 'a', data_as_of_at: '2026-09-01T12:00:00Z', imported_at: '2026-09-03T10:00:00+08:00', view_count: 20 },
      { id: 'b', data_as_of_at: '2026-09-01T12:00:00Z', imported_at: '2026-09-03T03:00:00Z', view_count: 30 },
      { id: 'c', data_as_of_at: '2026-09-02T12:00:00Z', imported_at: '2026-09-03T12:00:00Z', view_count: 40 },
    ] })
    const data = await loadContentAnalytics(project, period, 'seednote_post:n1')
    expect(data.totals.view_count).toBe(70)
    expect(data.series[0].view_count).toBe(70)
    expect(data.totals.collect_count).toBeNull()
    expect(data.selected?.contentType).toBe('image_text')
  })
})
