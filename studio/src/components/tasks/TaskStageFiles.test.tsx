import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TaskFileDownloads } from './TaskStageFiles'
import { api } from '@/lib/api'

const files = [
  { id: 'delivered', task_id: 'task-1', state: 'delivered' as const, role: 'content', file_name: 'final.md', mime_type: 'text/markdown', file_size: 10, url: '', is_deliverable: true, created_at: '' },
  { id: 'retained', task_id: 'task-1', state: 'retained' as const, role: 'content', file_name: 'draft.md', mime_type: 'text/markdown', file_size: 10, url: '', created_at: '' },
]

describe('TaskFileDownloads', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('offers one download icon and lets users choose the artifact set', async () => {
    render(<TaskFileDownloads taskId="task-1" files={files} />)

    const downloadButton = screen.getByRole('button', { name: '下载产物' })
    expect(downloadButton).toHaveClass('size-11')
    fireEvent.click(downloadButton)

    expect(await screen.findByRole('menuitem', { name: '下载交付成果' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '下载已保留产物' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /下载/ })).toHaveLength(1)
  })

  it('keeps a single icon while a download is loading', async () => {
    vi.spyOn(api.tasks, 'downloadZipBlob').mockImplementation(() => new Promise(() => {}))
    render(<TaskFileDownloads taskId="task-1" files={[files[0]]} />)

    const downloadButton = screen.getByRole('button', { name: '下载交付成果 (ZIP)' })
    expect(downloadButton.querySelectorAll('svg')).toHaveLength(1)
    fireEvent.click(downloadButton)
    await waitFor(() => expect(downloadButton).toHaveAttribute('aria-busy', 'true'))
    expect(downloadButton.querySelectorAll('svg')).toHaveLength(1)
  })

  it('shows one busy icon after choosing an artifact set from the download menu', async () => {
    vi.spyOn(api.tasks, 'downloadZipBlob').mockImplementation(() => new Promise(() => {}))
    render(<TaskFileDownloads taskId="task-1" files={files} />)

    fireEvent.click(screen.getByRole('button', { name: '下载产物' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: '下载交付成果' }))
    const downloadButton = screen.getByRole('button', { name: '下载产物' })
    await waitFor(() => expect(downloadButton).toHaveAttribute('aria-busy', 'true'))
    expect(downloadButton).toBeDisabled()
    expect(downloadButton.querySelectorAll('svg')).toHaveLength(1)
  })
})
