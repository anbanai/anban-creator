import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import TasksPage from './TasksPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

const mockNavigate = vi.fn()

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/lib/api', async () => {
  return {
    api: {
      channels: {
        list: vi.fn().mockResolvedValue([
          {
            id: 'ch-1',
            user_id: '1',
            platform: 'seednote',
            name: '种草账号',
            avatar_url: '',
            profile_url: '',
            positioning: '',
            keywords: '',
            style: '',
            theme: '',
            author: '',
            reference_image_url: '',
            image_ratio: '3:4',
            layout: '',
            image_preset: '',
            max_concurrent_tasks: 2,
            config: {},
            status: 'active',
            created_at: '2025-01-01T00:00:00Z',
            updated_at: '2025-01-01T00:00:00Z',
          },
        ]),
      },
      credits: {
        balance: vi.fn().mockResolvedValue({ balance: 10000 }),
        pricing: vi.fn().mockResolvedValue({
          task_costs: { article: 4000, seednote: 3200, viral_analysis: 800 },
          model_costs: {},
          income: { daily_sign_in: 1024, register_bonus: 4096, invite_reward: 2048 },
        }),
      },
      tasks: {
        list: vi.fn().mockResolvedValue({ items: [], total: 0 }),
        create: vi.fn().mockResolvedValue({ id: 'task-1' }),
        markPublished: vi.fn(),
        downloadBulkZipBlob: vi.fn(),
      },
      viralAnalyses: {
        create: vi.fn().mockResolvedValue({ id: 'analysis-1' }),
      },
    },
  }
})

describe('TasksPage viral analysis creation', () => {
  it('creates viral analysis through viralAnalyses API instead of tasks API', async () => {
    render(<TasksPage />)

    fireEvent.click(await screen.findByRole('button', { name: '新建任务' }))
    fireEvent.click(screen.getByRole('button', { name: /爆文拆解/ }))
    fireEvent.change(screen.getByPlaceholderText(/粘贴笔记链接或分享文本/), {
      target: { value: '帮我拆解 https://www.xiaohongshu.com/explore/mock' },
    })
    fireEvent.submit(document.querySelector('#task-create-form') as HTMLFormElement)

    await waitFor(() => {
      expect(api.viralAnalyses.create).toHaveBeenCalledWith({
        source_type: 'note',
        source_url: 'https://www.xiaohongshu.com/explore/mock',
      })
    })
    expect(api.tasks.create).not.toHaveBeenCalled()
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/workshop?tab=analysis&analysisId=analysis-1')
    })
  })
})
