import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectsPage from './ProjectsPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'

const { errorMock } = vi.hoisted(() => ({ errorMock: vi.fn() }))

vi.mock('sonner', () => ({ toast: { error: errorMock, success: vi.fn() } }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const { mockPlatformConfigs, mockProjectDetail, mockProjects } =
    await vi.importActual<typeof import('@/test/mocks/handlers')>('@/test/mocks/handlers')
  return {
    ...actual,
    api: {
      ...actual.api,
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue(mockProjects),
        stats: vi.fn().mockResolvedValue({ 'ch-1': mockProjectDetail.stats }),
        platformConfigs: vi.fn().mockResolvedValue(mockPlatformConfigs),
        delete: vi.fn(),
      },
      imageModels: {
        ...actual.api.imageModels,
        list: vi.fn().mockResolvedValue({ items: [], tier: 'pro' }),
      },
      video: {
        ...actual.api.video,
        models: vi.fn().mockResolvedValue({ items: [] }),
      },
    },
  }
})

describe('ProjectsPage deletion feedback', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'ch-1',
      user_id: '1',
      platform: 'article',
      name: '测试项目',
      avatar_url: '',
      profile_url: 'https://mp.weixin.qq.com/test',
      instructions: '测试定位',
      keywords: '测试',
      visual_style: '',
      writer: '',
      theme: '',
      author: '作者',
      template_id: '',
      reference_image_url: '',
      image_ratio: '16:9',
      max_concurrent_tasks: 2,
      config: { wechat_app_id: 'wx123' },
      status: 'active',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }])
    vi.mocked(api.projects.stats).mockResolvedValue({
      'ch-1': {
        total_tasks: 0,
        completed_tasks: 0,
        failed_tasks: 0,
        running_tasks: 0,
        pending_tasks: 0,
        success_rate: 0,
        last_activity_at: '',
      },
    })
    vi.mocked(api.projects.platformConfigs).mockResolvedValue([])
    vi.mocked(api.projects.delete).mockReset()
    window.history.pushState({}, '', '/projects')
  })

  it('shows the server archive guidance when deleting a project fails with associated work', async () => {
    vi.mocked(api.projects.delete).mockRejectedValueOnce({
      response: {
        data: {
          msg: 'cannot delete project with 13 associated tasks; archive it instead',
        },
      },
    })

    render(<ProjectsPage />)

    fireEvent.click(await screen.findByRole('button', { name: '删除项目' }))
    fireEvent.click(await screen.findByRole('button', { name: '删除' }))

    await waitFor(() => {
      expect(errorMock).toHaveBeenCalledWith('cannot delete project with 13 associated tasks; archive it instead')
    })
  })

  it('opens moments project creation without social-card preset jargon', async () => {
    window.history.pushState({}, '', '/projects?return_to=/tasks&create=true&type=moments&intent=new')
    render(<ProjectsPage />)

    expect(await screen.findByText('朋友圈')).toBeInTheDocument()
    const styleField = await screen.findByPlaceholderText(/描述图片视觉风格/)

    expect(screen.queryByRole('button', { name: /归藏社交卡/ })).not.toBeInTheDocument()
    expect(screen.queryByText(/Guizang|归藏|社交卡片/)).not.toBeInTheDocument()
    expect((styleField as HTMLTextAreaElement).value).toBe('')
  })
})
