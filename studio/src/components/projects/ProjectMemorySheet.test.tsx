import { fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import { ProjectMemorySheet } from './ProjectMemorySheet'
import { projectsApi } from '@/lib/api/projects'

vi.mock('@/lib/api/projects', () => ({
  projectsApi: { memory: vi.fn() },
}))

describe('ProjectMemorySheet', () => {
  beforeEach(() => vi.mocked(projectsApi.memory).mockReset())

  it('loads only when opened, prioritizes MEMORY.md, switches files, and blocks images', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({
      status: 'ready',
      updated_at: '2026-09-14T12:00:00Z',
      partial: true,
      files: [
        { path: 'MEMORY.md', content: '# Main\n![remote](https://example.com/a.png)', size_bytes: 42, modified_at: '2026-09-14T12:00:00Z', truncated: false },
        { path: 'notes/preferences.md', content: 'second file', size_bytes: 11, modified_at: '2026-09-14T11:00:00Z', truncated: true },
      ],
    })
    render(<ProjectMemorySheet projectId="project-1" projectName="公众号项目" open={false} onOpenChange={() => {}} />)
    expect(projectsApi.memory).not.toHaveBeenCalled()

    const { rerender } = render(<ProjectMemorySheet projectId="project-1" projectName="公众号项目" open onOpenChange={() => {}} />)
    expect(await screen.findByRole('heading', { name: 'Main' })).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('内容已按安全限制截断')).toBeInTheDocument()
    expect(screen.getByText('MEMORY.md', { selector: 'h2' })).toBeInTheDocument()
    expect(screen.getByText('项目记忆文件')).toBeInTheDocument()
    expect(screen.getByRole('main')).toHaveClass('min-w-0')
    expect(screen.getByRole('article')).toHaveClass('max-w-3xl')

    fireEvent.click(screen.getByRole('button', { name: 'notes/preferences.md' }))
    expect(screen.getByText('second file')).toBeInTheDocument()
    expect(screen.getByText('此文件已截断')).toBeInTheDocument()
    expect(screen.getByText('notes/preferences.md', { selector: 'h2' })).toBeInTheDocument()
    rerender(<div />)
  })

  it('shows empty and error states and supports refresh', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValueOnce({ status: 'empty', updated_at: null, partial: false, files: [] })
    render(<ProjectMemorySheet projectId="project-2" projectName="空项目" open onOpenChange={() => {}} />)
    expect(await screen.findByText('还没有项目记忆')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '刷新记忆' }))
    expect(projectsApi.memory).toHaveBeenCalledTimes(2)
  })
})
