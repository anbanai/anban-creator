import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { uploadToOSS } from '@/lib/direct-upload'
import { render } from '@/test/test-utils'
import WechatDataPage from './WechatDataPage'

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: vi.fn() }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      projects: { ...actual.api.projects, list: vi.fn() },
      tasks: { ...actual.api.tasks, bindManualWechatPublication: vi.fn(), reconcileWechat: vi.fn() },
      wechatAnalyticsImport: {
        ...actual.api.wechatAnalyticsImport,
        articles: vi.fn(),
        overview: vi.fn(),
        listBatches: vi.fn(),
        getBatch: vi.fn(),
        preview: vi.fn(),
        import: vi.fn(),
        resolve: vi.fn(),
      },
    },
  }
})

const project = {
  id: 'project-1', user_id: 'user-1', platform: 'article' as const, name: '茶事公众号',
  avatar_url: '', profile_url: '', keywords: '', visual_style: '', writer: '', theme: '', author: '',
  image_ratio: '16:9', max_concurrent_tasks: 1, config: {}, status: 'active' as const,
  created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
}

const awaitingArticle = {
  publication: {
    id: 'publication-1', task_id: 'task-1', project_id: project.id,
    draft_title: '秋天喝茶别急着买', draft_media_id: 'draft-1', source: 'wechat_console',
    status: 'awaiting_manual_publish', manual_publish_required: true, analytics_status: 'not_available',
    wechat_status_code: 48001, last_error: 'api unauthorized',
  },
}

