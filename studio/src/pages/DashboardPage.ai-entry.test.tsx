import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DashboardPage from './DashboardPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'

const navigateMock = vi.fn()
const uploadToOSSMock = vi.hoisted(() => vi.fn())

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

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

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
    uploadToOSSMock.mockImplementation(async ({ file }: { file: File }) => ({
      uploadId: `upload-${file.name}`,
      key: `uploads/pending/user/${file.name}`,
      publicUrl: `https://cdn.example/${file.name}?signed=secret`,
      contentType: file.type,
      size: file.size,
    }))
  })

  it('renders the shared prompt composer', async () => {
    render(<DashboardPage />)
    await screen.findByRole('heading', { name: '首页' })
    expect(document.querySelector('[data-slot="agent-prompt-input"]')).toBeInTheDocument()
  })

  it('sends first-time users to project creation from the AI entry', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    const projectControl = await screen.findByRole('combobox', { name: '项目上下文' })
    expect(projectControl).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /创建第一个项目/ })).not.toBeInTheDocument()
    fireEvent.click(projectControl)
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

  it('renders a Codex-style AI entry and submits every shared attachment unchanged', async () => {
    const files = [
      new File(['image'], 'product.png', { type: 'image/png' }),
      new File(['audio'], 'voice.mp3', { type: 'audio/mpeg' }),
      new File(['video'], 'demo.mp4', { type: 'video/mp4' }),
      new File(['pdf'], 'brief.pdf', { type: 'application/pdf' }),
      new File(['notes'], 'notes.txt', { type: 'text/plain' }),
    ]

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '首页项目选择' })).not.toBeInTheDocument()
    expect(await screen.findByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目')
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
    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files } })
    await waitFor(() => expect(uploadToOSSMock).toHaveBeenCalledTimes(5))
    await screen.findByText('product.png')
    fireEvent.change(prompt, { target: { value: '帮我写一篇新品发布公众号文章' } })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith({
      channel: 'studio',
      project_id: 'project-1',
      text: '帮我写一篇新品发布公众号文章',
      execution_target: 'local',
      attachments: files.map((file, index) => ({
        type: (['image', 'audio', 'video', 'document', 'text'] as const)[index],
        upload_id: `upload-${file.name}`,
        key: `uploads/pending/user/${file.name}`,
        file_name: file.name,
        content_type: file.type,
        size: file.size,
      })),
    }))
    expect(navigateMock).toHaveBeenCalledWith('/tasks/task-ai-1')
  })

  it('disables submission while the shared reference input is uploading', async () => {
    let resolveUpload!: (value: unknown) => void
    uploadToOSSMock.mockImplementationOnce(() => new Promise((resolve) => { resolveUpload = resolve }))
    render(<DashboardPage />)

    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    fireEvent.change(prompt, { target: { value: '写一篇新品介绍' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送创建任务' })).toBeEnabled())

    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['image'], 'pending.png', { type: 'image/png' })] },
    })

    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
    await act(async () => {
      resolveUpload({ uploadId: 'pending', key: 'uploads/pending/pending.png', publicUrl: '', contentType: 'image/png', size: 5 })
      await Promise.resolve()
    })
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

    fireEvent.click(await screen.findByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))
    expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('种草项目')

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
    expect(await screen.findByRole('combobox', { name: '项目上下文' })).toHaveTextContent('电商项目')
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
