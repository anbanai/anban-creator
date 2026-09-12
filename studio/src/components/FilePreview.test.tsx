import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { FilePreviewGallery } from './FilePreview'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import { downloadBlob, isDesktop, saveUrlToFile } from '@/lib/tauri'
import type { TaskFile } from '@/types'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        fetchPreviewHTML: vi.fn(),
        downloadFileBlob: vi.fn(),
        previewFileBlob: vi.fn(),
      },
    },
  }
})

vi.mock('@/lib/tauri', () => ({
  downloadBlob: vi.fn(),
  isDesktop: vi.fn(() => false),
  saveUrlToFile: vi.fn(),
}))

function fileWith(overrides: Partial<TaskFile>): TaskFile {
  return {
    id: 'file-1',
    task_id: 'task-1',
    role: 'output',
    file_name: 'article.html',
    mime_type: 'text/html',
    file_size: 42,
    url: '/api/v1/files/article.html',
    is_deliverable: true,
    created_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('FilePreviewGallery', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(isDesktop).mockReturnValue(false)
    vi.mocked(saveUrlToFile).mockResolvedValue(false)
    vi.mocked(downloadBlob).mockResolvedValue(undefined)
  })

  it('uses a stable 70vh frame for HTML previews', async () => {
    vi.mocked(api.tasks.fetchPreviewHTML).mockResolvedValue('<main>预览内容</main>')

    render(<FilePreviewGallery files={[fileWith({})]} taskId="task-1" />)

    fireEvent.click(screen.getByRole('button', { name: /预览/ }))

    const frame = await screen.findByTitle('文章预览')
    expect(frame).toHaveClass('h-[70vh]')
    expect(frame).toHaveAttribute('sandbox', '')
  })

  it('uses the file preview endpoint for HTML files when the server provides preview_url', async () => {
    vi.mocked(api.tasks.previewFileBlob).mockResolvedValue(new Blob([
      '<main>过程 HTML</main>',
    ], { type: 'text/html' }))
    const htmlFile = fileWith({
      id: 'html-process',
      file_name: 'review.html',
      url: '',
      preview_url: '/api/v1/tasks/task-1/files/html-process/preview',
      is_deliverable: false,
    })

    render(<FilePreviewGallery files={[htmlFile]} taskId="task-1" />)

    fireEvent.click(screen.getByRole('button', { name: '预览 review.html' }))

    const frame = await screen.findByTitle('文章预览')
    expect(frame).toHaveAttribute('srcdoc', '<main>过程 HTML</main>')
    expect(api.tasks.previewFileBlob).toHaveBeenCalledWith('task-1', 'html-process')
    expect(api.tasks.fetchPreviewHTML).not.toHaveBeenCalled()
  })

  it('uses montage role labels when task type is montage', () => {
    render(
      <FilePreviewGallery
        files={[fileWith({
          role: 'source_manifest',
          delivery_role: 'final_video',
          file_name: 'final.mp4',
          mime_type: 'video/mp4',
          url: 'https://cdn.example.com/final.mp4',
        })]}
        taskId="task-montage"
        taskType="montage"
      />,
    )

    expect(screen.getByText(/最终视频/)).toBeInTheDocument()
    expect(screen.queryByText(/素材清单/)).not.toBeInTheDocument()
  })

  it('streams a deliverable video from an absolute signed preview URL without loading a blob', async () => {
    const videoFile = fileWith({
      id: 'video-1',
      role: 'final_video',
      file_name: 'final.mp4',
      mime_type: 'video/mp4',
      url: 'https://cdn.example.com/final.mp4?signature=secret',
      preview_url: 'https://cdn.example.com/final.mp4?signature=secret',
      download_url: '/api/v1/tasks/task-1/files/video-1/download',
    })

    render(<FilePreviewGallery files={[videoFile]} taskId="task-1" taskType="montage" />)
    fireEvent.click(screen.getByRole('button', { name: '预览 final.mp4' }))

    await waitFor(() => {
      expect(document.querySelector('video')).toHaveAttribute(
        'src',
        'https://cdn.example.com/final.mp4?signature=secret',
      )
    })
    expect(api.tasks.previewFileBlob).not.toHaveBeenCalled()
    expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()
  })

  it('uses the authenticated download endpoint when download_url is relative even if file.url is absolute', async () => {
    vi.mocked(isDesktop).mockReturnValue(true)
    vi.mocked(api.tasks.downloadFileBlob).mockResolvedValue(new Blob(['video']))
    const videoFile = fileWith({
      id: 'video-1',
      role: 'final_video',
      file_name: 'final.mp4',
      mime_type: 'video/mp4',
      url: 'https://cdn.example.com/final.mp4?signature=secret',
      preview_url: 'https://cdn.example.com/final.mp4?signature=secret',
      download_url: '/api/v1/tasks/task-1/files/video-1/download',
    })

    render(<FilePreviewGallery files={[videoFile]} taskId="task-1" taskType="montage" />)
    fireEvent.click(screen.getByRole('button', { name: '下载 final.mp4' }))

    await waitFor(() => {
      expect(api.tasks.downloadFileBlob).toHaveBeenCalledWith('task-1', 'video-1')
    })
    expect(saveUrlToFile).not.toHaveBeenCalled()
    expect(downloadBlob).toHaveBeenCalledWith('final.mp4', expect.any(Blob))
  })

  it('starts a browser download from an absolute signed attachment URL without loading a blob', async () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const videoFile = fileWith({
      id: 'video-1',
      file_name: 'final.mp4',
      mime_type: 'video/mp4',
      url: 'https://cdn.example.com/final.mp4?preview=1',
      download_url: 'https://download.example.com/final.mp4?attachment=1',
    })

    render(<FilePreviewGallery files={[videoFile]} taskId="task-1" />)
    fireEvent.click(screen.getByRole('button', { name: '下载 final.mp4' }))

    await waitFor(() => expect(click).toHaveBeenCalledTimes(1))
    expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()
    expect(downloadBlob).not.toHaveBeenCalled()
    click.mockRestore()
  })

  it('loads a failed JSON preview once and only retries on demand', async () => {
    const error = Object.assign(new Error('too many requests'), { response: { status: 429 } })
    vi.mocked(api.tasks.previewFileBlob).mockRejectedValue(error)
    const failureFile = fileWith({
      file_name: 'failure-state.json',
      mime_type: 'application/json',
      url: '',
      preview_url: '/api/v1/tasks/task-1/files/failure-state/preview',
      is_deliverable: false,
    })

    const view = render(<FilePreviewGallery files={[failureFile]} taskId="task-1" />)
    fireEvent.click(screen.getByRole('button', { name: /预览/ }))

    expect(await screen.findByRole('alert')).toHaveTextContent('请求过于频繁，请稍后再试')
    expect(api.tasks.previewFileBlob).toHaveBeenCalledTimes(1)

    view.rerender(<FilePreviewGallery files={[{ ...failureFile }]} taskId="task-1" />)
    await act(async () => { await Promise.resolve() })
    expect(api.tasks.previewFileBlob).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(api.tasks.previewFileBlob).toHaveBeenCalledTimes(2))
  })

  it('renders a JSON preview with one download request', async () => {
    vi.mocked(api.tasks.downloadFileBlob).mockResolvedValue(new Blob([
      '{"stage":"failed","recoverable":true}',
    ], { type: 'application/json' }))

    render(
      <FilePreviewGallery
        files={[fileWith({
          file_name: 'failure-state.json',
          mime_type: 'application/json',
          url: '/api/v1/files/failure-state.json',
        })]}
        taskId="task-1"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /预览/ }))

    expect(await screen.findByText('{"stage":"failed","recoverable":true}')).toBeInTheDocument()
    expect(api.tasks.downloadFileBlob).toHaveBeenCalledTimes(1)
  })

  it('previews process files through the authenticated preview endpoint and disables download', async () => {
    vi.mocked(api.tasks.previewFileBlob).mockResolvedValue(new Blob([
      '{"stage":"review","internal":true}',
    ], { type: 'application/json' }))
    const processFile = fileWith({
      id: 'process-1',
      file_name: 'review.json',
      mime_type: 'application/json',
      url: '',
      preview_url: '/api/v1/tasks/task-1/files/process-1/preview',
      is_deliverable: false,
    })

    render(<FilePreviewGallery files={[processFile]} taskId="task-1" />)

    const downloadButton = screen.getByRole('button', { name: '下载 review.json' })
    expect(downloadButton).toBeDisabled()
    expect(downloadButton).toHaveAttribute('title', '仅支持预览')
    expect(screen.getByLabelText('仅支持预览')).toBeInTheDocument()
    expect(screen.queryByText(/过程文件/)).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '预览 review.json' }))

    expect(await screen.findByText('{"stage":"review","internal":true}')).toBeInTheDocument()
    expect(api.tasks.previewFileBlob).toHaveBeenCalledWith('task-1', 'process-1')
    expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()
  })

  it('does not use legacy storage or download endpoints for process files without preview_url', async () => {
    const processFile = fileWith({
      id: 'legacy-process',
      file_name: 'review.json',
      mime_type: 'application/json',
      url: '/api/v1/files/review.json',
      preview_url: undefined,
      is_deliverable: false,
    })

    render(<FilePreviewGallery files={[processFile]} taskId="task-1" />)
    fireEvent.click(screen.getByRole('button', { name: '预览 review.json' }))

    await waitFor(() => {
      expect(api.tasks.previewFileBlob).not.toHaveBeenCalled()
      expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()
      expect(api.tasks.fetchPreviewHTML).not.toHaveBeenCalled()
    })
  })

  it('does not use the task-wide HTML preview endpoint for process HTML without preview_url', async () => {
    const processFile = fileWith({
      id: 'legacy-process-html',
      file_name: 'review.html',
      mime_type: 'text/html',
      url: '/api/v1/files/review.html',
      preview_url: undefined,
      is_deliverable: false,
    })

    render(<FilePreviewGallery files={[processFile]} taskId="task-1" />)
    fireEvent.click(screen.getByRole('button', { name: '预览 review.html' }))

    await waitFor(() => {
      expect(api.tasks.fetchPreviewHTML).not.toHaveBeenCalled()
      expect(api.tasks.previewFileBlob).not.toHaveBeenCalled()
      expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()
    })
  })
})
