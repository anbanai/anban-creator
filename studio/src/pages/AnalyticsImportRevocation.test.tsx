import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClientProvider } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { createTestQueryClient, render } from '@/test/test-utils'
import SeednoteDataPage from './SeednoteDataPage'
import WechatDataPage from './WechatDataPage'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const methods = () => ({ listBatches: vi.fn(), overview: vi.fn(), getBatch: vi.fn(), revoke: vi.fn() })
  return { ...actual, api: { ...actual.api,
    projects: { ...actual.api.projects, list: vi.fn() },
    seednoteImport: { ...actual.api.seednoteImport, ...methods(), posts: vi.fn(), post: vi.fn() },
    wechatAnalyticsImport: { ...actual.api.wechatAnalyticsImport, ...methods(), articles: vi.fn() },
  } }
})
const batch = {
  id: 'batch-1', project_id: 'project-1', file_name: '导错账号.xlsx', file_size: 4,
  received_at: '2026-09-18T08:00:00Z', data_as_of_at: '2026-09-18T08:00:00Z',
  timezone: 'Asia/Shanghai', parser_version: 'v1', status: 'needs_review',
  total_rows: 2, resolved_rows: 1, matched_rows: 1, review_rows: 1, unmatched_rows: 0, invalid_rows: 0,
}
const row = { id: 'row-1', source_row: 2, title: '待确认内容', match_status: 'needs_review' }
const revoked = { ...batch, status: 'revoked', revoked_at: '2026-09-20T08:00:00Z' }
const importedURL = 'https://mp.weixin.qq.com/s/imported'
const publication = { id: 'publication-1', task_id: 'task-1', project_id: 'project-1', draft_title: '原有内容', source: 'wechat_console', status: 'published', manual_publish_required: false, analytics_status: 'available' }

