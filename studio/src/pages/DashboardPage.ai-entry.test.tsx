import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DashboardPage from './DashboardPage'
import { api } from '@/lib/api'
import { uploadToOSS } from '@/lib/direct-upload'
import { render } from '@/test/test-utils'

const navigateMock = vi.fn()

const { articleProject, seednoteProject, ecommerceProject } = vi.hoisted(() => {
  const articleProject = {
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
    image_ratio: '',
    max_concurrent_tasks: 1,
    config: { enable_publishing: true },
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
  } as const

  return {
    articleProject,
    seednoteProject: {
      ...articleProject,
      id: 'project-2',
      platform: 'seednote',
      name: '种草项目',
      config: {},
    } as const,
    ecommerceProject: {
      ...articleProject,
      id: 'project-3',
      platform: 'ecommerce',
      name: '电商项目',
      config: {},
    } as const,
  }
})

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => navigateMock,
  }
})

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({
    user: {
      nickname: '测试用户',
      invite_code: 'INVITE',
      invite_count: 0,
      max_invites: 3,
    },
  }),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), message: vi.fn(), success: vi.fn() } }))

vi.mock('@/lib/direct-upload', () => ({
  uploadToOSS: vi.fn().mockResolvedValue({
    uploadId: 'upload-1',
    key: 'uploads/pending/user/upload-1/ref.png',
    publicUrl: 'https://cdn.example.com/uploads/pending/user/upload-1/ref.png',
    contentType: 'image/png',
    size: 123,
  }),
}))

vi.mock('@/lib/tauri', () => ({
  isDesktop: () => true,
  getLocalExecutorStatus: vi.fn().mockResolvedValue({
    state: 'running_idle',
    available: true,
    running: true,
    reason: '',
  }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      aiEntry: {
        submit: vi.fn().mockResolvedValue({
          status: 'created',
          message: '已创建任务',
          task: {
            id: 'task-ai-1',
            type: 'article',
            prompt: '帮我写一篇新品发布公众号文章',
            status: 'pending',
            progress: 0,
            plan_id: null,
            project_id: 'project-1',
            result: { files: null, output: '' },
            published: false,
            published_at: null,
            created_at: '2026-07-07T00:00:00.000Z',
            started_at: '',
            completed_at: '',
          },
        }),
      },
      credits: {
        ...actual.api.credits,
        balance: vi.fn().mockResolvedValue({ balance: 800 }),
        signInStatus: vi.fn().mockResolvedValue({ signed_in_today: false }),
        pricing: vi.fn().mockResolvedValue({
          task_costs: {},
          model_costs: {},
          recharge_tiers: [],
          income: { daily_sign_in: 100, register_bonus: 1000, invite_reward: 1000 },
        }),
      },
      plans: {
        ...actual.api.plans,
        list: vi.fn().mockResolvedValue({ items: [], total: 0 }),
      },
      tasks: {
        ...actual.api.tasks,
        list: vi.fn().mockResolvedValue({ items: [], total: 0 }),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue([{ ...articleProject }]),
      },
      apiKeys: {
        ...actual.api.apiKeys,
        list: vi.fn().mockResolvedValue({ items: [{ id: 'key-1' }] }),
      },
      modelConfig: {
        ...actual.api.modelConfig,
        get: vi.fn().mockResolvedValue({ text: { model: 'gpt-5' }, image: null }),
      },
    },
  }
})

describe('DashboardPage AI entry', () => {
  beforeEach(() => {
    navigateMock.mockClear()
    vi.mocked(api.aiEntry.submit).mockClear()
    vi.mocked(uploadToOSS).mockClear()
  })

  it('sends first-time users to project creation from the AI entry', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '选择项目' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /创建第一个项目/ })).not.toBeInTheDocument()
    expect(await screen.findByRole('link', { name: '创建项目' })).toHaveAttribute(
      'href',
      '/projects?return_to=%2Ftasks&create=true&type=seednote&intent=new',
    )
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
  })

  it('blocks creation when model configuration is not ready', async () => {
    vi.mocked(api.modelConfig.get).mockResolvedValueOnce({})

    render(<DashboardPage />)

    expect(await screen.findByText('模型配置未就绪，先选择可用模型。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '去设置' })).toHaveAttribute('href', '/settings#model-key-settings')
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
  })

  it('renders a Codex-style AI entry and creates a task with uploaded attachments', async () => {
    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '首页项目选择' })).not.toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '选择项目 公众号项目' })).toHaveAttribute('aria-expanded', 'false')
    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    expect(screen.getByText('公众号文章')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /新建创作任务/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /安排自动计划/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /管理项目配置/ })).not.toBeInTheDocument()
    expect(screen.queryByText('今日创作态势')).not.toBeInTheDocument()
    expect(screen.queryByText('接入状态')).not.toBeInTheDocument()
    expect(screen.queryByText('下一步')).not.toBeInTheDocument()
    expect(screen.queryByText('最近任务')).not.toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('还没有任务')).not.toBeInTheDocument())

    fireEvent.change(prompt, { target: { value: '帮我写一篇新品发布公众号文章' } })
    const fileInput = screen.getByLabelText('上传参考素材')
    const file = new File(['img'], 'ref.png', { type: 'image/png' })
    fireEvent.change(fileInput, { target: { files: [file] } })

    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'ai_entry_attachment',
      file,
    })))

    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith({
      channel: 'studio',
      project_id: 'project-1',
      text: '帮我写一篇新品发布公众号文章',
      execution_target: 'local',
      attachments: [expect.objectContaining({
        type: 'image',
        url: 'https://cdn.example.com/uploads/pending/user/upload-1/ref.png',
        file_name: 'ref.png',
      })],
    }))
    expect(navigateMock).toHaveBeenCalledWith('/tasks/task-ai-1')
  })

  it('uses the selected project from the composer menu when multiple projects exist', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...articleProject },
      { ...seednoteProject },
    ])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '首页项目选择' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /新建创作任务/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /安排自动计划/ })).not.toBeInTheDocument()

    fireEvent.click(await screen.findByRole('button', { name: '选择项目 公众号项目' }))
    expect(await screen.findByRole('textbox', { name: '搜索项目' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /种草项目/ }))
    expect(screen.getByRole('button', { name: '选择项目 种草项目' })).toHaveAttribute('aria-expanded', 'false')

    fireEvent.change(await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '写一篇小红书种草笔记' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith(expect.objectContaining({
      project_id: 'project-2',
      text: '写一篇小红书种草笔记',
    })))
  })

  it('does not render legacy shortcut cards for project types that cannot create plans', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...ecommerceProject },
    ])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '选择项目 电商项目' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /新建创作任务/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /安排自动计划/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /管理项目配置/ })).not.toBeInTheDocument()
  })

  it('shows needs-configuration errors without navigating', async () => {
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({
      status: 'needs_configuration',
      message: '请先上传产品图',
      action_url: '/tasks?create=true&type=ecommerce&project_id=project-1&intent=new',
    })

    render(<DashboardPage />)
    fireEvent.change(await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '做一套保温杯主图' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    expect(await screen.findByText('请先上传产品图')).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: '补充配置' })).toHaveAttribute('href', '/tasks?create=true&type=ecommerce&project_id=project-1&intent=new')
    expect(navigateMock).not.toHaveBeenCalledWith('/tasks/task-ai-1')
  })
})
