import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import TaskFeedbackCard from './TaskFeedbackCard'
import { createTestQueryClient } from '@/test/test-utils'
import { api } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      feedback: {
        ...actual.api.feedback,
        getTask: vi.fn(),
        saveTask: vi.fn(),
      },
    },
  }
})

function renderCard() {
  const queryClient = createTestQueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskFeedbackCard taskId="task-1" />
    </QueryClientProvider>,
  )
}

describe('TaskFeedbackCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.feedback.getTask).mockResolvedValue(null)
    vi.mocked(api.feedback.saveTask).mockResolvedValue({
      id: 'feedback-1', task_id: 'task-1', user_id: 'user-1', rating: 4, content: '不错',
      created_at: '', updated_at: '',
    })
  })

  it('submits a selected star rating and optional comment', async () => {
    renderCard()

    fireEvent.click(await screen.findByRole('radio', { name: '4 星' }))
    fireEvent.change(screen.getByRole('textbox', { name: '评价意见' }), { target: { value: '不错' } })
    fireEvent.click(screen.getByRole('button', { name: '提交评价' }))

    await waitFor(() => expect(api.feedback.saveTask).toHaveBeenCalledWith('task-1', { rating: 4, content: '不错' }))
    expect(await screen.findByText('已评价，可随时修改')).toBeInTheDocument()
  })

  it('loads an existing evaluation and allows updating it', async () => {
    vi.mocked(api.feedback.getTask).mockResolvedValue({
      id: 'feedback-1', task_id: 'task-1', user_id: 'user-1', rating: 2, content: '需改进',
      created_at: '', updated_at: '',
    })
    renderCard()

    expect(await screen.findByDisplayValue('需改进')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio', { name: '5 星' }))
    fireEvent.click(screen.getByRole('button', { name: '更新评价' }))
    await waitFor(() => expect(api.feedback.saveTask).toHaveBeenCalledWith('task-1', { rating: 5, content: '需改进' }))
  })
})
