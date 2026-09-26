import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { loadContentAnalytics, parseTargetKey, taskContentDataHref } from './content-analytics'
import type { AnalyticsPeriod } from './analytics-period'
import type { Project } from '@/types'
vi.mock('@/lib/api', () => ({ api: { wechatAnalyticsImport: { articles: vi.fn() }, seednoteImport: { overview: vi.fn(), posts: vi.fn(), post: vi.fn(), listBatches: vi.fn() } } }))
const period: AnalyticsPeriod = { preset: 'custom', from: '2026-09-01', to: '2026-09-30', granularity: 'month' }
const article = { id: 'p1', platform: 'article' } as Project
beforeEach(() => vi.clearAllMocks())
describe('内容数据深链契约', () => {
  it('builds a task-scoped content data URL and parses target keys back', () => {
    expect(taskContentDataHref('p1', 't1')).toBe('/content-data?account=p1&content=task%3At1')
    expect(parseTargetKey('task:t1')).toEqual({ kind: 'task', id: 't1' })
    expect(parseTargetKey('seednote_post:n1')).toEqual({ kind: 'seednote_post', id: 'n1' })
  })
  it('rejects values that are not a known analytics target', () => {
    for (const value of [undefined, '', 'task', 'task:', 'unknown:x', ':t1']) expect(parseTargetKey(value)).toBeUndefined()
  })
})
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
  it('resolves a task target to the Seednote post it owns', async () => {
    const project = { id: 'p2', platform: 'seednote' } as Project
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], posts: [{ id: 'n1', title: '笔记', genre: '图文', task_id: 't1' }], post_summaries: [], series: [] })
    vi.mocked(api.seednoteImport.posts).mockResolvedValue({ items: [{ id: 'n1', title: '笔记', genre: '图文', task_id: 't1' }], total: 1 })
    vi.mocked(api.seednoteImport.listBatches).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.seednoteImport.post).mockResolvedValue({ post: { id: 'n1', title: '笔记', genre: '图文', task_id: 't1' }, versions: [{ id: 'a', data_as_of_at: '2026-09-01T12:00:00Z', imported_at: '2026-09-03T10:00:00Z', view_count: 12 }] })
    const data = await loadContentAnalytics(project, period, 'task:t1')
    expect(api.seednoteImport.post).toHaveBeenCalledWith('p2', 'n1', expect.anything())
    expect(data.selected?.id).toBe('seednote_post:n1')
    expect(data.totals.view_count).toBe(12)
  })
  it('keeps the most recently published Seednote post addressable when a task owns several', async () => {
    const project = { id: 'p2', platform: 'seednote' } as Project
    const owned = [
      { id: 'n1', title: '旧笔记', genre: '图文', task_id: 't1', first_published_at: '2026-01-01T00:00:00Z' },
      { id: 'n2', title: '新笔记', genre: '图文', task_id: 't1', first_published_at: '2026-05-01T00:00:00Z' },
    ]
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], posts: owned, post_summaries: [], series: [] })
    vi.mocked(api.seednoteImport.posts).mockResolvedValue({ items: owned, total: 2 })
    vi.mocked(api.seednoteImport.listBatches).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.seednoteImport.post).mockResolvedValue({ post: owned[1], versions: [{ id: 'a', data_as_of_at: '2026-09-01T12:00:00Z', imported_at: '2026-09-03T10:00:00Z', view_count: 5 }] })
    const data = await loadContentAnalytics(project, period, 'task:t1')
    expect(api.seednoteImport.post).toHaveBeenCalledWith('p2', 'n2', expect.anything())
    expect(data.selected?.title).toBe('新笔记')
    expect(data.contents.map((item) => item.id)).toEqual(['seednote_post:n1', 'seednote_post:n2'])
  })
  it('never falls back to whole-account metrics when a task target owns no post', async () => {
    const project = { id: 'p2', platform: 'seednote' } as Project
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], posts: [{ id: 'n1', title: '笔记', genre: '图文' }], post_summaries: [], series: [{ date: '2026-09-01', exposure_count: 1, view_count: 99, like_count: null, comment_count: null, collect_count: null, follower_gain_count: null, share_count: null, barrage_count: null }] })
    vi.mocked(api.seednoteImport.posts).mockResolvedValue({ items: [{ id: 'n1', title: '笔记', genre: '图文' }], total: 1 })
    vi.mocked(api.seednoteImport.listBatches).mockResolvedValue({ items: [], total: 0 })
    const data = await loadContentAnalytics(project, period, 'task:missing')
    expect(api.seednoteImport.post).not.toHaveBeenCalled()
    expect(data.selected).toBeUndefined()
    expect(data.totals.view_count).toBeNull()
  })
})
