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
        create: vi.fn(),
        update: vi.fn(),
        delete: vi.fn(),
      },
      imageModels: {
        ...actual.api.imageModels,
        list: vi.fn().mockResolvedValue({ items: [], tier: 'pro' }),
      },
      videoCreator: {
        ...actual.api.videoCreator,
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
    vi.mocked(api.projects.create).mockReset()
    vi.mocked(api.projects.update).mockReset()
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

    await screen.findByText('测试项目', {}, { timeout: 5000 })
    fireEvent.click(await screen.findByRole('button', { name: '删除项目' }, { timeout: 5000 }))
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

  it('creates a Montage project with every project default', async () => {
    window.history.pushState({}, '', '/projects?return_to=/tasks&create=true&type=montage&intent=new')
    vi.mocked(api.projects.create).mockResolvedValue({ project: {
      id: 'montage-created',
      user_id: '1',
      platform: 'montage',
      name: 'Launch montage',
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
      montage_defaults: {},
      max_concurrent_tasks: 1,
      config: {},
      status: 'active',
      created_at: '2026-07-17T00:00:00Z',
      updated_at: '2026-07-17T00:00:00Z',
    } })

    render(<ProjectsPage />)

    expect(await screen.findByText('Montage 默认配置')).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: 'Launch montage' } })
    fireEvent.change(screen.getByLabelText('默认 Pipeline'), { target: { value: 'social-short' } })
    fireEvent.change(screen.getByLabelText('默认时长（秒）'), { target: { value: '45' } })
    fireEvent.change(screen.getByLabelText('音乐提示'), { target: { value: 'minimal electronic' } })
    fireEvent.change(screen.getByLabelText('字幕模式'), { target: { value: 'burned-in' } })
    fireEvent.change(screen.getByLabelText('配音模式'), { target: { value: 'narrated' } })
    fireEvent.change(screen.getByLabelText('素材使用说明'), { target: { value: '优先使用实拍素材' } })
    const deliveryInput = screen.getByPlaceholderText('输入交付目标后按回车')
    fireEvent.change(deliveryInput, { target: { value: 'final_video' } })
    fireEvent.keyDown(deliveryInput, { key: 'Enter' })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.projects.create).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'montage',
      name: 'Launch montage',
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: expect.objectContaining({
          aspect_ratio: '9:16',
          duration_seconds: 45,
          music_prompt: 'minimal electronic',
          subtitle_mode: 'burned-in',
          voiceover_mode: 'narrated',
        }),
        asset_guidance: '优先使用实拍素材',
        delivery_targets: ['final_video'],
      },
    })))
  })

  it('restores and updates saved Montage project defaults', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'montage-1',
      user_id: '1',
      platform: 'montage',
      name: 'Saved montage',
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
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: { aspect_ratio: '16:9', duration_seconds: 60, music_prompt: 'cinematic' },
        asset_guidance: '保留品牌标志',
        delivery_targets: ['final_video', 'subtitles'],
      },
      max_concurrent_tasks: 1,
      config: {},
      status: 'active',
      created_at: '2026-07-17T00:00:00Z',
      updated_at: '2026-07-17T00:00:00Z',
    }])
    vi.mocked(api.projects.update).mockResolvedValue({} as never)

    render(<ProjectsPage />)

    await screen.findByText('Saved montage')
    fireEvent.click(screen.getByRole('button', { name: '编辑项目' }))
    expect(await screen.findByLabelText('默认 Pipeline')).toHaveValue('social-short')
    expect(screen.getByLabelText('默认画幅')).toHaveValue('16:9')
    expect(screen.getByLabelText('默认时长（秒）')).toHaveValue(60)
    expect(screen.getByLabelText('音乐提示')).toHaveValue('cinematic')
    expect(screen.getByText('final_video')).toBeInTheDocument()
    expect(screen.getByText('subtitles')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('默认时长（秒）'), { target: { value: '45' } })
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.projects.update).toHaveBeenCalledWith('montage-1', expect.objectContaining({
      platform: 'montage',
      montage_defaults: expect.objectContaining({
        default_pipeline: 'social-short',
        preferences: expect.objectContaining({ duration_seconds: 45, music_prompt: 'cinematic' }),
        asset_guidance: '保留品牌标志',
        delivery_targets: ['final_video', 'subtitles'],
      }),
    })))
  })
})
