import { fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import { ProjectMemoryDialog } from './ProjectMemoryDialog'
import { projectsApi } from '@/lib/api/projects'

vi.mock('@/lib/api/projects', () => ({
  projectsApi: { memory: vi.fn() },
}))

describe('ProjectMemoryDialog', () => {
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
    render(<ProjectMemoryDialog projectId="project-1" projectName="公众号项目" open={false} onOpenChange={() => {}} />)
    expect(projectsApi.memory).not.toHaveBeenCalled()

    const { rerender } = render(<ProjectMemoryDialog projectId="project-1" projectName="公众号项目" open onOpenChange={() => {}} />)
    expect(await screen.findByRole('heading', { name: 'Main' })).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('内容已按安全限制截断')).toBeInTheDocument()
    expect(screen.getByText('MEMORY.md', { selector: 'h2' })).toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '项目记忆' })).toHaveAttribute('data-slot', 'dialog-content')
    expect(screen.getByRole('region', { name: '记忆正文' })).toBeInTheDocument()
    const readingPane = screen.getByRole('region', { name: '记忆正文' })
    readingPane.scrollTop = 400

    fireEvent.click(screen.getByRole('button', { name: 'notes/preferences.md' }))
    expect(screen.getByText('second file')).toBeInTheDocument()
    expect(readingPane.scrollTop).toBe(0)
    expect(screen.getByRole('button', { name: 'notes/preferences.md' })).toHaveAttribute('aria-current', 'true')
    expect(screen.getByText('此文件已截断')).toBeInTheDocument()
    expect(screen.getByText('notes/preferences.md', { selector: 'h2' })).toBeInTheDocument()
    rerender(<div />)
  })

  it('shows the empty state and supports refresh', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    render(<ProjectMemoryDialog projectId="project-2" projectName="空项目" open onOpenChange={() => {}} />)
    expect(await screen.findByText('还没有项目记忆')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '刷新记忆' }))
    expect(projectsApi.memory).toHaveBeenCalledTimes(2)
  })

  it('supports the compact file selector and close control', async () => {
    const onOpenChange = vi.fn()
    vi.mocked(projectsApi.memory).mockResolvedValue({
      status: 'ready', updated_at: null, partial: false,
      files: [
        { path: 'anban-article/MEMORY.md', content: '项目概要', size_bytes: 12, modified_at: '2026-09-22T03:45:00Z', truncated: false },
        { path: 'notes/style.md', content: '风格偏好', size_bytes: 12, modified_at: '2026-09-22T03:45:00Z', truncated: false },
      ],
    })
    render(<ProjectMemoryDialog projectId="compact" projectName="项目" open onOpenChange={onOpenChange} />)
    expect(await screen.findByText('项目概要')).toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox', { name: '选择记忆文件' }), { target: { value: 'notes/style.md' } })
    expect(screen.getByText('风格偏好')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭项目记忆' }))
    expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything())
  })
  it('shows a recoverable error and retries without closing the dialog', async () => {
    vi.mocked(projectsApi.memory)
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ status: 'empty', updated_at: null, partial: false, files: [] })
    render(<ProjectMemoryDialog projectId="retry" projectName="项目" open onOpenChange={() => {}} />)
    expect(await screen.findByText('无法读取项目记忆')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('还没有项目记忆')).toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '项目记忆' })).toBeInTheDocument()
    expect(projectsApi.memory).toHaveBeenCalledTimes(2)
  })

})