for (const platform of ['seednote', 'article'] as const) {
  describe(`${platform} analytics import revocation`, () => {
    let client = createTestQueryClient()
    const importApi = platform === 'seednote' ? api.seednoteImport : api.wechatAnalyticsImport
    async function openPage() {
      render(<QueryClientProvider client={client}>{platform === 'seednote' ? <SeednoteDataPage /> : <WechatDataPage />}</QueryClientProvider>)
      if (platform === 'seednote') fireEvent.change(await screen.findByRole('combobox', { name: '种草笔记账号' }), { target: { value: 'project-1' } })
      fireEvent.click(await screen.findByText(/历史导入/))
      return screen.findByRole('button', { name: /导错账号.xlsx/ })
    }
    function metrics(value: number) {
      vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], series: [], posts: [], post_summaries: [{ id: 'post-1', title: '原有内容', exposure_count: value }] })
      vi.mocked(api.wechatAnalyticsImport.overview).mockResolvedValue({ articles: 1, published: 1, with_data: 1, read_users: value, share_users: 0, read_to_follow_users: 0, delivered_users: 0 })
    }
    beforeEach(() => {
      client = createTestQueryClient()
      client.setDefaultOptions({ queries: { retry: false, gcTime: Infinity } })
      vi.clearAllMocks()
      vi.mocked(importApi.revoke).mockReset()
      vi.mocked(api.projects.list).mockResolvedValue([{ id: 'project-1', name: '当前账号', platform }, { id: 'project-2', name: '其他账号', platform }] as never)
      vi.mocked(importApi.listBatches).mockResolvedValue({ items: [batch], total: 1 })
      vi.mocked(importApi.getBatch).mockResolvedValue({ batch, rows: [row] })
      vi.mocked(importApi.revoke).mockResolvedValue({ batch: revoked, rows: [row] })
      metrics(321)
      vi.mocked(api.seednoteImport.posts).mockResolvedValue({ items: [], total: 0 })
      vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [{ publication }] })
    })
    it('requires filename and impact confirmation and allows cancellation', async () => {
      await openPage()
      fireEvent.click(screen.getByRole('button', { name: '撤销本次导入' }))
      const dialog = screen.getByRole('dialog', { name: '撤销本次导入' })
      expect(within(dialog).getByText('导错账号.xlsx')).toBeInTheDocument()
      expect(within(dialog).getByText(/统计/)).toBeInTheDocument()
      expect(within(dialog).getByText(/重新导入/)).toBeInTheDocument()
      expect(importApi.revoke).not.toHaveBeenCalled()
      fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
      expect(importApi.revoke).not.toHaveBeenCalled()
      expect(screen.getByText('321')).toBeInTheDocument()
    })
    it('refreshes history and metrics and removes matching actions after success', async () => {
      fireEvent.click(await openPage())
      expect(await screen.findByRole('button', { name: '跳过' })).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: '撤销本次导入' }))
      vi.mocked(importApi.listBatches).mockResolvedValue({ items: [revoked], total: 1 })
      vi.mocked(importApi.getBatch).mockResolvedValue({ batch: revoked, rows: [row] })
      metrics(12)
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认撤销' }))
      await waitFor(() => expect(importApi.revoke).toHaveBeenCalledWith('project-1', 'batch-1'))
      expect(await screen.findByText('12')).toBeInTheDocument()
      expect(screen.queryByText('321')).not.toBeInTheDocument()
      expect(screen.getAllByText('已撤销').length).toBeGreaterThan(0)
      expect(screen.queryByRole('button', { name: '跳过' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '确认匹配' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '新建帖子' })).not.toBeInTheDocument()
      expect(screen.queryByText('导入完成')).not.toBeInTheDocument()
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    })
    it('keeps failures inside the dialog and permits retry', async () => {
      vi.mocked(importApi.revoke).mockRejectedValueOnce(new Error('暂时无法撤销，请重试'))
      await openPage()
      fireEvent.click(screen.getByRole('button', { name: '撤销本次导入' }))
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认撤销' }))
      expect(await screen.findByText('暂时无法撤销，请重试')).toBeInTheDocument()
      const dialog = screen.getByRole('dialog', { name: '撤销本次导入' })
      expect(within(dialog).getByRole('button', { name: '确认撤销' })).toBeEnabled()
      expect(screen.getByText('321')).toBeInTheDocument()
      fireEvent.click(within(dialog).getByRole('button', { name: '确认撤销' }))
      await waitFor(() => expect(importApi.revoke).toHaveBeenCalledTimes(2))
    })
    it('keeps revoked batches read-only even when original rows need review', async () => {
      vi.mocked(importApi.listBatches).mockResolvedValue({ items: [revoked], total: 1 })
      vi.mocked(importApi.getBatch).mockResolvedValue({ batch: revoked, rows: [row] })
      fireEvent.click(await openPage())
      await waitFor(() => expect(importApi.getBatch).toHaveBeenCalledWith('project-1', 'batch-1'))
      expect(screen.getAllByText('已撤销').length).toBeGreaterThan(0)
      expect(screen.queryByRole('button', { name: '撤销本次导入' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '跳过' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '确认匹配' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '新建帖子' })).not.toBeInTheDocument()
      expect(screen.queryByText('导入完成')).not.toBeInTheDocument()
    })
    it('invalidates all project analytics including inactive details and retains other project caches', async () => {
      const prefix = platform === 'seednote' ? 'seednote-import-' : 'wechat-import-'
      const suffix = platform === 'seednote' ? 'post' : 'article'
      const detailKey = [prefix + suffix, 'project-1', 'inactive-content', { from: '2026-09-01' }]
      const otherProjectKey = [prefix + 'overview', 'project-2', { from: '2026-09-01' }]
      client.setQueryData(detailKey, { url: 'previous-import-url' })
      client.setQueryData(otherProjectKey, { total: 50 })
      const taskKey = platform === 'seednote' ? ['task', 'task-1', 'seednote-analytics'] : ['wechat-publication', 'task-1']
      client.setQueryData(taskKey, { url: 'previous-import-url' })
      await openPage()
      fireEvent.click(screen.getByRole('button', { name: '撤销本次导入' }))
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认撤销' }))
      await waitFor(() => expect(client.getQueryState(detailKey)?.isInvalidated).toBe(true))
      expect(client.getQueryState(otherProjectKey)?.isInvalidated).toBe(false)
      await waitFor(() => expect(client.getQueryState(taskKey)?.isInvalidated).toBe(true))
    })

    it('removes rolled-back URLs and closes stale content details', async () => {
      vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], series: [], posts: [], post_summaries: [{ id: 'post-1', title: '原有内容', note_url: 'https://www.xiaohongshu.com/explore/imported' }] })
      vi.mocked(api.seednoteImport.post).mockResolvedValue({ post: { id: 'post-1', title: '原有内容', note_url: 'https://www.xiaohongshu.com/explore/imported' }, versions: [] })
      vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [{ publication: { ...publication, article_url: importedURL } }] })
      await openPage()
      expect(screen.getByRole('link', { name: '查看原文' })).toBeInTheDocument()
      if (platform === 'seednote') {
        fireEvent.click(screen.getByRole('button', { name: '原有内容' }))
        expect(await screen.findByRole('button', { name: '关闭帖子详情' })).toBeInTheDocument()
      }
      fireEvent.click(screen.getByRole('button', { name: '撤销本次导入' }))
      metrics(12)
      vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [{ publication }] })
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认撤销' }))
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
      expect(screen.queryByRole('link', { name: '查看原文' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '关闭帖子详情' })).not.toBeInTheDocument()
    })

    it('disables cancellation and project switching while pending', async () => {
      let finish!: (value: { batch: typeof revoked; rows: typeof row[] }) => void
      vi.mocked(importApi.revoke).mockImplementation(() => new Promise<{ batch: typeof revoked; rows: typeof row[] }>((resolve) => { finish = resolve }))
      await openPage()
      fireEvent.click(screen.getByRole('button', { name: '撤销本次导入' }))
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认撤销' }))
      await waitFor(() => expect(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' })).toBeDisabled())
      expect(document.querySelector('select')).toBeDisabled()
      finish({ batch: revoked, rows: [row] })
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    })
  })
}
