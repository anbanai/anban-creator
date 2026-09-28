import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { contentAnalyticsApi } from '@/lib/content-analytics'
import { uploadToOSS } from '@/lib/direct-upload'
import type { Project } from '@/types'
import ContentImportDialog, { invalidateContentAnalytics } from './ContentImportDialog'
import { QueryClient } from '@tanstack/react-query'

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
  vi.mocked(contentAnalyticsApi.import).mockResolvedValue({ revision: 4, count: 1, date: '2026-09-24' })
  vi.mocked(contentAnalyticsApi.candidates).mockResolvedValue({ items: [{ target, title: '当前账号草稿', content_type: 'image_text', status: 'pending' }], total: 1 })
})
function setExplicitScope() {
  fireEvent.change(screen.getByLabelText('数据截至时间'), { target: { value: '2026-09-24T12:00' } })
  fireEvent.click(screen.getByLabelText('导入统计口径'))
  fireEvent.click(screen.getByRole('option', { name: '当日新增' }))
}
describe('统一导入确认', () => {
  it('defaults the data cutoff to the current Beijing minute and keeps it after replacing the file', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-28T00:12:34.000Z'))
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    expect(screen.getByLabelText('数据截至时间')).toHaveValue('2026-09-28T08:12')
    await upload()
    expect(screen.getByText(/文件已解析/)).toBeInTheDocument()
    expect(screen.getByText('data.xlsx')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('选择导入文件'), { target: { files: [new File(['fixture-2'], 'data-2.xlsx')] } })
    expect(screen.getByLabelText('数据截至时间')).toHaveValue('2026-09-28T08:12')
    vi.useRealTimers()
  })

  it('invalidates only analytics belonging to the imported project', async () => {
    const client = new QueryClient()
    const own = ['content-analytics', 'p1', 'overview']
    const other = ['content-analytics', 'p2', 'overview']
    const unrelated = ['task', 'other-task']
    for (const key of [own, other, unrelated]) client.setQueryData(key, { value: 1 })
    await invalidateContentAnalytics(client, 'p1')
    expect(client.getQueryState(own)?.isInvalidated).toBe(true)
    expect(client.getQueryState(other)?.isInvalidated).toBe(false)
    expect(client.getQueryState(unrelated)?.isInvalidated).toBe(false)
    client.clear()
  })
  it('fixes WeChat imports to cumulative while still requiring an explicit date', async () => {
    const wechat = { ...project, platform: 'article' as const }
    render(<ContentImportDialog project={wechat} onClose={vi.fn()} />)
    await upload()
    expect(screen.getByLabelText('导入统计口径')).toBeDisabled()
    expect(screen.getByLabelText('导入统计口径')).toHaveTextContent('累计数据')
    expect(screen.getByRole('button', { name: '确认导入 1 条' })).toBeEnabled()
    fireEvent.change(screen.getByLabelText('数据截至时间'), { target: { value: '2026-09-24T12:00' } })
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledWith(wechat, expect.objectContaining({ metric_basis: 'cumulative' })))
  })
  it('requires an explicit date and basis before importing Seednote', async () => {
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    await upload()
    expect(screen.getByRole('button', { name: '确认导入 1 条' })).toBeDisabled()
    setExplicitScope()
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledWith(project, expect.objectContaining({ metric_basis: 'daily', data_as_of_at: '2026-09-24T04:00:00.000Z', idempotency_key: expect.any(String) })))
  })
  it('retains the idempotency key on retry and replaces it after editing the request', async () => {
    vi.mocked(contentAnalyticsApi.import).mockRejectedValue(new Error('网络中断'))
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    await upload(); setExplicitScope()
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await screen.findByRole('alert')
    const first = vi.mocked(contentAnalyticsApi.import).mock.calls[0][1].idempotency_key
    expect(first).toMatch(/^[0-9a-f-]{36}$/)
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledTimes(2))
    expect(vi.mocked(contentAnalyticsApi.import).mock.calls[1][1].idempotency_key).toBe(first)
    await waitFor(() => expect(screen.getByLabelText('数据截至时间')).toBeEnabled())
    fireEvent.change(screen.getByLabelText('数据截至时间'), { target: { value: '2026-09-23T12:00' } })
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledTimes(3))
    expect(vi.mocked(contentAnalyticsApi.import).mock.calls[2][1].idempotency_key).not.toBe(first)
  })
  it('previews without importing and submits only explicitly selected linked rows', async () => {
    const onImported = vi.fn(); const onClose = vi.fn()
    render(<ContentImportDialog project={project} onClose={onClose} onImported={onImported} />)
    await upload(); setExplicitScope()
    expect(contentAnalyticsApi.import).not.toHaveBeenCalled()
    expect(screen.getByLabelText('选择第 3 行')).toBeDisabled()
    expect(screen.getByLabelText('选择第 4 行')).toBeDisabled()
    expect(screen.getByText('日期无效')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledWith(project, expect.objectContaining({ upload_id: 'upload-1', selections: [{ source_row: 2, target }] })))
    await waitFor(() => expect(onImported).toHaveBeenCalledWith({ revision: 4, count: 1, date: '2026-09-24' }))
    expect(onClose).toHaveBeenCalledOnce()
  })
  it('supports manual current-account all-status matching and blocks duplicate targets', async () => {
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    await upload(); setExplicitScope()
    fireEvent.click(screen.getByRole('button', { name: '第 3 行选择对应内容' }))
    fireEvent.click(await screen.findByRole('button', { name: /当前账号草稿/ }))
    expect(contentAnalyticsApi.candidates).toHaveBeenCalledWith('p1', expect.objectContaining({ offset: 0, limit: 25 }), expect.any(AbortSignal))
    expect(screen.getByRole('button', { name: '确认导入 2 条' })).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('同一内容只能导入一行')
    fireEvent.click(screen.getByLabelText('选择第 2 行'))
    fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
    await waitFor(() => expect(contentAnalyticsApi.import).toHaveBeenCalledWith(project, expect.objectContaining({ selections: [{ source_row: 3, target }] })))
  })
  it('keeps confirmation failures and choices for retry', async () => {
    vi.mocked(contentAnalyticsApi.import).mockRejectedValueOnce(new Error('匹配内容已改变，请重试'))
    render(<ContentImportDialog project={project} onClose={vi.fn()} />)
    await upload(); setExplicitScope(); fireEvent.click(screen.getByRole('button', { name: '确认导入 1 条' }))
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
