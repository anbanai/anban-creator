import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DashboardPage from './DashboardPage'
import { api } from '@/lib/api'
import { uploadToOSS } from '@/lib/direct-upload'
import { render } from '@/test/test-utils'

const navigateMock = vi.fn()

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
            error: null,
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
        list: vi.fn().mockResolvedValue([{
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
        }]),
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
  })

  it('renders a Codex-style AI entry and creates a task with uploaded attachments', async () => {
    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '今天想让 Anban 帮你创作什么？' })).toBeInTheDocument()
    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    expect(await screen.findByText('公众号项目')).toBeInTheDocument()
    expect(screen.queryByText('今日创作态势')).not.toBeInTheDocument()
    expect(screen.queryByText('接入状态')).not.toBeInTheDocument()
    expect(screen.queryByText('下一步')).not.toBeInTheDocument()
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
