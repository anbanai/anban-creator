import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { TaskDeliverablesPanel } from './TaskDeliverablesPanel'

const files = [
  { id: 'core', task_id: 'task-1', state: 'delivered' as const, role: 'content', file_name: 'final.md', mime_type: 'text/markdown', file_size: 10, url: '', is_deliverable: true, delivery_role: 'content', created_at: '' },
  { id: 'internal', task_id: 'task-1', state: 'delivered' as const, role: 'review', file_name: 'review.json', mime_type: 'application/json', file_size: 10, url: '', is_deliverable: false, producer_stage_id: 'review', created_at: '' },
]

describe('TaskDeliverablesPanel', () => {
  it('shows and downloads only core delivery files to ordinary users', () => {
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" files={files} isAdmin={false} />)

    expect(screen.getByText('final.md')).toBeInTheDocument()
    expect(screen.queryByText('review.json')).not.toBeInTheDocument()
    expect(screen.queryByText('内部文件')).not.toBeInTheDocument()
  })

  it('lets admins preview internal files in a separate grouped disclosure', () => {
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" files={files} isAdmin />)

    expect(screen.getByText('final.md')).toBeInTheDocument()
    fireEvent.click(screen.getByText('内部文件 (1)'))
    expect(screen.getByText('review.json')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'review' })).toBeInTheDocument()
  })

  it('keeps sharing reachable after failed continuation but hides unsupported task kinds', () => {
    const { rerender } = render(<TaskDeliverablesPanel taskId="task-1" taskStatus="failed" taskType="seednote" taskKind="content_generation" files={files} isAdmin={false} />)
    expect(screen.getByRole('button', { name: '分享作品' })).toBeEnabled()
    rerender(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" taskType="seednote" taskKind="analysis" files={files} isAdmin={false} />)
    expect(screen.queryByRole('button', { name: '分享作品' })).not.toBeInTheDocument()
  })

  it('opens sharing beside the trigger without hiding the delivery page', async () => {
    server.use(http.get('/api/v1/tasks/task-1/share', () => HttpResponse.json({ data: { status: 'none' } })))
    render(<TaskDeliverablesPanel taskId="task-1" taskStatus="completed" taskType="seednote" files={files} isAdmin={false} />)
    fireEvent.click(screen.getByRole('button', { name: '分享作品' }))
    expect(await screen.findByRole('dialog', { name: '分享作品' })).toBeVisible()
    expect(screen.getByRole('heading', { name: '交付结果' })).toBeVisible()
    expect(screen.getByText('final.md')).toBeVisible()
    expect(screen.queryByRole('region', { name: '分享设置' })).not.toBeInTheDocument()
  })
})