describe('WechatDataPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.projects.list).mockResolvedValue([project])
    vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [awaitingArticle] })
    vi.mocked(api.wechatAnalyticsImport.overview).mockResolvedValue({ articles: 1, published: 0, with_data: 0, read_users: 0, share_users: 0, read_to_follow_users: 0, delivered_users: 0 })
    vi.mocked(api.wechatAnalyticsImport.listBatches).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.tasks.bindManualWechatPublication).mockResolvedValue({ ...awaitingArticle.publication, status: 'published', manual_publish_required: false })
    vi.mocked(api.tasks.reconcileWechat).mockResolvedValue({ reconciled: true })
    vi.mocked(uploadToOSS).mockResolvedValue({ uploadSessionId: 'upload-1', uploadId: 'upload-1', key: 'file.xls', previewUrl: '', publicUrl: '', contentType: 'application/vnd.ms-excel', size: 100 })
  })

  it('opens the imported URL from the article details and prefills manual confirmation', async () => {
    vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({ items: [{ publication: { ...awaitingArticle.publication, article_url: 'https://mp.weixin.qq.com/s/imported' } }] })
    render(<WechatDataPage />)
    fireEvent.click(await screen.findByRole('button', { name: '秋天喝茶别急着买' }))
    const details = await screen.findByRole('dialog')
    expect(within(details).getByRole('link', { name: '查看原文' })).toHaveAttribute('href', 'https://mp.weixin.qq.com/s/imported')
    fireEvent.click(within(details).getByRole('button', { name: '确认已在公众号发布' }))
    expect(screen.getByRole('textbox', { name: '文章 URL（可选）' })).toHaveValue('https://mp.weixin.qq.com/s/imported')
    expect(within(screen.getByRole('dialog')).getByRole('link', { name: '查看原文' })).toHaveAttribute('target', '_blank')
    expect(screen.getAllByRole('dialog')).toHaveLength(1)
  })

  it('renders 48001 as an actionable manual publication state', async () => {
    render(<WechatDataPage />)

    expect(await screen.findByText('待人工发布')).toBeInTheDocument()
    expect(screen.queryByText('发布失败')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认已在公众号发布' })).toBeInTheDocument()
  })

  it('keeps publication state and the next action visible in a narrow article row', async () => {
    render(<WechatDataPage />)

    const title = await screen.findByRole('button', { name: '秋天喝茶别急着买' })
    const row = title.closest('tr')
    expect(row).not.toBeNull()
    expect(row).toHaveClass('max-md:grid', 'max-md:grid-cols-6')
    expect(within(row!).getByText('当前状态')).toBeInTheDocument()
    expect(within(row!).getByText('下一步')).toBeInTheDocument()
    expect(within(row!).getByRole('button', { name: '确认已在公众号发布' })).toBeInTheDocument()
  })

  it('allows confirming manual publication without a URL', async () => {
    render(<WechatDataPage />)
    fireEvent.click(await screen.findByRole('button', { name: '确认已在公众号发布' }))
    fireEvent.click(screen.getByRole('button', { name: '保存发布状态' }))

    await waitFor(() => expect(api.tasks.bindManualWechatPublication).toHaveBeenCalledWith('task-1', ''))
  })

  it('automatically previews the workbook and classifies article content before importing', async () => {
    vi.mocked(api.wechatAnalyticsImport.preview).mockResolvedValue({
      source: '数据来源概况', file_name: 'total.xls', total_rows: 120, parser_version: 'wechat-xls-v1',
      field_mapping: [{ excel_field: '内容标题', internal_field: 'title' }],
      rows: [
        { source_row: 4, source: '文章', title: '白茶到底分几类？', read_users: 642, article_url: 'https://mp.weixin.qq.com/s/article' },
        { source_row: 5, source: '贴图', title: '夏日茶席', read_users: 120, article_url: 'https://mp.weixin.qq.com/s/sticker' },
      ],
    })
    vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({
      items: [{ publication: { ...awaitingArticle.publication, article_url: 'https://mp.weixin.qq.com/s/article' } }],
    })
    vi.mocked(api.wechatAnalyticsImport.import).mockResolvedValue({
      batch_id: 'batch-1', source_file: 'total.xls', parser_version: 'wechat-xls-v1',
      total_rows: 120, matched_rows: 108, ambiguous_rows: 7, unmatched_rows: 3, invalid_rows: 2,
    })
    render(<WechatDataPage />)
    fireEvent.click(await screen.findByRole('button', { name: '导入数据' }))
    const dialog = await screen.findByRole('dialog')
    const file = new File(['xls'], 'total.xls', { type: 'application/vnd.ms-excel' })
    const fileInput = dialog.querySelector<HTMLInputElement>('input[type="file"]')
    expect(fileInput).not.toBeNull()
    fireEvent.change(fileInput!, { target: { files: [file] } })

    expect(await screen.findByText('白茶到底分几类？')).toBeInTheDocument()
    expect(within(dialog).getAllByText('文章').length).toBeGreaterThan(0)
    expect(within(dialog).getByText('贴图')).toBeInTheDocument()
    expect(within(dialog).getAllByText(/自动匹配/).length).toBeGreaterThan(0)
    expect(within(dialog).queryByRole('button', { name: '预览数据' })).not.toBeInTheDocument()
    expect(api.wechatAnalyticsImport.import).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '确认导入' }))
    await waitFor(() => expect(api.wechatAnalyticsImport.import).toHaveBeenCalledWith(project.id, expect.objectContaining({ upload_id: 'upload-1', timezone: 'Asia/Shanghai' })))
  })

  it('shows official analytics in the same article stream', async () => {
    vi.mocked(api.wechatAnalyticsImport.articles).mockResolvedValue({
      items: [{
        publication: {
          ...awaitingArticle.publication,
          status: 'published',
          source: 'anban_api',
          manual_publish_required: false,
          analytics_status: 'official_available',
        },
        latest: {
          id: 'official-1', publication_id: 'publication-1', source: 'wechat_official_api',
          data_as_of_at: '2026-09-18T00:00:00Z', imported_at: '2026-09-18T08:00:00Z',
          read_users: 642, share_users: 51, read_completion_rate: 0.4,
        },
      }],
    })

    render(<WechatDataPage />)

    expect(await screen.findByText('官方接口')).toBeInTheDocument()
    expect(screen.getByText('642')).toBeInTheDocument()
    expect(screen.getByText('40.0%')).toBeInTheDocument()
  })
})
