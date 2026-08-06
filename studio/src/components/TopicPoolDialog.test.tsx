import { screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { TopicPoolDialog } from './TopicPoolDialog'
import { render } from '@/test/test-utils'
import { mockProjects } from '@/test/mocks/handlers'
import { api } from '@/lib/api'
import { QueryClient } from '@tanstack/react-query'

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
    vi.restoreAllMocks()
    errorMock.mockClear()
    successMock.mockClear()
    vi.mocked(api.topicPool.list).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.topicPool.create).mockReset()
    vi.mocked(api.topicPool.delete).mockReset()
    vi.mocked(api.topicPool.reset).mockReset()
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
    const invalidateQueries = vi.spyOn(QueryClient.prototype, 'invalidateQueries')
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
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['project-stats'] })
  })

  it('refreshes project stats when an unused topic is deleted', async () => {
    const invalidateQueries = vi.spyOn(QueryClient.prototype, 'invalidateQueries')
    vi.mocked(api.topicPool.list).mockResolvedValueOnce({
      items: [{ id: 11, user_id: '1', project_id: mockProjects[0].id, topic: '待删除', status: 'unused', created_at: '', updated_at: '' }],
      total: 1,
    })
    vi.mocked(api.topicPool.delete).mockResolvedValueOnce(undefined)

    render(<TopicPoolDialog project={mockProjects[0]} open onOpenChange={vi.fn()} />)

    fireEvent.click(await screen.findByRole('button', { name: '删除' }))

    await waitFor(() => {
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['project-stats'] })
    })
  })

  it('refreshes project stats when a used topic is reset', async () => {
    const invalidateQueries = vi.spyOn(QueryClient.prototype, 'invalidateQueries')
    vi.mocked(api.topicPool.list).mockResolvedValueOnce({
      items: [{ id: 12, user_id: '1', project_id: mockProjects[0].id, topic: '待重置', status: 'used', created_at: '', updated_at: '' }],
      total: 1,
    })
    vi.mocked(api.topicPool.reset).mockResolvedValueOnce(undefined)

    render(<TopicPoolDialog project={mockProjects[0]} open onOpenChange={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: '重置' }))

    await waitFor(() => {
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['project-stats'] })
    })
  })
})
