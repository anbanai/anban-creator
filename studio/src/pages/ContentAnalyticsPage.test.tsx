import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import type { Project } from '@/types'
import ContentAnalyticsPage from './ContentAnalyticsPage'
import { contentAnalyticsApi } from '@/lib/content-analytics'
import { wechatAccountAnalyticsApi } from '@/lib/wechat-account-analytics'
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'user-1' } }) }))
vi.mock('@/lib/api', () => ({ api: { projects: { list: vi.fn(async () => [{ id: 'notes', platform: 'seednote', name: '生活笔记', agent_ids: ['seednote'] }]) } } }))
vi.mock('@/components/analytics/AnalyticsTrend', () => ({ default: () => <div>趋势</div> }))
vi.mock('@/components/analytics/ContentImportDialog', () => ({ default: () => null }))
vi.mock('@/components/analytics/ContentImportHistory', () => ({ default: () => null }))
vi.mock('@/lib/content-analytics', async importOriginal => ({ ...await importOriginal<typeof import('@/lib/content-analytics')>(), contentAnalyticsApi: { overview: vi.fn(), contents: vi.fn(), detail: vi.fn(), dates: vi.fn(), observations: vi.fn() } }))
vi.mock('@/lib/wechat-account-analytics', async importOriginal => ({ ...await importOriginal<typeof import('@/lib/wechat-account-analytics')>(), wechatAccountAnalyticsApi: { overview: vi.fn(), trafficSources: vi.fn() } }))
const content = { id: 'canonical-1', title: '服务器内容', content_type: 'image_text', tags: [{ dimension: 'source_relation', value: 'hot_search', display_name: '热搜' }, { dimension: 'performance', value: 'viral', display_name: '爆款' }], metrics: { view_count: 12 } }
const summary = { revision: 4, updated_at: '', metric_basis: 'cumulative' as const, totals: { view_count: 12 }, series: [], unavailable_metrics: {} }
beforeEach(() => {
  vi.mocked(api.projects.list).mockResolvedValue([{ id: 'notes', platform: 'seednote', name: '生活笔记', agent_ids: ['seednote'] } as Project])
  vi.clearAllMocks(); localStorage.clear(); window.history.replaceState(null, '', '/content-analytics?account=notes')
  vi.mocked(contentAnalyticsApi.overview).mockResolvedValue({ ...summary, coverage: { contents: 51 } })
  vi.mocked(contentAnalyticsApi.contents).mockResolvedValue({ revision: 4, items: [content], total: 51, offset: 0, limit: 25 })
  vi.mocked(contentAnalyticsApi.detail).mockResolvedValue({ ...summary, content })
  vi.mocked(contentAnalyticsApi.dates).mockResolvedValue({ revision: 4, dates: ['2026-09-24'] })
  vi.mocked(contentAnalyticsApi.observations).mockResolvedValue({ revision: 4, items: [], total: 0, offset: 0, limit: 25 })
  vi.mocked(wechatAccountAnalyticsApi.overview).mockResolvedValue({ revision: 8, from: '2026-09-01', to: '2026-09-30', data_through: '2026-09-30', covered_dates: [], coverage: { covered_days: 0, expected_days: 30, complete: false }, series: [], totals: {}, comparisons: {}, unavailable_metrics: {} })
  vi.mocked(wechatAccountAnalyticsApi.trafficSources).mockResolvedValue({ revision: 8, from: '2026-09-01', to: '2026-09-30', metric: 'read_users', total: null, coverage: { covered_days: 0, expected_days: 30, complete: false }, covered_dates: [], data_quality: null, series: [], items: [] })
})
describe('server analytics queries', () => {
  it('exposes icon-only import actions with accessible names', async () => {
    render(<ContentAnalyticsPage />)

    expect(await screen.findByRole('button', { name: '查看导入记录' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '导入分析数据' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '查看导入记录' })).not.toHaveTextContent('导入记录')
    expect(screen.getByRole('button', { name: '导入分析数据' })).not.toHaveTextContent('导入分析数据')
  })

  it('separates cumulative content imports from account-day imports for WeChat', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{ id: 'wechat', platform: 'wechat', name: '公众号项目', agent_ids: ['wechat-article'] } as Project])
    window.history.replaceState(null, '', '/content-analytics?account=wechat&platform=wechat')
    render(<ContentAnalyticsPage />)

    expect((await screen.findAllByRole('button', { name: '导入公众号内容累计数据' })).length).toBeGreaterThanOrEqual(1)
    expect(screen.getByRole('button', { name: '导入公众号账号统计' })).toBeInTheDocument()
  })

  it('shows cumulative content totals alongside account daily analytics', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{ id: 'wechat', platform: 'wechat', name: '公众号项目', agent_ids: ['wechat-article'] } as Project])
    vi.mocked(contentAnalyticsApi.overview).mockResolvedValue({
      ...summary,
      metric_basis: 'cumulative',
      totals: { read_users: 88, share_users: 12 },
      series: [{ date: '2026-10-10', read_users: 88, share_users: 12 }],
      coverage: { contents: 30 },
    })
    window.history.replaceState(null, '', '/content-analytics?account=wechat&platform=wechat')
    render(<ContentAnalyticsPage />)

    const panel = await screen.findByRole('region', { name: '内容累计总览' })
    await waitFor(() => expect(contentAnalyticsApi.overview).toHaveBeenCalled())
    expect(panel).toHaveTextContent('统计口径：累计')
    expect(panel).toHaveTextContent('阅读人数')
    expect(panel).toHaveTextContent('88')
    expect(panel).toHaveTextContent('30 篇内容')
  })

  it('switches analytics data with the project Agent icons', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{ id: 'shared', platform: '', name: '通用创作项目', agent_ids: ['seednote', 'wechat-article'] } as Project])
    window.history.replaceState(null, '', '/content-analytics?account=shared&platform=seednote')
    render(<ContentAnalyticsPage />)
    await waitFor(() => expect(screen.getByRole('combobox', { name: '筛选创作项目' })).toHaveTextContent('通用创作项目'))
    await waitFor(() => expect(contentAnalyticsApi.overview).toHaveBeenCalledWith('shared', expect.objectContaining({ platform: 'seednote' }), expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole('button', { name: '选择公众号文章' }))
    await waitFor(() => expect(contentAnalyticsApi.overview).toHaveBeenLastCalledWith('shared', expect.objectContaining({ platform: 'wechat', content_type: 'wechat-article' }), expect.any(AbortSignal)))
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('shared', expect.objectContaining({ content_type: 'wechat-article' }), expect.any(AbortSignal)))
    expect(screen.getByRole('button', { name: '选择公众号文章' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByLabelText('分析平台')).not.toBeInTheDocument()
  })

  it('keeps account totals account-wide while source trends use their own filter', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{ id: 'wechat', platform: 'wechat', name: '公众号项目', agent_ids: ['wechat-article'] } as Project])
    window.history.replaceState(null, '', '/content-analytics?account=wechat&platform=wechat&account_source=search')
    render(<ContentAnalyticsPage />)

    await waitFor(() => expect(wechatAccountAnalyticsApi.overview).toHaveBeenCalledWith('wechat', { from: expect.any(String), to: expect.any(String) }, expect.any(AbortSignal)))
    expect(vi.mocked(wechatAccountAnalyticsApi.overview).mock.calls[0]?.[1]).not.toHaveProperty('source')
    await waitFor(() => expect(wechatAccountAnalyticsApi.trafficSources).toHaveBeenCalledWith('wechat', expect.objectContaining({ source: 'search' }), expect.any(AbortSignal)))
  })

  it('renders every supported Agent as a selectable icon for a multi-Agent project', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'matrix', platform: 'wechat', name: '内容矩阵', agent_ids: ['wechat-article', 'seednote', 'wechat-picture'],
    } as Project])
    window.history.replaceState(null, '', '/content-analytics?account=matrix')
    render(<ContentAnalyticsPage />)

    expect(await screen.findByRole('button', { name: '选择公众号文章' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '选择种草笔记' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '选择公众号贴图' })).toBeInTheDocument()
    expect(screen.queryByLabelText('分析平台')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '选择种草笔记' }))
    await waitFor(() => expect(contentAnalyticsApi.overview).toHaveBeenLastCalledWith('matrix', expect.objectContaining({ platform: 'seednote' }), expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole('button', { name: '选择公众号贴图' }))
    await waitFor(() => expect(contentAnalyticsApi.overview).toHaveBeenLastCalledWith('matrix', expect.objectContaining({ platform: 'wechat', content_type: 'wechat-picture' }), expect.any(AbortSignal)))
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('matrix', expect.objectContaining({ content_type: 'wechat-picture' }), expect.any(AbortSignal)))
  })

  it('keeps Agent icons only in the analytics switcher', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'matrix', platform: 'wechat', name: '内容矩阵', agent_ids: ['wechat-article', 'seednote', 'wechat-picture'],
    } as Project])
    window.history.replaceState(null, '', '/content-analytics?account=matrix')
    render(<ContentAnalyticsPage />)

    const projectSelector = await screen.findByRole('combobox', { name: '筛选创作项目' })
    await waitFor(() => expect(projectSelector).toHaveTextContent('内容矩阵'))
    expect(projectSelector.querySelectorAll('[data-slot="agent-icon"]')).toHaveLength(0)
    expect(screen.getByRole('group', { name: '选择分析 Agent' }).querySelectorAll('button')).toHaveLength(3)
  })

  it('uses borderless backgroundless controls for analytics actions', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'matrix', platform: 'wechat', name: '内容矩阵', agent_ids: ['wechat-article', 'seednote'],
    } as Project])
    window.history.replaceState(null, '', '/content-analytics?account=matrix')
    render(<ContentAnalyticsPage />)

    const switcher = await screen.findByRole('group', { name: '选择分析 Agent' })
    for (const button of Array.from(switcher.querySelectorAll('button'))) {
      expect(button).not.toHaveClass('border-primary', 'border-border/70')
      expect(button.className).not.toMatch(/\bbg-/)
      expect(button.className).not.toMatch(/\bshadow(?:-|$)/)
    }
    for (const label of ['查看账号导入记录', '导入公众号账号统计']) {
      const button = screen.getByRole('button', { name: label })
      expect(button).toHaveClass('hover:bg-muted')
      expect(button.className).not.toMatch(/\bbg-primary(?:-|$)/)
    }
  })

  it('resolves a legacy picture deep link before applying an Agent filter', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'matrix', platform: 'wechat', name: '内容矩阵', agent_ids: ['wechat-article', 'wechat-picture'],
    } as Project])
    vi.mocked(contentAnalyticsApi.detail).mockResolvedValue({
      ...summary,
      content: { ...content, id: 'task:picture-1', title: '贴图作品', content_type: 'wechat-picture', metrics: { read_users: 8 } },
    })
    window.history.replaceState(null, '', '/content-analytics?account=matrix&content=task%3Apicture-1&platform=wechat')
    render(<ContentAnalyticsPage />)

    expect(await screen.findByRole('heading', { name: '贴图作品' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: '选择公众号贴图' })).toHaveAttribute('aria-pressed', 'true'))
    expect(vi.mocked(contentAnalyticsApi.detail).mock.calls.every(([, , request]) => request.content_type === undefined)).toBe(true)
  })

  it('keeps a conflicting Agent deep link on the canonical content type', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'matrix', platform: 'wechat', name: '内容矩阵', agent_ids: ['wechat-article', 'wechat-picture'],
    } as Project])
    vi.mocked(contentAnalyticsApi.detail).mockResolvedValue({
      ...summary,
      content: { ...content, id: 'task:picture-2', title: '贴图作品', content_type: 'wechat-picture', metrics: { read_users: 8 } },
    })
    window.history.replaceState(null, '', '/content-analytics?account=matrix&content=task%3Apicture-2&platform=wechat&agent=wechat-article')
    render(<ContentAnalyticsPage />)

    expect(await screen.findByRole('heading', { name: '贴图作品' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: '选择公众号贴图' })).toHaveAttribute('aria-pressed', 'true'))
    expect(vi.mocked(contentAnalyticsApi.detail).mock.calls.every(([, , request]) => request.content_type === undefined)).toBe(true)
    expect(window.location.search).toContain('agent=wechat-picture')
  })

  it('does not guess an Agent for an unknown detail content type', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'matrix', platform: 'wechat', name: '内容矩阵', agent_ids: ['wechat-article', 'wechat-picture'],
    } as Project])
    vi.mocked(contentAnalyticsApi.detail).mockResolvedValue({
      ...summary,
      content: { ...content, id: 'task:legacy-1', title: '旧内容', content_type: 'legacy', metrics: { read_users: 8 } },
    })
    window.history.replaceState(null, '', '/content-analytics?account=matrix&content=task%3Alegacy-1&platform=wechat&agent=wechat-article')
    render(<ContentAnalyticsPage />)

    expect(await screen.findByRole('heading', { name: '旧内容' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '选择公众号文章' })).toHaveAttribute('aria-pressed', 'false')
      expect(screen.getByRole('button', { name: '选择公众号贴图' })).toHaveAttribute('aria-pressed', 'false')
    })
    expect(window.location.search).not.toContain('agent=')
  })

  it('does not invent analytics Agents for projects that only use unsupported Agents', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'video', platform: 'montage', name: '视频创作项目', agent_ids: ['montage', 'hypit'],
    } as Project])
    window.history.replaceState(null, '', '/content-analytics?account=video')
    render(<ContentAnalyticsPage />)

    await waitFor(() => expect(screen.getByRole('combobox', { name: '筛选创作项目' })).toHaveTextContent('视频创作项目'))
    expect(await screen.findByText('该创作项目没有可用的内容分析 Agent。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /选择公众号|选择种草笔记/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '导入分析数据' })).toBeDisabled()
    expect(contentAnalyticsApi.overview).not.toHaveBeenCalled()
  })

  it('uses the shared compact project selector and removes redundant date controls', async () => {
    render(<ContentAnalyticsPage />)

    const projectSelector = await screen.findByRole('combobox', { name: '筛选创作项目' })
    await waitFor(() => expect(projectSelector).toHaveTextContent('生活笔记'))
    expect(projectSelector).not.toHaveClass('border-border')
    expect(projectSelector.querySelector('[data-slot="avatar"]')).toBeInTheDocument()
    expect(screen.queryByText(/最近数据/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText('数据年份')).not.toBeInTheDocument()
    expect(contentAnalyticsApi.dates).not.toHaveBeenCalled()
  })

  it('uses cumulative reporting without a daily or increment basis control', async () => {
    render(<ContentAnalyticsPage />)
    expect(await screen.findByText('趋势')).toBeInTheDocument()
    expect(screen.queryByLabelText('统计口径')).not.toBeInTheDocument()
    expect(screen.queryByText(/当日新增|增量/)).not.toBeInTheDocument()
  })
  it('keeps the existing title and date sort options compatible with the server API', async () => {
    render(<ContentAnalyticsPage />)
    await screen.findByRole('button', { name: '服务器内容' })

    fireEvent.change(screen.getByRole('combobox', { name: '排序指标' }), { target: { value: 'title' } })
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ sort: 'title' }), expect.any(AbortSignal)))

    fireEvent.change(screen.getByRole('combobox', { name: '排序指标' }), { target: { value: 'date' } })
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ sort: 'date' }), expect.any(AbortSignal)))
  })
  it('presents Seednote analytics in the four official analysis views', async () => {
    vi.mocked(contentAnalyticsApi.overview).mockResolvedValue({
      ...summary,
      totals: { exposure_count: 1200, view_count: 320, cover_click_rate: 0.18, avg_watch_duration: 12.4 },
      coverage: { contents: 51 },
    })
    window.history.replaceState(null, '', '/content-analytics?account=notes&platform=seednote')
    render(<ContentAnalyticsPage />)

    expect(await screen.findByRole('tab', { name: '观看数据' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '互动数据' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '涨粉数据' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '发布数据' })).toBeInTheDocument()
    expect(screen.getByLabelText('观看数据关键指标')).toHaveTextContent('曝光数')
    expect(screen.getByText('暂未接入搜索、首页推荐等来源维度。')).toBeInTheDocument()
    expect(screen.getByText('暂未接入按小时聚合的观看数据。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '互动数据' }))
    await waitFor(() => expect(window.location.search).toContain('view=interaction'))
    expect(screen.getByLabelText('互动数据关键指标')).toHaveTextContent('分享')
  })
  it('shows each content item tags below its title without making them interactive', async () => {
    render(<ContentAnalyticsPage />)

    const tagRow = await screen.findByTestId('content-tags-row-canonical-1')
    const row = tagRow.previousElementSibling as HTMLElement
    expect(row).toHaveTextContent('服务器内容')
    expect(tagRow).toHaveTextContent('热搜')
    expect(tagRow).toHaveTextContent('爆款')
    expect(tagRow.querySelectorAll('[data-content-tag]')).toHaveLength(2)
    expect(row.querySelectorAll('button')).toHaveLength(1)
  })
  it('gives each content item a full-width tag row beneath its metrics row', async () => {
    render(<ContentAnalyticsPage />)

    const tagRow = await screen.findByTestId('content-tags-row-canonical-1')
    expect(tagRow).toHaveAttribute('aria-label', '服务器内容的内容标签')
    expect(tagRow.querySelector('td')).toHaveAttribute('colspan', '9')
    expect(tagRow.querySelectorAll('[data-content-tag]')).toHaveLength(2)
  })
  it('shows the selected content tags below its title in detail view', async () => {
    window.history.replaceState(null, '', '/content-analytics?account=notes&content=canonical-1')
    render(<ContentAnalyticsPage />)

    const heading = await screen.findByRole('heading', { name: '服务器内容' })
    const detail = heading.closest('section')
    expect(detail).toHaveTextContent('热搜')
    expect(detail).toHaveTextContent('爆款')
    expect(detail?.querySelectorAll('[data-content-tag]')).toHaveLength(2)
    expect(detail?.querySelectorAll('[data-content-tag] button')).toHaveLength(0)
  })
  it('hides the tag section for a selected content item without tags', async () => {
    vi.mocked(contentAnalyticsApi.detail).mockResolvedValue({
      ...summary,
      content: { ...content, tags: undefined },
    })
    window.history.replaceState(null, '', '/content-analytics?account=notes&content=canonical-1')
    render(<ContentAnalyticsPage />)

    const heading = await screen.findByRole('heading', { name: '服务器内容' })
    expect(heading.closest('section')).not.toHaveTextContent('暂无足够证据标注')
    expect(heading.closest('section')?.querySelector('[aria-label="内容标签"]')).not.toBeInTheDocument()
  })
  it('hides the tag row for a listed content item without tags', async () => {
    vi.mocked(contentAnalyticsApi.contents).mockResolvedValue({ revision: 4, items: [{ ...content, tags: undefined }], total: 1, offset: 0, limit: 25 })
    render(<ContentAnalyticsPage />)

    await screen.findByRole('button', { name: '服务器内容' })
    expect(screen.queryByTestId('content-tags-row-canonical-1')).not.toBeInTheDocument()
  })
  it('reads observation history only after explicitly opening it', async () => {
    window.history.replaceState(null, '', '/content-analytics?account=notes&content=canonical-1')
    render(<ContentAnalyticsPage />)
    await screen.findByRole('heading', { name: '服务器内容' })
    expect(contentAnalyticsApi.observations).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '查看观测记录' }))
    await waitFor(() => expect(contentAnalyticsApi.observations).toHaveBeenCalledWith('notes', 'canonical-1', expect.objectContaining({ offset: 0, limit: 25 }), expect.any(AbortSignal)))
  })
  it('retains the previous page while the next server page is pending', async () => {
    vi.mocked(contentAnalyticsApi.contents).mockResolvedValueOnce({ revision: 4, items: [content], total: 51, offset: 0, limit: 25 }).mockImplementationOnce(() => new Promise(() => {}))
    render(<ContentAnalyticsPage />)
    await screen.findByRole('button', { name: '服务器内容' })
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: '服务器内容' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
  })
  it('opens a task deep link directly without reading overview or content lists', async () => {
    window.history.replaceState(null, '', '/content-analytics?account=notes&content=task:t1')
    render(<ContentAnalyticsPage />)
    expect(await screen.findByRole('heading', { name: '服务器内容' })).toBeInTheDocument()
    expect(contentAnalyticsApi.detail).toHaveBeenCalledWith('notes', 'task:t1', expect.objectContaining({ metric_basis: 'cumulative' }), expect.any(AbortSignal))
    expect(contentAnalyticsApi.overview).not.toHaveBeenCalled()
    expect(contentAnalyticsApi.contents).not.toHaveBeenCalled()
    expect(contentAnalyticsApi.observations).not.toHaveBeenCalled()
  })
  it('paginates on the server and keeps overview cached when selecting a row', async () => {
    render(<ContentAnalyticsPage />)
    await screen.findByRole('button', { name: '服务器内容' })
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ offset: 25, limit: 25 }), expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole('button', { name: '服务器内容' }))
    expect(await screen.findByRole('heading', { name: '服务器内容' })).toBeInTheDocument()
    expect(contentAnalyticsApi.overview).toHaveBeenCalledTimes(1)
  })
  it('debounces search while retaining server-side filtering', async () => {
    render(<ContentAnalyticsPage />)
    await screen.findByRole('button', { name: '服务器内容' })
    const calls = vi.mocked(contentAnalyticsApi.contents).mock.calls.length
    fireEvent.change(screen.getByRole('textbox', { name: '搜索内容' }), { target: { value: '新标题' } })
    expect(contentAnalyticsApi.contents).toHaveBeenCalledTimes(calls)
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ search: '新标题', offset: 0 }), expect.any(AbortSignal)))
  })
  it('refreshes the overview revision after a list conflict', async () => {
    vi.mocked(contentAnalyticsApi.contents).mockRejectedValueOnce(Object.assign(new Error('revision conflict'), { response: { status: 409 } }))
    vi.mocked(contentAnalyticsApi.overview).mockResolvedValueOnce({ ...summary, coverage: { contents: 51 } }).mockResolvedValue({ ...summary, revision: 5, coverage: { contents: 51 } })
    render(<ContentAnalyticsPage />)
    expect(await screen.findByRole('button', { name: '服务器内容' })).toBeInTheDocument()
    expect(contentAnalyticsApi.overview).toHaveBeenCalledTimes(2)
    expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ expected_revision: 5 }), expect.any(AbortSignal))
  })
})
