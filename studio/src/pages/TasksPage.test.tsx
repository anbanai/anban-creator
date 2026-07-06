import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import TasksPage from './TasksPage'
import { api } from '@/lib/api'
import { createTestQueryClient } from '@/test/test-utils'
import type { Project, Task } from '@/types'

vi.mock('sonner', () => ({ toast: { error: vi.fn(), message: vi.fn(), success: vi.fn() } }))

const fixtures = vi.hoisted(() => {
  const project = {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'article',
    name: '公众号项目',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    reference_image_url: '',
    image_ratio: '16:9',
    max_concurrent_tasks: 1,
    config: {},
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
  }
  const failedTask = {
    id: 'failed-task',
    type: 'article',
    title: '失败文章',
    prompt: '失败任务',
    status: 'failed',
    progress: 0,
    error: '模型超时',
    plan_id: null,
    project_id: project.id,
    result: { files: null, output: '' },
    published: false,
    published_at: null,
    created_at: '2026-07-06T01:00:00.000Z',
    started_at: '',
    completed_at: '',
  }
  return { project, failedTask }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        list: vi.fn().mockResolvedValue({ items: [fixtures.failedTask], total: 1 }),
        create: vi.fn(),
        bulkCancel: vi.fn(),
        bulkClone: vi.fn(),
        bulkDelete: vi.fn(),
        downloadBulkZipBlob: vi.fn(),
        markPublished: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue([fixtures.project]),
      },
      credits: {
        ...actual.api.credits,
        balance: vi.fn().mockResolvedValue({ balance: 1000 }),
        pricing: vi.fn().mockResolvedValue({
          task_costs: {},
          model_costs: {},
          ecommerce_module_prices: {},
          income: { daily_sign_in: 1024, register_bonus: 4096, invite_reward: 2048 },
        }),
      },
      video: {
        ...actual.api.video,
        estimate: vi.fn(),
      },
    },
  }
})

function renderTasksPage(initialPath = '/tasks') {
  const queryClient = createTestQueryClient()
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[initialPath]}>
          <Routes>
            <Route path="/tasks" element={<TasksPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  }
}

describe('TasksPage URL-driven recovery filters', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.list).mockResolvedValue({ items: [fixtures.failedTask as Task], total: 1 })
    vi.mocked(api.projects.list).mockResolvedValue([fixtures.project as Project])
  })

  it('syncs recovery queue links into the task status filter', async () => {
    renderTasksPage()

    await waitFor(() => {
      expect(api.tasks.list).toHaveBeenCalledWith(expect.objectContaining({ status: undefined }))
    })

    fireEvent.click(await screen.findByRole('link', { name: /失败待恢复/ }))

    await waitFor(() => {
      expect(api.tasks.list).toHaveBeenCalledWith(expect.objectContaining({ status: 'failed' }))
    })
  })
})
