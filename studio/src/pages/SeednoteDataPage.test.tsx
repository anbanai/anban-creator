import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { uploadToOSS } from '@/lib/direct-upload'
import { render } from '@/test/test-utils'
import SeednoteDataPage from './SeednoteDataPage'

const projects = [
  { id: 'account-1', user_id: 'user-1', platform: 'seednote', name: '主账号', avatar_url: '', profile_url: '', keywords: '', instructions: '', visual_style: '', writer: '', theme: '', author: '', image_ratio: '3:4', max_concurrent_tasks: 1, status: 'active', created_at: '', updated_at: '', config: {} },
  { id: 'account-2', user_id: 'user-1', platform: 'seednote', name: '副账号', avatar_url: '', profile_url: '', keywords: '', instructions: '', visual_style: '', writer: '', theme: '', author: '', image_ratio: '3:4', max_concurrent_tasks: 1, status: 'active', created_at: '', updated_at: '', config: {} },
]

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      projects: { ...actual.api.projects, list: vi.fn() },
      seednoteImport: {
        ...actual.api.seednoteImport,
        listBatches: vi.fn(),
        overview: vi.fn(),
        posts: vi.fn(),
        getBatch: vi.fn(),
        post: vi.fn(),
        import: vi.fn(),
      },
    },
  }
})

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: vi.fn() }
})

