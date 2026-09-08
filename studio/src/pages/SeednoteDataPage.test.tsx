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
    vi.mocked(api.seednoteImport.overview).mockResolvedValue({ dates: [], series: [], posts: [] })
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

  it('does not choose an account or load account data until the user explicitly selects one', async () => {
    render(<SeednoteDataPage />)

    const accountSelect = await screen.findByRole('combobox', { name: '小红书账号' })
    expect(accountSelect).toHaveValue('')
    expect(screen.getByText('请先选择小红书账号')).toBeInTheDocument()
    expect(api.seednoteImport.listBatches).not.toHaveBeenCalled()
    expect(api.seednoteImport.overview).not.toHaveBeenCalled()
    expect(api.seednoteImport.posts).not.toHaveBeenCalled()
  })

  it('shows the selected file for review and waits for an explicit parse action', async () => {
    render(<SeednoteDataPage />)

    fireEvent.change(await screen.findByRole('combobox', { name: '小红书账号' }), { target: { value: 'account-1' } })
    const file = new File(['xlsx'], '日报.xlsx', { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
    fireEvent.change(screen.getByLabelText('导入数据文件'), { target: { files: [file] } })

    expect(screen.getByText('日报.xlsx')).toBeInTheDocument()
    expect(uploadToOSS).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '确认导入' }))

    expect(screen.getByRole('dialog', { name: '确认这次导入' })).toBeInTheDocument()
    expect(uploadToOSS).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '开始解析' }))
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(1))
  })

  it('lets the user choose posts before selecting a file', async () => {
    render(<SeednoteDataPage />)

    fireEvent.change(await screen.findByRole('combobox', { name: '小红书账号' }), { target: { value: 'account-1' } })

    expect(await screen.findByText('优先匹配帖子')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: '夏日穿搭' })).toBeEnabled()
    expect(screen.getByRole('checkbox', { name: '秋日妆容' })).toBeEnabled()
  })
})
