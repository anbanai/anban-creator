import { fireEvent, screen } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { describe, expect, it } from 'vitest'
import { TaskFileDownloads } from './TaskStageFiles'

const files = [
  { id: 'delivered', task_id: 'task-1', state: 'delivered' as const, role: 'content', file_name: 'final.md', mime_type: 'text/markdown', file_size: 10, url: '', is_deliverable: true, created_at: '' },
  { id: 'retained', task_id: 'task-1', state: 'retained' as const, role: 'content', file_name: 'draft.md', mime_type: 'text/markdown', file_size: 10, url: '', created_at: '' },
]

describe('TaskFileDownloads', () => {
  it('offers one download icon and lets users choose the artifact set', async () => {
    render(<TaskFileDownloads taskId="task-1" files={files} />)

    fireEvent.click(screen.getByRole('button', { name: '下载产物' }))

    expect(await screen.findByRole('menuitem', { name: '下载交付成果' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '下载已保留产物' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /下载/ })).toHaveLength(1)
  })
})
