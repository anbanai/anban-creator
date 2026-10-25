import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createTestQueryClient } from '@/test/test-utils'
import UserMediaLibraryPage from './UserMediaLibraryPage'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  projects: vi.fn(),
  update: vi.fn(),
  reanalyze: vi.fn(),
  remove: vi.fn(),
  upload: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    userMedia: { list: mocks.list, update: mocks.update, reanalyze: mocks.reanalyze, remove: mocks.remove, create: vi.fn() },
    projects: { list: mocks.projects },
  },
}))

vi.mock('@/lib/direct-upload', () => ({ uploadToOSS: mocks.upload }))

function renderPage() {
  return render(<QueryClientProvider client={createTestQueryClient()}><MemoryRouter><UserMediaLibraryPage /></MemoryRouter></QueryClientProvider>)
}

describe('UserMediaLibraryPage', () => {
  afterEach(() => vi.clearAllMocks())

  it('shows analysis states, supports notes, and retries failed assets', async () => {
    mocks.projects.mockResolvedValue([{ id: 'project-1', name: '新品项目' }])
    mocks.list.mockResolvedValue({ items: [
      { id: 'media-1', file_name: 'product.png', media_type: 'image', content_type: 'image/png', size: 10, status: 'available', description: '白色包装正面', usage_notes: '', project_id: 'project-1', recommended_uses: ['商品主图'], created_at: '', updated_at: '' },
      { id: 'media-2', file_name: 'demo.mp4', media_type: 'video', content_type: 'video/mp4', size: 20, status: 'failed', error_message: '模型不支持该视频', created_at: '', updated_at: '' },
    ], total: 2, offset: 0, limit: 50 })
    mocks.update.mockResolvedValue({})
    mocks.reanalyze.mockResolvedValue({ id: 'media-2', status: 'queued' })
    renderPage()

    expect(await screen.findByText('白色包装正面')).toBeInTheDocument()
    expect(screen.getByText('模型不支持该视频')).toBeInTheDocument()
    const notes = screen.getByRole('textbox', { name: 'product.png 使用说明' })
    fireEvent.change(notes, { target: { value: '仅用于新品发布' } })
    fireEvent.blur(notes)
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith('media-1', { usage_notes: '仅用于新品发布' }))
    fireEvent.click(within(screen.getByRole('heading', { name: 'demo.mp4' }).closest('article')!).getByRole('button', { name: /重新解析/ }))
    await waitFor(() => expect(mocks.reanalyze).toHaveBeenCalledWith('media-2'))
  })

  it('uploads an image through the media-library purpose', async () => {
    mocks.projects.mockResolvedValue([])
    mocks.list.mockResolvedValue({ items: [], total: 0, offset: 0, limit: 50 })
    mocks.upload.mockResolvedValue({ uploadSessionId: 'upload-1' })
    renderPage()
    const file = new File(['png'], 'new.png', { type: 'image/png' })
    fireEvent.change(await screen.findByLabelText('上传素材'), { target: { files: [file] } })
    await waitFor(() => expect(mocks.upload).toHaveBeenCalledWith(expect.objectContaining({ purpose: 'media_library', file })))
  })

  it('shows upload failures without losing the library page', async () => {
    mocks.projects.mockResolvedValue([])
    mocks.list.mockResolvedValue({ items: [], total: 0, offset: 0, limit: 50 })
    mocks.upload.mockRejectedValueOnce(new Error('视频超过模型支持的大小限制'))
    renderPage()
    const file = new File(['video'], 'long.mp4', { type: 'video/mp4' })
    fireEvent.change(await screen.findByLabelText('上传素材'), { target: { files: [file] } })
    expect(await screen.findByRole('alert')).toHaveTextContent('视频超过模型支持的大小限制')
  })
})
