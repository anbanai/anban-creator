import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectsPage from './ProjectsPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import type { Project } from '@/types'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const { errorMock, authState } = vi.hoisted(() => ({ errorMock: vi.fn(), authState: { isAdmin: true } }))
const uploadToOSSMock = vi.hoisted(() => vi.fn())

const projectWithReference: Project = {
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

  reference_image: {
    asset_id: '44444444-4444-4444-8444-444444444444',
    file_name: 'project-reference.png',
    content_type: 'image/png',
    size: 9,
    download_url: 'https://signed.example/project-reference.png',
    download_expires_at: '2026-07-20T10:00:00Z',
  },
  image_ratio: '16:9',
  max_concurrent_tasks: 2,
  config: { wechat_app_id: 'wx123' },
  status: 'active',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

async function clickProjectAction(projectName: string, actionName: string) {
  fireEvent.click(await screen.findByRole('button', { name: `${actionName}：${projectName}` }))
}

vi.mock('sonner', () => ({ toast: { error: errorMock, success: vi.fn() } }))

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { is_admin: authState.isAdmin } }),
}))

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

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
      },
      imageCapabilities: {
        ...actual.api.imageCapabilities,
        list: vi.fn().mockResolvedValue({ items: [], tier: 'pro' }),
      },
		agentPacks: {
			...actual.api.agentPacks,
			list: vi.fn().mockResolvedValue({ packs: [] }),
		},
    },
  }
})

