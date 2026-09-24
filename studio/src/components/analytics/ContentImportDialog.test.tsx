import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { contentAnalyticsApi } from '@/lib/content-analytics'
import { uploadToOSS } from '@/lib/direct-upload'
import type { Project } from '@/types'
import ContentImportDialog from './ContentImportDialog'

vi.mock('@/lib/direct-upload', () => ({ uploadToOSS: vi.fn() }))
vi.mock('@/lib/content-analytics', async () => ({ ...await vi.importActual('@/lib/content-analytics'), contentAnalyticsApi: { preview: vi.fn(), import: vi.fn(), candidates: vi.fn() } }))
const project = { id: 'p1', name: '当前账号', platform: 'seednote' } as Project
const target = { kind: 'task' as const, id: 'task-1' }
const preview = { file_name: 'data.xlsx', total_rows: 3, rows: [
  { source_row: 2, title: '已识别内容', content_type: 'image_text', match_status: 'matched', target },
  { source_row: 3, title: '未识别内容', content_type: 'video', match_status: 'unmatched' },
  { source_row: 4, title: '错误内容', content_type: 'unknown', match_status: 'invalid', parse_error: '日期无效' },
] }
async function upload() {
  fireEvent.change(screen.getByLabelText('选择导入文件'), { target: { files: [new File(['fixture'], 'data.xlsx')] } })
  await screen.findByText('已识别内容', { selector: 'p' })
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(uploadToOSS).mockResolvedValue({ uploadSessionId: 'upload-1' } as never)
  vi.mocked(contentAnalyticsApi.preview).mockResolvedValue(preview)
  vi.mocked(contentAnalyticsApi.import).mockResolvedValue({ count: 1, date: '2026-09-24' })
  vi.mocked(contentAnalyticsApi.candidates).mockResolvedValue({ items: [{ target, title: '当前账号草稿', content_type: 'image_text', status: 'pending' }], total: 1 })
})
describe('统一导入确认', () => {
  it('previews without importing and submits only explicitly selected linked rows', async () => {
    const onImported = vi.fn(); const onClose = vi.fn()
    render(<ContentImportDialog project={project} onClose={onClose} onImported={onImported} />)
    await upload()
    expect(contentAnalyticsApi.import).not.toHaveBeenCalled()
    expect(screen.getByLabelText('选择第 3 行')).toBeDisabled()
    expect(screen.getByLabelText('选择第 4 行')).toBeDisabled()
    expect(screen.getByText('日期无效')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledWith(project, expect.objectContaining({ upload_id: 'upload-1', selections: [{ source_row: 2, target }] })))
    await waitFor(() => expect(onImported).toHaveBeenCalledWith({ count: 1, date: '2026-09-24' }))
    expect(onClose).toHaveBeenCalledOnce()
  })
  it('supports manual current-account all-status matching and blocks duplicate targets', async () => {
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    await upload()
    fireEvent.click(screen.getByRole('button', { name: '第 3 行选择对应内容' }))
    fireEvent.click(await screen.findByRole('button', { name: /当前账号草稿/ }))
    expect(contentAnalyticsApi.candidates).toHaveBeenCalledWith('p1', expect.objectContaining({ offset: 0, limit: 25 }))
    expect(screen.getByRole('button', { name: '确认导入 2 条' })).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('同一内容只能导入一行')
    fireEvent.click(screen.getByLabelText('选择第 2 行'))
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledWith(project, expect.objectContaining({ selections: [{ source_row: 3, target }] })))
  })
  it('keeps confirmation failures and choices for retry', async () => {
    vi.mocked(contentAnalyticsApi.import).mockRejectedValueOnce(new Error('匹配内容已改变，请重试'))
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    await upload(); fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('匹配内容已改变')
    expect(screen.getByLabelText('选择第 2 行')).toBeChecked()
    expect(screen.getByRole('button', { name: '确认导入 1 条' })).toBeEnabled()
  })
  it('does not upload unsupported files, and prevents importing no matches', async () => {
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('选择导入文件'), { target: { files: [new File(['x'], 'data.xls')] } })
    expect(uploadToOSS).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('.xlsx')
    vi.mocked(contentAnalyticsApi.preview).mockResolvedValue({ ...preview, rows: preview.rows.slice(1), total_rows: 2 })
    fireEvent.change(screen.getByLabelText('选择导入文件'), { target: { files: [new File(['x'], 'data.xlsx')] } })
    await screen.findByText('未识别内容')
    expect(within(screen.getByRole('dialog')).getByRole('button', { name: '确认导入' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: '新建帖子' })).not.toBeInTheDocument()
  })
})
