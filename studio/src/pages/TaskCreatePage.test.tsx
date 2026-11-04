import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import type { TaskFormDialogProps } from '@/components/tasks/TaskFormDialog'
import type { Task } from '@/types'
import TaskCreatePage from './TaskCreatePage'
vi.mock('@/components/tasks/TaskFormDialog', () => ({ TaskFormDialog: (props: TaskFormDialogProps) => <><output data-testid="defaults">{JSON.stringify({ project: props.initialProjectId, asset: props.initialAssetId, prompt: props.initialPrompt })}</output><button onClick={() => { props.onCreated({ id: 'created' } as Task, 1); props.onOpenChange(false) }}>完成创建</button></> }))
describe('topic creation handoff', () => {
  it('prefills the existing form and stays on the created task after it closes', () => {
    render(<MemoryRouter initialEntries={['/tasks/new?project_id=p&asset_id=4b073aec-e9cf-4f18-b8b8-93b04d07aa93&prompt=topic']}><Routes><Route path="/tasks/new" element={<TaskCreatePage />} /><Route path="/tasks/created" element={<h1>新作品</h1>} /><Route path="/portraits/p" element={<h1>人设</h1>} /></Routes></MemoryRouter>)
    expect(JSON.parse(screen.getByTestId('defaults').textContent!)).toEqual({ project: 'p', asset: '4b073aec-e9cf-4f18-b8b8-93b04d07aa93', prompt: 'topic' })
    fireEvent.click(screen.getByRole('button', { name: '完成创建' }))
    expect(screen.getByRole('heading', { name: '新作品' })).toBeInTheDocument()
  })
})