describe('SeednoteDataPage import flow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.projects.list).mockResolvedValue(projects as never)
    vi.mocked(api.seednoteImport.listBatches).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], series: [], posts: [], post_summaries: [] })
    vi.mocked(api.seednoteImport.posts).mockResolvedValue({
      items: [
        { id: 'post-1', title: '夏日穿搭', first_published_at: '2026-09-01T08:00:00+08:00' },
        { id: 'post-2', title: '秋日妆容', first_published_at: '2026-09-02T08:00:00+08:00' },
      ],
      total: 2,
    })
    vi.mocked(api.seednoteImport.import).mockResolvedValue({
      batch: {
        id: 'batch-1', project_id: 'account-1', file_name: '日报.xlsx', file_size: 4,
        received_at: '2026-09-08T08:00:00+08:00', data_as_of_at: '2026-09-08T23:59:00+08:00',
        timezone: 'Asia/Shanghai', status: 'completed', total_rows: 2, resolved_rows: 2,
        review_rows: 0, invalid_rows: 0,
      },
      rows: [],
    })
    vi.mocked(uploadToOSS).mockResolvedValue({ uploadSessionId: 'session-1', uploadId: 'asset-1', key: 'key', previewUrl: '', publicUrl: '', contentType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', size: 10 })
  })

  it('opens full note URLs and falls back to public note IDs without using internal IDs', async () => {
    const summaries = [
      { id: 'post-1', title: '完整链接', note_id: 'note-one', note_url: 'https://www.xiaohongshu.com/explore/note-one?xsec_token=example' },
      { id: 'post-2', title: '只有笔记ID', note_id: 'note-two' },
      { id: 'internal-only', title: '未关联' },
    ]
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], series: [], posts: [], post_summaries: summaries })
    render(<SeednoteDataPage />)
    fireEvent.change(await screen.findByRole('combobox', { name: '种草笔记账号' }), { target: { value: 'account-1' } })
    const links = await screen.findAllByRole('link', { name: '查看原文' })
    expect(links.map((link) => link.getAttribute('href'))).toEqual([
      'https://www.xiaohongshu.com/explore/note-one?xsec_token=example',
      'https://www.xiaohongshu.com/explore/note-two',
    ])
    expect(links.every((link) => link.getAttribute('target') === '_blank')).toBe(true)
  })

  it('does not choose an account or load account data until the user explicitly selects one', async () => {
    render(<SeednoteDataPage />)

    const accountSelect = await screen.findByRole('combobox', { name: '种草笔记账号' })
    expect(accountSelect).toHaveValue('')
    expect(screen.getByText('请选择账号后查看数据看板')).toBeInTheDocument()
    expect(api.seednoteImport.listBatches).not.toHaveBeenCalled()
    expect(api.seednoteImport.overview).not.toHaveBeenCalled()
    expect(api.seednoteImport.posts).not.toHaveBeenCalled()
  })

  it('opens on the dashboard with period shortcuts and multiple metric toggles', async () => {
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({
      dates: ['2026-09-01'],
      series: [{ date: '2026-09-01', exposure_count: 100, view_count: 80, like_count: 12, comment_count: 4, collect_count: 9, follower_gain_count: 2, share_count: 1, barrage_count: 0 }],
      posts: [],
      post_summaries: [],
    } as never)
    render(<SeednoteDataPage />)

    fireEvent.change(await screen.findByRole('combobox', { name: '种草笔记账号' }), { target: { value: 'account-1' } })

    expect(await screen.findByText('数据看板')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '本周' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '本月' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '近 7 天' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '近 30 天' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '自定义' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '评论' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '收藏' })).toBeInTheDocument()
    expect(screen.queryByText('优先匹配帖子')).not.toBeInTheDocument()
  })

  it('uploads and imports immediately after selecting a valid file', async () => {
    render(<SeednoteDataPage />)

    fireEvent.change(await screen.findByRole('combobox', { name: '种草笔记账号' }), { target: { value: 'account-1' } })
    fireEvent.click(await screen.findByRole('button', { name: '导入数据' }))
    const file = new File(['xlsx'], '日报.xlsx', { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
    fireEvent.change(screen.getByLabelText('导入数据文件'), { target: { files: [file] } })

    expect(screen.getByText('日报.xlsx')).toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '导入数据' })).toBeInTheDocument()
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(api.seednoteImport.import).toHaveBeenCalledWith('account-1', expect.objectContaining({ upload_id: 'session-1', timezone: 'Asia/Shanghai' })))
  })

  it('surfaces invalid import rows instead of showing a false completion state', async () => {
    vi.mocked(api.seednoteImport.import).mockResolvedValue({
      batch: {
        id: 'batch-invalid', project_id: 'account-1', file_name: '日报.xlsx', file_size: 4,
        received_at: '2026-09-08T08:00:00+08:00', data_as_of_at: '2026-09-08T23:59:00+08:00',
        timezone: 'Asia/Shanghai', status: 'needs_review', total_rows: 2, resolved_rows: 1,
        review_rows: 0, invalid_rows: 1,
      },
      rows: [],
    })
    vi.mocked(api.seednoteImport.getBatch).mockResolvedValue({
      batch: {
        id: 'batch-invalid', project_id: 'account-1', file_name: '日报.xlsx', file_size: 4,
        received_at: '2026-09-08T08:00:00+08:00', data_as_of_at: '2026-09-08T23:59:00+08:00',
        timezone: 'Asia/Shanghai', status: 'needs_review', total_rows: 2, resolved_rows: 1,
        review_rows: 0, invalid_rows: 1,
      },
      rows: [{ id: 'row-invalid', source_row: 4, title: '无法解析', match_status: 'invalid', parse_error: '缺少曝光字段' }],
    })
    render(<SeednoteDataPage />)

    fireEvent.change(await screen.findByRole('combobox', { name: '种草笔记账号' }), { target: { value: 'account-1' } })
    fireEvent.click(await screen.findByRole('button', { name: '导入数据' }))
    const file = new File(['xlsx'], '日报.xlsx', { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
    fireEvent.change(screen.getByLabelText('导入数据文件'), { target: { files: [file] } })
    await waitFor(() => expect(api.seednoteImport.import).toHaveBeenCalledTimes(1))
    expect(await screen.findByText('导入存在无法解析的行')).toBeInTheDocument()
    expect(screen.queryByText('导入完成')).not.toBeInTheDocument()
  })

  it('does not ask the user to choose priority posts before importing', async () => {
    render(<SeednoteDataPage />)

    fireEvent.change(await screen.findByRole('combobox', { name: '种草笔记账号' }), { target: { value: 'account-1' } })

    expect(await screen.findByText('数据看板')).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '夏日穿搭' })).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '秋日妆容' })).not.toBeInTheDocument()
  })
})
