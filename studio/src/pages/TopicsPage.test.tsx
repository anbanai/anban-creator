import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Routes, Route, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { mockProjects } from '@/test/mocks/handlers'
import TopicsPage from './TopicsPage'
import type { ContentAsset } from '@/types'
const asset: ContentAsset = { asset_id: '4b073aec-e9cf-4f18-b8b8-93b04d07aa93', user_id: 'u', title: '一个值得写的选题', core_question: '这个问题为何重要', status: 'candidate', lifecycle: 'evergreen', created_at: '', updated_at: '', project_uses: [{ project_id: mockProjects[0].id, project_name: mockProjects[0].name, status: 'unused' }], outputs: [{ task_id: 'script-task', project_id: mockProjects[0].id, type: 'content-script', title: '脚本', status: 'completed' }] }
const secondProject = { ...mockProjects[0], id: 'ch-2', name: '另一个人设' }
function HistoryControls() {
  const navigate = useNavigate()
  const location = useLocation()
  return <><output data-testid="url">{location.search}</output><button onClick={() => navigate(-1)}>历史后退</button><button onClick={() => navigate(1)}>历史前进</button><button onClick={() => navigate('/away')}>离开选题</button></>
}
function page(url = '/topics') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const view = render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[url]}><HistoryControls /><main><Routes><Route path="/topics" element={<TopicsPage />} /><Route path="/away" element={<p>其他页面</p>} /></Routes></main></MemoryRouter></QueryClientProvider>)
  return { ...view, client }
}
beforeEach(() => {
  vi.spyOn(api.projects, 'list').mockResolvedValue(mockProjects)
  vi.spyOn(api.contentAssets, 'list').mockResolvedValue({ items: [asset], total: 1 })
  vi.spyOn(api.contentAssets, 'get').mockResolvedValue({ asset, evidence: [], links: [], scores: [] })
})
describe('unified topics page', () => {
  it('loads existing lineage and analytics only when expanded', async () => {
    const lineage = vi.spyOn(api.contentAssets, 'getLineage').mockResolvedValue({ asset, evidence: [], scores: [], links: [], platform_performance: [], latest_runs: [], summary: { origin_count: 0, derivative_count: 0, run_count: 0, published_count: 0, cost_credits: 0, reserved_credits: 0 } })
    page(`/topics?asset_id=${asset.asset_id}`)
    const drawer = await screen.findByRole('dialog')
    expect(lineage).not.toHaveBeenCalled()
    const summary = await within(drawer).findByText('平台表现与内容链路')
    const details = summary.closest('details')!
    details.open = true
    fireEvent(details, new Event('toggle'))
    await waitFor(() => expect(lineage).toHaveBeenCalledWith(asset.asset_id))
  })
  it('shows concise work result links alongside scripts', async () => {
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [{ ...asset, outputs: [...asset.outputs!, { task_id: 'article-result', project_id: mockProjects[0].id, type: 'wechat-article', status: 'completed', title: '已发布文章' }] }], total: 1 })
    page()
    const list = screen.getByRole('region', { name: '选题列表' })
    expect(await within(list).findByRole('link', { name: '作品：已完成' })).toHaveAttribute('href', '/tasks/article-result')
    expect(within(list).getByRole('link', { name: '脚本：已完成' })).toBeInTheDocument()
  })

  it('shows independent usage for each persona in the all-persona view', async () => {
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [{ ...asset, project_uses: [{ project_id: 'a', project_name: '甲', status: 'used' }, { project_id: 'b', project_name: '乙', status: 'unused' }] }], total: 1 })
    page()
    const list = screen.getByRole('region', { name: '选题列表' })
    expect(await within(list).findByText('甲：已使用 · 乙：未使用')).toBeInTheDocument()
    expect(within(list).queryByText('已使用')).not.toBeInTheDocument()
  })
  it.each(['queued', 'failed'])('opens durable %s scripts and retries failures without a task', async status => {
    const output = { job_id: 'durable-script', project_id: mockProjects[0].id, type: 'topic-script', title: '脚本', status, error: status === 'failed' ? '积分不足' : undefined }
    const withScript = { ...asset, outputs: [output] }
    const job = { id: 'durable-script', suggestion_id: '', digest_id: '', aspect_ratio: '9:16', target_platform: 'douyin', duration_seconds: 60, status, error: output.error }
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [withScript], total: 1 })
    vi.mocked(api.contentAssets.get).mockResolvedValue({ asset: withScript, evidence: [], links: [], scores: [] })
    vi.spyOn(api.topicRadar, 'script').mockResolvedValue(job)
    const retry = vi.spyOn(api.topicRadar, 'retryScript').mockResolvedValue({ ...job, status: 'queued' })
    page()
    fireEvent.click(await screen.findByRole('link', { name: status === 'failed' ? '脚本：失败' : '脚本：生成中' }))
    await waitFor(() => expect(api.topicRadar.script).toHaveBeenCalledWith('durable-script'))
    expect(screen.getByTestId('url').textContent).toContain('script_id=durable-script')
    expect(screen.queryByRole('link', { name: '查看脚本结果' })).not.toBeInTheDocument()
    if (status === 'failed') {
      expect((await screen.findAllByText(/积分不足/)).length).toBeGreaterThan(0)
      fireEvent.click(await screen.findByRole('button', { name: '重试脚本' }))
      await waitFor(() => expect(retry).toHaveBeenCalledWith('durable-script'))
    }
    expect(document.querySelector('a[href*="undefined"]')).toBeNull()
  })
  it('keeps search, persona, usage, and page when opening a script result', async () => {
    const withScript: ContentAsset = { ...asset, outputs: [{ job_id: 'filtered-script', project_id: mockProjects[0].id, type: 'topic-script', title: '脚本', status: 'completed' }] }
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [withScript], total: 60 })
    vi.mocked(api.contentAssets.get).mockResolvedValue({ asset: withScript, evidence: [], links: [], scores: [] })
    vi.spyOn(api.topicRadar, 'script').mockResolvedValue({ id: 'filtered-script', suggestion_id: '', digest_id: '', aspect_ratio: '9:16', target_platform: 'douyin', duration_seconds: 60, status: 'completed' })
    page(`/topics?q=foo&project_id=${mockProjects[0].id}&usage=unused&page=2`)
    fireEvent.click(await screen.findByRole('link', { name: '脚本：已完成' }))
    await screen.findByRole('dialog')
    const query = new URLSearchParams(screen.getByTestId('url').textContent!)
    expect(query.get('q')).toBe('foo')
    expect(query.get('project_id')).toBe(mockProjects[0].id)
    expect(query.get('usage')).toBe('unused')
    expect(query.get('page')).toBe('2')
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(new URLSearchParams(screen.getByTestId('url').textContent!).get('page')).toBe('2')
  })

  it('defaults to all personas and separates script status from persona usage', async () => {
    page()
    const list = await screen.findByRole('region', { name: '选题列表' })
    await within(list).findByText(asset.title)
    expect(screen.queryByRole('combobox', { name: '所属人设筛选' })).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '人设使用状态' })).toBeInTheDocument()
    expect(within(list).getByText(`${mockProjects[0].name}：未使用`)).toBeInTheDocument()
    expect(within(list).getByRole('link', { name: '脚本：已完成' })).toHaveAttribute('href', '/tasks/script-task')
    expect(screen.queryByRole('navigation', { name: '选题分页' })).not.toBeInTheDocument()
  })
  it('shows search and useful persona filters in one toolbar', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([mockProjects[0], secondProject])
    page()
    await screen.findByText(asset.title)
    const search = screen.getByRole('textbox', { name: '搜索选题' })
    const toolbar = search.parentElement!
    expect(within(toolbar).getByRole('combobox', { name: '所属人设筛选' })).toBeInTheDocument()
    expect(within(toolbar).getByRole('combobox', { name: '人设使用状态' })).toBeInTheDocument()
  })
  it('omits the persona filter when only one persona exists', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([mockProjects[0]])
    page()
    await screen.findByText(asset.title)
    expect(screen.queryByRole('combobox', { name: '所属人设筛选' })).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '人设使用状态' })).toBeInTheDocument()
  })
  it('keeps the all-persona list unfiltered and omits a redundant persona choice for one persona', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([mockProjects[0]])
    page()
    await screen.findByText(asset.title)
    expect(screen.queryByRole('combobox', { name: '所属人设筛选' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '自动化设置' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '找新选题' }))
    await screen.findByRole('button', { name: '开始找选题' })
    expect(screen.queryByRole('combobox', { name: '操作人设' })).not.toBeInTheDocument()
  })
  it('offers first discovery only in first-use empty state', async () => {
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [], total: 0 })
    page()
    expect(await screen.findByRole('button', { name: '找第一批选题' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '清除筛选' })).not.toBeInTheDocument()
  })
  it('clears a filtered empty state and resets pagination', async () => {
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [], total: 0 })
    page('/topics?q=missing&usage=unused&page=3')
    fireEvent.click(await screen.findByRole('button', { name: '清除筛选' }))
    await waitFor(() => expect(screen.getByTestId('url')).toHaveTextContent(''))
    expect(screen.getByRole('textbox', { name: '搜索选题' })).toHaveValue('')
  })
  it('restores URL filters and page with browser history', async () => {
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [asset], total: 60 })
    page(`/topics?q=关键词&project_id=${mockProjects[0].id}&usage=unused&page=2`)
    await screen.findByText(asset.title)
    const viewport = screen.getByRole('main')
    viewport.scrollTop = 420
    fireEvent.change(screen.getByRole('combobox', { name: '人设使用状态' }), { target: { value: 'used' } })
    expect(screen.getByRole('combobox', { name: '人设使用状态' })).toHaveValue('used')
    fireEvent.click(screen.getByRole('button', { name: '历史后退' }))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '人设使用状态' })).toHaveValue('unused'))
    expect(screen.getByRole('textbox', { name: '搜索选题' })).toHaveValue('关键词')
    expect(screen.getByTestId('url').textContent).toContain('page=2')
    expect(viewport.scrollTop).toBe(420)
    fireEvent.click(screen.getByRole('button', { name: '历史前进' }))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '人设使用状态' })).toHaveValue('used'))
  })
  it('opens an accessible drawer and hands canonical asset ID to existing creation form', async () => {
    page(`/topics?project_id=${mockProjects[0].id}`)
    fireEvent.click(await screen.findByRole('button', { name: asset.title }))
    const drawer = await screen.findByRole('dialog')
    const moreInfo = await within(drawer).findByText('更多信息')
    expect(moreInfo.closest('details')).not.toHaveAttribute('open')
    const link = await within(drawer).findByRole('link', { name: '开始创作' })
    const url = new URL(link.getAttribute('href')!, 'https://studio.test')
    expect(url.pathname).toBe('/tasks/new')
    expect(url.searchParams.get('asset_id')).toBe(asset.asset_id)
    expect(url.searchParams.get('project_id')).toBe(mockProjects[0].id)
    expect(url.searchParams.get('prompt')).toContain(asset.core_question)
    fireEvent.click(within(drawer).getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByTestId('url').textContent).not.toContain('asset_id')
  })
  it('hides empty usage and result sections until the topic has history', async () => {
    const fresh: ContentAsset = { ...asset, project_uses: [], outputs: [] }
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [fresh], total: 1 })
    vi.mocked(api.contentAssets.get).mockResolvedValue({ asset: fresh, evidence: [], links: [], scores: [] })
    page(`/topics?project_id=${mockProjects[0].id}`)
    fireEvent.click(await screen.findByRole('button', { name: fresh.title }))
    const drawer = await screen.findByRole('dialog')
    expect(within(drawer).queryByRole('heading', { name: '人设使用情况' })).not.toBeInTheDocument()
    expect(within(drawer).queryByRole('heading', { name: '创作结果' })).not.toBeInTheDocument()
    expect(await within(drawer).findByRole('link', { name: '开始创作' })).toBeInTheDocument()
  })
  it('allows global topic availability from a persona-filtered add drawer', async () => {
    const create = vi.spyOn(api.contentAssets, 'create').mockResolvedValue({ ...asset, project_uses: [] })
    page(`/topics?project_id=${mockProjects[0].id}`)
    fireEvent.click(await screen.findByRole('button', { name: '添加选题' }))
    fireEvent.change(await screen.findByRole('combobox', { name: '操作人设' }), { target: { value: '__all__' } })
    expect(screen.getByRole('combobox', { name: '操作人设' })).toHaveValue('__all__')
    fireEvent.change(screen.getByRole('textbox', { name: '选题标题' }), { target: { value: '全局选题' } })
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: '添加选题' }))
    await waitFor(() => expect(create).toHaveBeenCalledWith(expect.objectContaining({ title: '全局选题', project_ids: [] })))
    expect(new URLSearchParams(screen.getByTestId('url').textContent!).get('project_id')).toBe(mockProjects[0].id)
  })
  it('routes an already-used persona and topic to the existing reuse flow', async () => {
    const used: ContentAsset = { ...asset, project_uses: [{ project_id: mockProjects[0].id, project_name: mockProjects[0].name, status: 'used' }] }
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [used], total: 1 })
    vi.mocked(api.contentAssets.get).mockResolvedValue({ asset: used, evidence: [], links: [], scores: [] })
    vi.spyOn(api.contentAssets, 'listRuns').mockResolvedValue({ items: [], total: 0, offset: 0, limit: 25 })
    page(`/topics?project_id=${mockProjects[0].id}&asset_id=${asset.asset_id}`)
    const drawer = await screen.findByRole('dialog')
    expect(await within(drawer).findByRole('button', { name: '开始复用' })).toBeInTheDocument()
    expect(within(drawer).queryByRole('link', { name: '开始创作' })).not.toBeInTheDocument()
  })
  it('opens the collapsed extra information when a link targets reuse history', async () => {
    vi.mocked(api.contentAssets.get).mockResolvedValue({ asset, evidence: [], links: [], scores: [] })
    vi.spyOn(api.contentAssets, 'listRuns').mockResolvedValue({ items: [], total: 0, offset: 0, limit: 25 })
    page(`/topics?project_id=${mockProjects[0].id}&asset_id=${asset.asset_id}&detail=reuse`)

    const drawer = await screen.findByRole('dialog')
    expect(await within(drawer).findByRole('button', { name: '开始复用' })).toBeInTheDocument()
    expect(within(drawer).getByText('更多信息').closest('details')).toHaveAttribute('open')
  })
  it.each([false, true])('clamps empty last page, archive=%s', async archiveLast => {
    let archived = !archiveLast
    vi.mocked(api.contentAssets.list).mockImplementation(async filters => ({ items: filters?.offset && archived ? [] : [asset], total: archived ? 25 : 26 }))
    vi.spyOn(api.contentAssets, 'archive').mockImplementation(async () => { archived = true })
    page(`/topics?page=2${archiveLast ? `&asset_id=${asset.asset_id}` : ''}`)
    if (archiveLast) {
      await waitFor(() => expect(api.contentAssets.list).toHaveBeenCalled())
      fireEvent.click(await screen.findByRole('button', { name: '归档选题' }))
      await waitFor(() => expect(screen.getByTestId('url').textContent).not.toContain('asset_id'))
    }
    await waitFor(() => expect(screen.getByTestId('url').textContent).not.toContain('page=2'))
    expect(await screen.findByRole('button', { name: asset.title })).toBeInTheDocument()
    expect(screen.queryByText('你的第一批选题，从这里开始')).not.toBeInTheDocument()
  })
  it('clears source context before reusing another asset', async () => {
    const other = { ...asset, asset_id: 'other', title: '另一个选题' }
    vi.mocked(api.contentAssets.list).mockResolvedValue({ items: [other], total: 1 })
    vi.mocked(api.contentAssets.get).mockImplementation(async id => ({ asset: id === 'other' ? other : asset, evidence: [], links: [], scores: [] }))
    vi.spyOn(api.contentAssets, 'listRuns').mockResolvedValue({ items: [], total: 0, offset: 0, limit: 25 })
    const reuse = vi.spyOn(api.contentAssets, 'createReuseRun').mockRejectedValue(new Error('stop'))
    page(`/topics?asset_id=${asset.asset_id}&source_task_id=original-task&target_project_id=${mockProjects[0].id}`)
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    fireEvent.click(await screen.findByRole('button', { name: other.title }))
    fireEvent.click(await screen.findByText('更多信息'))
    fireEvent.click(await screen.findByText('跨平台复用与运行历史'))
    fireEvent.click(await screen.findByRole('button', { name: '开始复用' }))
    await waitFor(() => expect(reuse).toHaveBeenCalled())
    expect(reuse.mock.calls[0][0]).toBe('other')
    expect(reuse.mock.calls[0][1]).not.toHaveProperty('source_content_id')
  })
  it('waits for evicted history data before restoring scroll', async () => {
    const { client } = page('/topics?usage=unused')
    await screen.findByText(asset.title)
    const viewport = screen.getByRole('main')
    let position = 420
    Object.defineProperty(viewport, 'scrollTop', { configurable: true, get: () => position, set: value => { position = screen.queryByRole('button', { name: asset.title }) ? value : 0 } })
    fireEvent.scroll(viewport)
    fireEvent.change(screen.getByRole('combobox', { name: '人设使用状态' }), { target: { value: 'used' } })
    await waitFor(() => expect(client.isFetching()).toBe(0))
    client.removeQueries({ predicate: query => query.queryKey[0] === 'content-assets' && JSON.stringify(query.queryKey).includes('unused') })
    let finish!: (value: { items: ContentAsset[]; total: number }) => void
    vi.mocked(api.contentAssets.list).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    position = 0
    fireEvent.scroll(viewport)
    fireEvent.click(screen.getByRole('button', { name: '历史后退' }))
    await waitFor(() => expect(finish).toBeDefined())
    expect(position).toBe(0)
    await act(async () => finish({ items: [asset], total: 1 }))
    await waitFor(() => expect(position).toBe(420))
  })

  it('restores after remount with loading layout that clamps scrollTop', async () => {
    const { client } = page('/topics?usage=unused')
    await screen.findByText(asset.title)
    const viewport = screen.getByRole('main')
    let position = 420
    Object.defineProperty(viewport, 'scrollTop', { configurable: true, get: () => position, set: value => { position = screen.queryByRole('button', { name: asset.title }) ? value : 0 } })
    fireEvent.scroll(viewport)
    fireEvent.click(screen.getByRole('button', { name: '离开选题' }))
    client.removeQueries({ queryKey: ['content-assets'] })
    let finish!: (value: { items: ContentAsset[]; total: number }) => void
    vi.mocked(api.contentAssets.list).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    viewport.scrollTop = 0
    fireEvent.click(screen.getByRole('button', { name: '历史后退' }))
    await screen.findByText('正在加载选题…')
    expect(position).toBe(0)
    await act(async () => finish({ items: [asset], total: 1 }))
    await waitFor(() => expect(position).toBe(420))
  })

})
