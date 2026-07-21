import { fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { FilePreviewGallery } from './FilePreview'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
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
      },
    },
  }
})

function fileWith(overrides: Partial<TaskFile>): TaskFile {
  return {
    id: 'file-1',
    task_id: 'task-1',
    role: 'output',
    file_name: 'article.html',
    mime_type: 'text/html',
    file_size: 42,
    url: '/api/v1/files/article.html',
    created_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('FilePreviewGallery', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses a stable 70vh frame for HTML previews', async () => {
    vi.mocked(api.tasks.fetchPreviewHTML).mockResolvedValue('<main>预览内容</main>')

    render(<FilePreviewGallery files={[fileWith({})]} taskId="task-1" />)

    fireEvent.click(screen.getByRole('button', { name: /预览/ }))

    const frame = await screen.findByTitle('文章预览')
    expect(frame).toHaveClass('h-[70vh]')
  })

  it('uses montage role labels when task type is montage', () => {
    render(
      <FilePreviewGallery
        files={[fileWith({
          role: 'final_video',
          file_name: 'final.mp4',
          mime_type: 'video/mp4',
          url: 'https://cdn.example.com/final.mp4',
        })]}
        taskId="task-montage"
        taskType="montage"
      />,
    )

    expect(screen.getByText(/最终视频/)).toBeInTheDocument()
  })
})
