import { screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { TopicPoolDialog } from './TopicPoolDialog'
import { render } from '@/test/test-utils'
import { mockProjects } from '@/test/mocks/handlers'
import { api } from '@/lib/api'

// Capture toast calls so we can assert the silent-failure feedback.
const { errorMock, successMock } = vi.hoisted(() => ({
  errorMock: vi.fn(),
  successMock: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: { error: errorMock, success: successMock },
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      topicPool: {
        ...actual.api.topicPool,
        list: vi.fn().mockResolvedValue({ items: [], total: 0 }),
        create: vi.fn(),
        delete: vi.fn(),
        reset: vi.fn(),
      },
    },
  }
})

describe('TopicPoolDialog — mutation feedback (no silent failure)', () => {
  beforeEach(() => {
    errorMock.mockClear()
    successMock.mockClear()
  })

  it('surfaces a toast with the server message when adding topics fails', async () => {
    // Server rejects with a { msg } body — the common real-world failure shape.
    vi.mocked(api.topicPool.create).mockRejectedValueOnce({
      response: { data: { msg: '选题不能为空' } },
    })

    render(
      <TopicPoolDialog project={mockProjects[0]} open onOpenChange={vi.fn()} />,
    )

    const input = await screen.findByPlaceholderText('输入选题，每行一个')
    fireEvent.change(input, { target: { value: '新选题A' } })
    fireEvent.click(screen.getByRole('button', { name: '添加选题' }))

    // Regression: previously this rejected silently — no toast at all.
    await waitFor(() => {
      expect(errorMock).toHaveBeenCalledTimes(1)
    })
    expect(errorMock).toHaveBeenCalledWith('选题不能为空')
  })

  it('confirms with a count when topics are added successfully', async () => {
    vi.mocked(api.topicPool.create).mockResolvedValueOnce({} as never)

    render(
      <TopicPoolDialog project={mockProjects[0]} open onOpenChange={vi.fn()} />,
    )

    const input = await screen.findByPlaceholderText('输入选题，每行一个')
    fireEvent.change(input, { target: { value: '选题一\n选题二' } })
    fireEvent.click(screen.getByRole('button', { name: '添加选题' }))

    await waitFor(() => {
      expect(successMock).toHaveBeenCalledWith('已添加 2 条选题')
    })
  })
})
