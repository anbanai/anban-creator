import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import ContentDataPage from './ContentDataPage'
import { contentAnalyticsApi } from '@/lib/content-analytics'
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'user-1' } }) }))
vi.mock('@/lib/api', () => ({ api: { projects: { list: vi.fn(async () => [{ id: 'notes', platform: 'seednote', name: '生活笔记' }]) } } }))
vi.mock('@/components/analytics/AnalyticsTrend', () => ({ default: () => <div>趋势</div> }))
vi.mock('@/components/analytics/ContentImportDialog', () => ({ default: () => null }))
vi.mock('@/components/analytics/ContentImportHistory', () => ({ default: () => null }))
vi.mock('@/lib/content-analytics', async importOriginal => ({ ...await importOriginal<typeof import('@/lib/content-analytics')>(), contentAnalyticsApi: { overview: vi.fn(), contents: vi.fn(), detail: vi.fn(), dates: vi.fn(), observations: vi.fn() } }))
const content = { id: 'canonical-1', title: '服务器内容', content_type: 'image_text', metrics: { view_count: 12 } }
const summary = { revision: 4, updated_at: '', metric_basis: 'cumulative' as const, totals: { view_count: 12 }, series: [], unavailable_metrics: {} }
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear(); window.history.replaceState(null, '', '/content-data?account=notes')
  vi.mocked(contentAnalyticsApi.overview).mockResolvedValue({ ...summary, coverage: { contents: 51 } })
  vi.mocked(contentAnalyticsApi.contents).mockResolvedValue({ revision: 4, items: [content], total: 51, offset: 0, limit: 25 })
  vi.mocked(contentAnalyticsApi.detail).mockResolvedValue({ ...summary, content })
  vi.mocked(contentAnalyticsApi.dates).mockResolvedValue({ revision: 4, dates: ['2026-09-24'] })
  vi.mocked(contentAnalyticsApi.observations).mockResolvedValue({ revision: 4, items: [], total: 0, offset: 0, limit: 25 })
})
describe('server analytics queries', () => {
  it('uses the shared compact project selector and removes redundant date controls', async () => {
    render(<ContentDataPage />)

    const projectSelector = await screen.findByRole('combobox', { name: '筛选项目' })
    await waitFor(() => expect(projectSelector).toHaveTextContent('生活笔记'))
    expect(projectSelector).not.toHaveClass('border-border')
    expect(projectSelector.querySelector('[data-slot="avatar-fallback"] svg')).toBeInTheDocument()
    expect(screen.queryByText(/最近数据/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText('数据年份')).not.toBeInTheDocument()
    expect(contentAnalyticsApi.dates).not.toHaveBeenCalled()
  })

  it('uses cumulative reporting without a daily or increment basis control', async () => {
    render(<ContentDataPage />)
    expect(await screen.findByText('趋势')).toBeInTheDocument()
    expect(screen.queryByLabelText('统计口径')).not.toBeInTheDocument()
    expect(screen.queryByText(/当日新增|增量/)).not.toBeInTheDocument()
  })

  it('reads observation history only after explicitly opening it', async () => {
    window.history.replaceState(null, '', '/content-data?account=notes&content=canonical-1')
    render(<ContentDataPage />)
    await screen.findByRole('heading', { name: '服务器内容' })
    expect(contentAnalyticsApi.observations).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '查看观测记录' }))
    await waitFor(() => expect(contentAnalyticsApi.observations).toHaveBeenCalledWith('notes', 'canonical-1', expect.objectContaining({ offset: 0, limit: 25 }), expect.any(AbortSignal)))
  })
  it('retains the previous page while the next server page is pending', async () => {
    vi.mocked(contentAnalyticsApi.contents).mockResolvedValueOnce({ revision: 4, items: [content], total: 51, offset: 0, limit: 25 }).mockImplementationOnce(() => new Promise(() => {}))
    render(<ContentDataPage />)
    await screen.findByRole('button', { name: '服务器内容' })
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: '服务器内容' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
  })
  it('opens a task deep link directly without reading overview or content lists', async () => {
    window.history.replaceState(null, '', '/content-data?account=notes&content=task:t1')
    render(<ContentDataPage />)
    expect(await screen.findByRole('heading', { name: '服务器内容' })).toBeInTheDocument()
    expect(contentAnalyticsApi.detail).toHaveBeenCalledWith('notes', 'task:t1', expect.objectContaining({ metric_basis: 'cumulative' }), expect.any(AbortSignal))
    expect(contentAnalyticsApi.overview).not.toHaveBeenCalled()
    expect(contentAnalyticsApi.contents).not.toHaveBeenCalled()
    expect(contentAnalyticsApi.observations).not.toHaveBeenCalled()
  })
  it('paginates on the server and keeps overview cached when selecting a row', async () => {
    render(<ContentDataPage />)
    await screen.findByRole('button', { name: '服务器内容' })
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ offset: 25, limit: 25 }), expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole('button', { name: '服务器内容' }))
    expect(await screen.findByRole('heading', { name: '服务器内容' })).toBeInTheDocument()
    expect(contentAnalyticsApi.overview).toHaveBeenCalledTimes(1)
  })
  it('debounces search while retaining server-side filtering', async () => {
    render(<ContentDataPage />)
    await screen.findByRole('button', { name: '服务器内容' })
    const calls = vi.mocked(contentAnalyticsApi.contents).mock.calls.length
    fireEvent.change(screen.getByRole('textbox', { name: '搜索内容' }), { target: { value: '新标题' } })
    expect(contentAnalyticsApi.contents).toHaveBeenCalledTimes(calls)
    await waitFor(() => expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ search: '新标题', offset: 0 }), expect.any(AbortSignal)))
  })
  it('refreshes the overview revision after a list conflict', async () => {
    vi.mocked(contentAnalyticsApi.contents).mockRejectedValueOnce(Object.assign(new Error('revision conflict'), { response: { status: 409 } }))
    vi.mocked(contentAnalyticsApi.overview).mockResolvedValueOnce({ ...summary, coverage: { contents: 51 } }).mockResolvedValue({ ...summary, revision: 5, coverage: { contents: 51 } })
    render(<ContentDataPage />)
    expect(await screen.findByRole('button', { name: '服务器内容' })).toBeInTheDocument()
    expect(contentAnalyticsApi.overview).toHaveBeenCalledTimes(2)
    expect(contentAnalyticsApi.contents).toHaveBeenLastCalledWith('notes', expect.objectContaining({ expected_revision: 5 }), expect.any(AbortSignal))
  })
})