describe('ProjectsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authState.isAdmin = true
    vi.mocked(api.projects.list).mockResolvedValue([projectWithReference])
    vi.mocked(api.projects.stats).mockResolvedValue({
      'ch-1': {
        total_tasks: 0,
        completed_tasks: 0,
        failed_tasks: 0,
        running_tasks: 0,
        pending_tasks: 0,
        unused_topics: 0,
        success_rate: 0,
        last_activity_at: '',
      },
    })
    vi.mocked(api.projects.platformConfigs).mockResolvedValue([])
    vi.mocked(api.projects.create).mockReset()
    vi.mocked(api.projects.update).mockReset()
		vi.mocked(api.agentPacks.list).mockReset().mockResolvedValue({ packs: [] })
    uploadToOSSMock.mockResolvedValue({
      uploadSessionId: '11111111-1111-4111-8111-111111111111',
      uploadId: 'upload-project-reference',
      key: 'uploads/pending/project-reference.png',
      publicUrl: 'https://staging.example/project-reference.png',
      previewUrl: 'https://staging.example/project-reference.png',
      contentType: 'image/png',
      size: 9,
    })
    window.history.pushState({}, '', '/projects')
  })

  it('keeps Montage visible while hiding internal project platforms from ordinary users', async () => {
    authState.isAdmin = false
    vi.mocked(api.projects.list).mockResolvedValue([
      projectWithReference,
      { ...projectWithReference, id: 'moments-1', name: '内部朋友圈', platform: 'moments' },
      { ...projectWithReference, id: 'ecommerce-1', name: '内部电商', platform: 'ecommerce' },
      { ...projectWithReference, id: 'montage-1', name: '内部剪辑', platform: 'montage' },
    ])

    render(<ProjectsPage />)

    expect(await screen.findByText('测试项目')).toBeInTheDocument()
    expect(screen.queryByText('内部朋友圈')).not.toBeInTheDocument()
    expect(screen.queryByText('内部电商')).not.toBeInTheDocument()
    expect(screen.getByText('内部剪辑')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建项目' }))
    const dialog = await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.click(within(dialog).getAllByRole('combobox')[0])
    expect(await screen.findByRole('option', { name: '公众号' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '种草笔记' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '朋友圈' })).not.toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '电商出图' })).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Montage' })).toBeInTheDocument()
  })

  it('opens Montage project creation from a deep link for ordinary users', async () => {
    authState.isAdmin = false
    window.history.pushState({}, '', '/projects?create=true&type=montage&intent=new')

    render(<ProjectsPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建项目' })
    expect(within(dialog).getAllByRole('combobox')[0]).toHaveTextContent('Montage')
    expect(within(dialog).getByText('Montage 默认配置')).toBeInTheDocument()
  })

  it('does not ask seednote projects for a publishing author', async () => {
    window.history.pushState({}, '', '/projects?create=true&type=seednote&intent=new')

    render(<ProjectsPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建项目' })
    expect(within(dialog).queryByLabelText('作者名')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('作者名')).not.toBeInTheDocument()
  })

  it('constrains project ratios with the selected business platform', () => {
    const source = readFileSync(resolve(import.meta.dirname, 'ProjectsPage.tsx'), 'utf8')
    expect(source).toContain('ratios={currentPlatformConfig?.supported_image_ratios ?? []}')
    expect(source).not.toContain('generation_features')
    expect(source).not.toContain('generationFeatures')
    expect(source).not.toContain('sizePresets')
    expect(source).not.toContain('imageRatioUnsupported')
    expect(source).toContain('该图像能力已停用，请重新选择')
    expect(source).toContain('submittedImageCapability.price_available !== true')
    expect(source).toContain('submittedImageCapability.enabled !== true')
  })

	it('keeps the project composer usable when the optional Agent Pack catalog is partial', async () => {
		vi.mocked(api.agentPacks.list).mockResolvedValueOnce({} as Awaited<ReturnType<typeof api.agentPacks.list>>)
		window.history.pushState({}, '', '/projects?create=true&type=article&intent=new')

		render(<ProjectsPage />)

		expect(await screen.findByRole('dialog', { name: '新建项目' })).toBeInTheDocument()
	})

  it('shows the catalog default capability for a new ecommerce project', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValueOnce({
      tier: 'pro',
      default_capability: 'catalog-default',
      items: [
        { key: 'first-sorted', display_name: '首个排序能力', min_tier: 'free', sort_order: 1, enabled: true, price_available: true },
        { key: 'catalog-default', display_name: '目录默认能力', min_tier: 'free', sort_order: 2, enabled: true, price_available: true },
      ],
    })
    window.history.pushState({}, '', '/projects?create=true&type=ecommerce&intent=new')

    render(<ProjectsPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建项目' })
    await waitFor(() => {
      const selector = Array.from(dialog.querySelectorAll('[role="combobox"]')).find((element) =>
        element.textContent?.includes('目录默认能力'),
      )
      expect(selector).toBeDefined()
    })
  })

  it('creates a project with a reference session and no legacy URL field', async () => {
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project: {
      ...(await vi.mocked(api.projects.list)())[0],
      id: 'created-project',
      platform: 'seednote',
    } })
    window.history.pushState({}, '', '/projects?create=true&type=seednote&intent=new')

    render(<ProjectsPage />)

    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['reference'], 'reference.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.projects.create).toHaveBeenCalled())
    const payload = vi.mocked(api.projects.create).mock.calls[0][0]
    expect(payload.reference_image).toEqual({
      upload_session_id: '11111111-1111-4111-8111-111111111111',
    })
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('keeps the uploaded selection open when server finalization fails', async () => {
    vi.mocked(api.projects.create).mockRejectedValueOnce({
      response: { data: { msg: '上传会话已过期，请重新上传参考图' } },
    })
    window.history.pushState({}, '', '/projects?create=true&type=seednote&intent=new')

    render(<ProjectsPage />)

    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['reference'], 'reference.png', { type: 'image/png' })] },
    })
    const preview = await screen.findByRole('img', { name: '参考图' })
    await waitFor(() => expect(screen.getByRole('button', { name: '创建' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(errorMock).toHaveBeenCalledWith('上传会话已过期，请重新上传参考图'))
    expect(screen.getByRole('dialog', { name: '新建项目' })).toBeInTheDocument()
    expect(preview).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建' })).toBeEnabled()
  })

  it('omits an untouched existing project asset from an edit payload', async () => {
    vi.mocked(api.projects.update).mockResolvedValueOnce(projectWithReference)
    render(<ProjectsPage />)

    await clickProjectAction('测试项目', '编辑项目')
    expect(await screen.findByRole('img', { name: '参考图' })).toHaveAttribute(
      'src',
      'https://signed.example/project-reference.png',
    )
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.projects.update).toHaveBeenCalled())
    const payload = vi.mocked(api.projects.update).mock.calls[0][1]
    expect(payload).not.toHaveProperty('reference_image')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('sends null when an existing project asset is explicitly cleared', async () => {
    vi.mocked(api.projects.update).mockResolvedValueOnce(projectWithReference)
    render(<ProjectsPage />)

    await clickProjectAction('测试项目', '编辑项目')
    fireEvent.click(await screen.findByRole('button', { name: '移除参考图' }))
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.projects.update).toHaveBeenCalled())
    const payload = vi.mocked(api.projects.update).mock.calls[0][1]
    expect(payload.reference_image).toBeNull()
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('replaces an existing project asset with an upload session selection', async () => {
    vi.mocked(api.projects.update).mockResolvedValueOnce(projectWithReference)
    render(<ProjectsPage />)

    await clickProjectAction('测试项目', '编辑项目')
    fireEvent.click(await screen.findByRole('button', { name: '移除参考图' }))
    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['replacement'], 'replacement.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '更新' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.projects.update).toHaveBeenCalled())
    const payload = vi.mocked(api.projects.update).mock.calls[0][1]
    expect(payload.reference_image).toEqual({
      upload_session_id: '11111111-1111-4111-8111-111111111111',
    })
    expect(payload).not.toHaveProperty('reference_image_url')
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

      reference_image: null,
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

      reference_image: null,
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
    await clickProjectAction('Saved montage', '编辑项目')
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
