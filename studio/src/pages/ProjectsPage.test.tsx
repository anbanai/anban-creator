import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectsPage from './ProjectsPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import { mockPlatformConfigs } from '@/test/mocks/handlers'
import type { Project } from '@/types'

const { errorMock, successMock, authState } = vi.hoisted(() => ({
  errorMock: vi.fn(),
  successMock: vi.fn(),
  authState: { isAdmin: true },
}))
const uploadToOSSMock = vi.hoisted(() => vi.fn())

const projectWithReference: Project = {
  id: 'ch-1',
  user_id: '1',
  platform: 'wechat',
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
  status: 'active',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

async function clickProjectAction(projectName: string, actionName: string) {
  fireEvent.click(await screen.findByRole('button', { name: `${actionName}：${projectName}` }))
}

vi.mock('sonner', () => ({ toast: { error: errorMock, success: successMock } }))

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
        getChannelConfig: vi.fn().mockResolvedValue({ id: "channel-1", project_id: "ch-1", channel: "wechat-article", config: { wechat_app_id: "wx-test" } }),
        upsertChannelConfig: vi.fn().mockResolvedValue({}),
        get: vi.fn(),
        update: vi.fn(),
      },
      imageCapabilities: {
        ...actual.api.imageCapabilities,
        list: vi.fn().mockResolvedValue({ items: [], tier: 'pro' }),
      },
		hypitCapabilities: { list: vi.fn().mockResolvedValue({ enabled: true, configured: true, missing_configuration: [], limits: { max_assets: 20, max_duration_seconds: 180, max_asset_bytes: 100000, max_input_bytes: 200000 } }) },
      montageCapabilities: {
			...actual.api.montageCapabilities,
			list: vi.fn(),
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
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
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
    vi.mocked(api.projects.platformConfigs).mockResolvedValue(mockPlatformConfigs)
    vi.mocked(api.projects.create).mockReset()
    vi.mocked(api.projects.get).mockReset()
    vi.mocked(api.projects.getChannelConfig).mockReset().mockResolvedValue({ id: 'channel-1', project_id: 'ch-1', channel: 'wechat-article', config: { wechat_app_id: 'wx-test' } })
    vi.mocked(api.projects.upsertChannelConfig).mockReset().mockResolvedValue({ id: 'channel-1', project_id: 'ch-1', channel: 'wechat-article', config: {} })
    vi.mocked(api.projects.update).mockReset()
		vi.mocked(api.agentPacks.list).mockReset().mockResolvedValue({ packs: [] })
    vi.mocked(api.montageCapabilities.list).mockReset().mockResolvedValue({
      enabled: true,
      default_pipeline: 'cinematic',
      max_duration_seconds: 600,
      max_assets: 20,
      items: [
        {
          key: 'cinematic', display_name: '电影感制作', description: '品牌片、预告片与情绪叙事',
          best_for: ['品牌发布', '概念预告'], source_hint: '可使用视频、图片，也可仅根据创意说明生成',
          output_hint: '一条完整成片', source_requirement: 'optional', output_mode: 'single',
          recommended_duration_seconds: 30,
        },
        {
          key: 'talking-head', display_name: '口播精剪', description: '人物讲解、访谈与课程内容',
          best_for: ['人物口播', '采访精剪'], source_hint: '需要一段包含人物讲话的原始视频',
          output_hint: '一条带字幕的精剪视频', source_requirement: 'video', output_mode: 'single',
          recommended_duration_seconds: 60,
        },
      ],
    })
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

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('requires a nonblank project name before creating a shared project', async () => {
    window.history.pushState({}, '', '/projects?create=true')
    render(<ProjectsPage />)
    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '   ' } })
    if (!screen.getByPlaceholderText<HTMLInputElement>('例如 我的科技博客').value) fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '测试品牌' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))
    expect(await screen.findByText('请输入项目名称')).toBeInTheDocument()
    expect(api.projects.create).not.toHaveBeenCalled()
  })

  it.each(['wechat-article', 'seednote', 'moments', 'montage', 'hypit'])('creates a shared project from %s without binding it to a platform', async (type) => {
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project: { ...projectWithReference, id: 'shared-project' } })
    window.history.pushState({}, '', `/projects?create=true&type=${type}&intent=new`)
    render(<ProjectsPage />)
    const dialog = await screen.findByRole('dialog', { name: '新建项目' })
    expect(within(dialog).queryByText('平台', { exact: true })).not.toBeInTheDocument()
    expect(within(dialog).queryByText('头像', { exact: true })).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('微信 AppID')).not.toBeInTheDocument()
    fireEvent.change(within(dialog).getByPlaceholderText('例如 我的科技博客'), { target: { value: '共享品牌项目' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.projects.create).toHaveBeenCalledTimes(1))
    const payload = vi.mocked(api.projects.create).mock.calls[0][0]
    expect(payload.name).toBe('共享品牌项目')
    for (const key of ['platform', 'avatar_url', 'agent_config', 'wechat_app_id', 'wechat_secret', 'hypit_defaults', 'montage_defaults', 'ecommerce_defaults']) {
      expect(payload).not.toHaveProperty(key)
    }
    expect(api.projects.upsertChannelConfig).not.toHaveBeenCalled()
  })

  it('creates a project with a reference session and no legacy URL field', async () => {
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project: {
      ...(await vi.mocked(api.projects.list)())[0],
      id: 'created-project',
      platform: 'seednote',
      image_analysis: {
        id: 'analysis-created',
        kind: 'project_visual_style',
        status: 'queued',
        attempt_count: 0,
        can_retry: false,
        updated_at: '2026-09-18T10:00:00Z',
      },
    } })
    window.history.pushState({}, '', '/projects?create=true&type=seednote&intent=new')

    render(<ProjectsPage />)

    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByLabelText('参考图文件'), {
      target: { files: [new File(['reference'], 'reference.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '创建' })).toBeEnabled())
    if (!screen.getByPlaceholderText<HTMLInputElement>('例如 我的科技博客').value) fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '测试品牌' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.projects.create).toHaveBeenCalled())
    const payload = vi.mocked(api.projects.create).mock.calls[0][0]
    expect(payload.reference_image).toEqual({
      upload_session_id: '11111111-1111-4111-8111-111111111111',
    })
    expect(payload).not.toHaveProperty('reference_image_url')
    expect(successMock).toHaveBeenCalledWith('项目已创建，正在后台识别视觉风格')
  })

  it('only polls the project list while an image analysis is active', async () => {
    vi.useFakeTimers()
    const activeProject: Project = {
      ...projectWithReference,
      image_analysis: {
        id: 'analysis-active',
        kind: 'project_visual_style',
        status: 'running',
        attempt_count: 1,
        can_retry: false,
        updated_at: '2026-09-18T10:00:00Z',
      },
    }
    vi.mocked(api.projects.list)
      .mockResolvedValueOnce([activeProject])
      .mockResolvedValue([projectWithReference])

    render(<ProjectsPage />)
    await vi.waitFor(() => expect(api.projects.list).toHaveBeenCalledTimes(1))
    await vi.advanceTimersByTimeAsync(2100)
    await vi.waitFor(() => expect(api.projects.list).toHaveBeenCalledTimes(2))

    await vi.advanceTimersByTimeAsync(2100)
    expect(api.projects.list).toHaveBeenCalledTimes(2)
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
    if (!screen.getByPlaceholderText<HTMLInputElement>('例如 我的科技博客').value) fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '测试品牌' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(errorMock).toHaveBeenCalledWith('上传会话已过期，请重新上传参考图'))
    expect(screen.getByRole('dialog', { name: '新建项目' })).toBeInTheDocument()
    expect(preview).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建' })).toBeEnabled()
  })

  it.each(['wechat', 'seednote', 'montage', 'hypit'] as const)('uploads an independent portrait for a %s project', async (platform) => {
    const project = { ...projectWithReference, platform }
    vi.mocked(api.projects.list).mockResolvedValue([project])
    vi.mocked(api.projects.update).mockResolvedValue(project)
    render(<ProjectsPage />)
    await clickProjectAction('测试项目', '编辑项目')
    fireEvent.change(await screen.findByLabelText('人物参考图文件'), { target: { files: [new File(['portrait'], 'portrait.png', { type: 'image/png' })] } })
    await waitFor(() => expect(uploadToOSSMock).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByRole('button', { name: '更新' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.projects.update).toHaveBeenCalled())
    expect(vi.mocked(api.projects.update).mock.calls[0][1]).toMatchObject({ portrait_reference_image: { upload_session_id: '11111111-1111-4111-8111-111111111111' } })
    expect(vi.mocked(api.projects.update).mock.calls[0][1]).not.toHaveProperty('reference_image')
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
    expect(payload).not.toHaveProperty('visual_style')
    expect(payload).not.toHaveProperty('reference_image_url')
  })

  it('polls an open active analysis and unlocks the generated style on completion', async () => {
    let resolveGet!: (value: Awaited<ReturnType<typeof api.projects.get>>) => void
    const runningProject: Project = {
      ...projectWithReference,
      visual_style: '',
      image_analysis: {
        id: 'analysis-running',
        kind: 'project_visual_style',
        status: 'running',
        attempt_count: 1,
        can_retry: false,
        updated_at: '2026-09-18T10:00:00Z',
      },
    }
    vi.mocked(api.projects.list).mockResolvedValue([runningProject])
    vi.mocked(api.projects.get).mockReturnValue(new Promise((resolve) => { resolveGet = resolve }))
    render(<ProjectsPage />)

    await clickProjectAction('测试项目', '编辑项目')
    const style = screen.getByPlaceholderText(/可手动描述封面与配图风格/)
    expect(style).toBeDisabled()
    resolveGet({
      project: {
        ...runningProject,
        visual_style: '最新自动视觉风格',
        visual_style_source: 'analysis',
        image_analysis: {
          ...runningProject.image_analysis!,
          status: 'succeeded',
          updated_at: '2026-09-18T10:01:00Z',
        },
      },
      stats: {
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

    await waitFor(() => expect(style).toBeEnabled())
    expect(style).toHaveValue('最新自动视觉风格')
  })

  it('edits legacy projects without resubmitting platform or account settings', async () => {
    vi.mocked(api.projects.update).mockResolvedValueOnce(projectWithReference)
    render(<ProjectsPage />)
    await clickProjectAction('测试项目', '编辑项目')
    const dialog = await screen.findByRole('dialog', { name: '编辑项目' })
    expect(within(dialog).queryByText('平台', { exact: true })).not.toBeInTheDocument()
    expect(within(dialog).queryByText('头像', { exact: true })).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('微信 AppID')).not.toBeInTheDocument()
    fireEvent.change(within(dialog).getByPlaceholderText('例如 我的科技博客'), { target: { value: '新的品牌名称' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.projects.update).toHaveBeenCalledWith('ch-1', { name: '新的品牌名称', instructions: '测试定位', keywords: '测试' }))
    expect(api.projects.upsertChannelConfig).not.toHaveBeenCalled()
  })

  it.each(['create failure', 'create success', 'update success'])('ignores a stale %s after another editor opens', async (outcome) => {
    const secondProject = { ...projectWithReference, id: 'second-project', name: '第二项目' }
    vi.mocked(api.projects.list).mockResolvedValue([projectWithReference, secondProject])
    vi.mocked(api.projects.update).mockResolvedValue(secondProject)
    let resolveWrite!: () => void
    let rejectWrite!: (reason: Error) => void
    if (outcome.startsWith('create')) {
      vi.mocked(api.projects.create).mockImplementationOnce(() => new Promise((resolve, reject) => {
        resolveWrite = () => resolve({ project: { ...projectWithReference, id: 'created-project' } })
        rejectWrite = reject
      }))
    } else {
      vi.mocked(api.projects.update).mockImplementationOnce(() => new Promise((resolve) => {
        resolveWrite = () => resolve(projectWithReference)
      }))
    }
    render(<ProjectsPage />)
    if (outcome.startsWith('create')) fireEvent.click(await screen.findByRole('button', { name: '新建项目' }))
    else await clickProjectAction('测试项目', '编辑项目')
    const originalDialog = await screen.findByRole('dialog', { name: outcome.startsWith('create') ? '新建项目' : '编辑项目' })
    fireEvent.change(within(originalDialog).getByPlaceholderText('例如 我的科技博客'), { target: { value: '第一个编辑会话' } })
    fireEvent.click(within(originalDialog).getByRole('button', { name: outcome.startsWith('create') ? '创建' : '更新' }))
    await waitFor(() => expect(outcome.startsWith('create') ? api.projects.create : api.projects.update).toHaveBeenCalledTimes(1))
    fireEvent.click(within(originalDialog).getByRole('button', { name: '取消' }))
    const confirmation = await screen.findByRole('alertdialog')
    fireEvent.click(within(confirmation).getByRole('button', { name: '放弃' }))
    await clickProjectAction('第二项目', '编辑项目')
    const currentDialog = await screen.findByRole('dialog', { name: '编辑项目' })
    fireEvent.change(within(currentDialog).getByPlaceholderText('例如 我的科技博客'), { target: { value: '第二个编辑会话' } })
    await act(async () => {
      if (outcome === 'create failure') rejectWrite(new Error('late failure'))
      else resolveWrite()
    })
    expect(currentDialog).toBeInTheDocument()
    expect(within(currentDialog).getByPlaceholderText('例如 我的科技博客')).toHaveValue('第二个编辑会话')
    fireEvent.click(within(currentDialog).getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.projects.update).toHaveBeenLastCalledWith('second-project', expect.objectContaining({ name: '第二个编辑会话' })))
  })

  it('returns to the original task flow with a newly created shared project', async () => {
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project: { ...projectWithReference, platform: '', id: 'shared-project' } })
    window.history.pushState({}, '', '/projects?create=true&type=wechat-picture&intent=new&return_to=/tasks')
    render(<ProjectsPage />)
    await screen.findByRole('dialog', { name: '新建项目' })
    if (!screen.getByPlaceholderText<HTMLInputElement>('例如 我的科技博客').value) fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '测试品牌' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))
    await waitFor(() => expect(window.location.pathname).toBe('/tasks'))
    const params = new URLSearchParams(window.location.search)
    expect(params.get('project_id')).toBe('shared-project')
    expect(params.get('type')).toBe('wechat-picture')
    expect(api.projects.upsertChannelConfig).not.toHaveBeenCalled()
  })
})
