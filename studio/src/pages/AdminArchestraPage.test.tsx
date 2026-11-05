import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminArchestraPage from './AdminArchestraPage'

const api = vi.hoisted(() => ({
  agents: vi.fn(),
  sendMessage: vi.fn(),
  task: vi.fn(),
  approval: vi.fn(),
  cancel: vi.fn(),
}))

vi.mock('@/lib/api/archestra', () => ({ archestraApi: api }))

function renderPage() {
  return render(<MemoryRouter><AdminArchestraPage /></MemoryRouter>)
}

describe('AdminArchestraPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.agents.mockResolvedValue([{ id: 'agent-1', name: 'Writer', description: 'writer' }])
  })

  it('loads agents and sends a first message', async () => {
    api.sendMessage.mockResolvedValue({ message: { role: 'ROLE_AGENT', context_id: 'ctx-1', text: 'hello' } })
    renderPage()
    await screen.findByRole('option', { name: 'Writer' })
    fireEvent.change(screen.getByRole('textbox', { name: '消息' }), { target: { value: 'hi' } })
    fireEvent.click(screen.getByRole('button', { name: '发送' }))
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledWith('agent-1', { text: 'hi' }))
    expect(await screen.findByText('hello')).toBeInTheDocument()
  })

  it('polls input-required tasks and submits an approval decision', async () => {
    api.sendMessage.mockResolvedValue({ task: { task_id: 'task-1', context_id: 'ctx-1', state: 'TASK_STATE_WORKING' } })
    api.task.mockResolvedValue({ task_id: 'task-1', context_id: 'ctx-1', state: 'TASK_STATE_INPUT_REQUIRED', approval_requests: [{ approval_id: 'approval-1', text: 'Approve?', resolved: false }] })
    api.approval.mockResolvedValue({ task: { task_id: 'task-1', context_id: 'ctx-1', state: 'TASK_STATE_COMPLETED', response: 'approved result' } })
    renderPage()
    await screen.findByRole('option', { name: 'Writer' })
    fireEvent.change(screen.getByRole('textbox', { name: '消息' }), { target: { value: 'run' } })
    fireEvent.click(screen.getByRole('button', { name: '发送' }))
    expect(await screen.findByText('Approve?')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '批准' }))
    await waitFor(() => expect(api.approval).toHaveBeenCalledWith('task-1', { agent_id: 'agent-1', context_id: 'ctx-1', approval_id: 'approval-1', approved: true }))
    expect(await screen.findByText('approved result')).toBeInTheDocument()
  })

  it('clears the in-memory conversation', async () => {
    api.sendMessage.mockResolvedValue({ message: { role: 'ROLE_AGENT', text: 'hello' } })
    renderPage()
    await screen.findByRole('option', { name: 'Writer' })
    fireEvent.change(screen.getByRole('textbox', { name: '消息' }), { target: { value: 'hi' } })
    fireEvent.click(screen.getByRole('button', { name: '发送' }))
    expect(await screen.findByText('hello')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '清空' }))
    expect(screen.queryByText('hello')).not.toBeInTheDocument()
  })
})
