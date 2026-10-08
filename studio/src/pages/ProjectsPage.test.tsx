import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectsPage from './ProjectsPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import { mockPlatformConfigs } from '@/test/mocks/handlers'
import type { Project, ProjectProfile } from '@/types'

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

function profileFixture(status: ProjectProfile['status'] = 'draft', initialization_status: ProjectProfile['initialization_status'] = 'not_started'): ProjectProfile {
  const dimension = { content: {}, sources: [], evidence: [], missing_fields: [] }
  return {
    schema_version: 1,
    status,
    initialization_status,
    version: 0,
    dimensions: { identity: dimension, style: dimension, audience: dimension, platforms: dimension, preferences: dimension, memory: dimension },
    analysis_limits: [],
    follow_up_questions: [],
  }
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
        getAccountProfile: vi.fn(),
        refreshProfile: vi.fn(),
        retryProfile: vi.fn(),
        profileAnalysis: vi.fn(),
        confirmAccountProfile: vi.fn(),
        updateProfileDimension: vi.fn(),
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
  it('does not repeat workspace navigation already available in the sidebar', async () => {
    render(<ProjectsPage />)
    await screen.findByRole('heading', { name: '项目' })
    expect(screen.queryByRole('navigation', { name: '工作区导航' })).not.toBeInTheDocument()
  })
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
    vi.mocked(api.projects.getAccountProfile).mockReset()
		vi.mocked(api.projects.refreshProfile).mockReset()
		vi.mocked(api.projects.retryProfile).mockReset()
		vi.mocked(api.projects.profileAnalysis).mockReset()
    vi.mocked(api.projects.confirmAccountProfile).mockReset()
		vi.mocked(api.projects.getAccountProfile).mockResolvedValue(profileFixture())
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

  it('keeps a legacy project portrait available as a default without exposing project-level upload controls', async () => {
    const project = {
      ...projectWithReference,
      portrait_reference_image: {
        asset_id: '55555555-5555-4555-8555-555555555555',
        file_name: 'legacy-portrait.png',
        content_type: 'image/png',
        size: 9,
        download_url: 'https://signed.example/legacy-portrait.png',
        download_expires_at: '2026-07-20T10:00:00Z',
      },
    }
    vi.mocked(api.projects.list).mockResolvedValue([project])
    vi.mocked(api.projects.update).mockResolvedValue(project)
    render(<ProjectsPage />)
    await clickProjectAction('测试项目', '编辑项目')
    const dialog = await screen.findByRole('dialog', { name: '编辑项目' })
    expect(within(dialog).queryByLabelText('人物参考图文件')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/人物参考图保留为任务和计划的默认值/)).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.projects.update).toHaveBeenCalled())
    expect(vi.mocked(api.projects.update).mock.calls[0][1]).not.toHaveProperty('portrait_reference_image')
  })

  it('offers the optional profile guide after quick creation and lets the user skip it', async () => {
    const createdProject: Project = { ...projectWithReference, id: 'created-project', platform: '', name: '快速创建项目' }
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project: createdProject, profile_initialization: { status: 'not_started' } })
    vi.mocked(api.projects.list).mockResolvedValueOnce([projectWithReference]).mockResolvedValueOnce([projectWithReference, createdProject])
    window.history.pushState({}, '', '/projects?create=true')
    render(<ProjectsPage />)
    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '快速创建项目' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))
    await waitFor(() => expect(api.projects.create).toHaveBeenCalledWith(expect.objectContaining({ name: '快速创建项目' })))
    await waitFor(() => expect(successMock).toHaveBeenCalledWith('项目创建成功'))
    const guide = await screen.findByRole('dialog', { name: '项目画像引导' })
    expect(within(guide).getByRole('heading', { name: '基础信息' })).toBeInTheDocument()
    fireEvent.click(within(guide).getByRole('button', { name: '暂时跳过' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '项目画像引导' })).not.toBeInTheDocument())
    expect(api.projects.refreshProfile).not.toHaveBeenCalled()
    expect(screen.getByText('快速创建项目')).toBeInTheDocument()
  })

  it('shows the portrait guide before returning to task creation from a project shortcut', async () => {
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project: { ...projectWithReference, id: 'return-project', platform: '' }, profile_initialization: { status: 'not_started' } })
    window.history.pushState({}, '', '/projects?return_to=%2Ftasks&create=true&type=seednote&intent=new')
    render(<ProjectsPage />)
    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '从任务入口创建' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    const guide = await screen.findByRole('dialog', { name: '项目画像引导' })
    fireEvent.click(within(guide).getByRole('button', { name: '暂时跳过' }))

    await waitFor(() => expect(window.location.pathname).toBe('/tasks'))
    expect(window.location.search).toContain('project_id=return-project')
  })

  it('submits the four-step account guide as structured profile answers', async () => {
    const project: Project = { ...projectWithReference, id: 'created-project', platform: '', name: '引导项目' }
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project, profile_initialization: { status: 'not_started' } })
    const queuedProfile = { schema_version: 1, status: 'draft' as const, initialization_status: 'queued' as const, version: 1, analysis_task_id: 'profile-task', dimensions: {}, analysis_limits: [], follow_up_questions: [] }
    vi.mocked(api.projects.refreshProfile).mockResolvedValueOnce({ task: { id: 'profile-task', status: 'pending' } as never, profile: queuedProfile as never })
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue(queuedProfile as never)
    vi.mocked(api.projects.profileAnalysis).mockResolvedValue({ status: 'pending', profile: queuedProfile as never })
    window.history.pushState({}, '', '/projects?create=true')
    render(<ProjectsPage />)

    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '引导项目' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))
    const guide = await screen.findByRole('dialog', { name: '项目画像引导' })
    fireEvent.click(within(guide).getByRole('checkbox', { name: '小红书' }))
    fireEvent.click(within(guide).getByRole('radio', { name: '已有账号' }))
    fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.change(within(guide).getByLabelText('小红书账号名'), { target: { value: '小红书品牌号' } })
    fireEvent.change(within(guide).getByLabelText('小红书主页链接'), { target: { value: 'https://www.xiaohongshu.com/user/profile/test' } })
    fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.change(within(guide).getByLabelText('运营目标'), { target: { value: '建立品牌认知' } })
    fireEvent.change(within(guide).getByLabelText('内容方向'), { target: { value: '设计方法' } })
    fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.change(within(guide).getByLabelText('内容偏好'), { target: { value: '具体、有案例' } })
    fireEvent.change(within(guide).getByLabelText('目标受众'), { target: { value: '独立设计师' } })
    fireEvent.change(within(guide).getByLabelText('合规红线'), { target: { value: '不做夸大承诺' } })
    expect(within(guide).getByRole('heading', { name: '偏好与红线' })).toBeInTheDocument()
    fireEvent.click(within(guide).getByRole('button', { name: '开始生成画像' }))

    await waitFor(() => expect(api.projects.refreshProfile).toHaveBeenCalledWith('created-project', 1, expect.objectContaining({
      basic: { project_name: '引导项目', account_status: 'existing' },
      platform_accounts: [{ platform: 'xiaohongshu', account_name: '小红书品牌号', profile_url: 'https://www.xiaohongshu.com/user/profile/test' }],
      intent: { goals: '建立品牌认知', direction: '设计方法', differentiation: '' },
      content: { preferences: '具体、有案例', formats: '', tone: '', audience: '独立设计师' },
      boundaries: { exclusions: '', collaboration: '', compliance: '不做夸大承诺' },
    })))
    expect(within(guide).getByText(/画像任务已加入队列/)).toBeInTheDocument()
  })

  it('allows an unknown account status and submits it as empty', async () => {
    const project: Project = { ...projectWithReference, id: 'unknown-status-project', platform: '', name: '状态未知项目' }
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project, profile_initialization: { status: 'not_started' } })
    const queuedProfile = { ...profileFixture('draft', 'queued'), analysis_task_id: 'unknown-status-task' }
    vi.mocked(api.projects.refreshProfile).mockResolvedValueOnce({ task: { id: 'unknown-status-task', status: 'pending' } as never, profile: queuedProfile as never })
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue(queuedProfile as never)
    vi.mocked(api.projects.profileAnalysis).mockResolvedValue({ status: 'pending', profile: queuedProfile as never })
    window.history.pushState({}, '', '/projects?create=true')
    render(<ProjectsPage />)

    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '状态未知项目' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))
    const guide = await screen.findByRole('dialog', { name: '项目画像引导' })
    expect(within(guide).getByRole('radio', { name: '全新起号' })).not.toBeChecked()
    expect(within(guide).getByRole('radio', { name: '已有账号' })).not.toBeChecked()
    fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.click(within(guide).getByRole('button', { name: '开始生成画像' }))

    await waitFor(() => expect(api.projects.refreshProfile).toHaveBeenCalledWith('unknown-status-project', 0, expect.objectContaining({
      basic: { project_name: '状态未知项目', account_status: '' },
    })))
  })

  it('keeps a failed guide refresh from reporting the retained confirmed profile as successful', async () => {
    const confirmedProfile = profileFixture('confirmed', 'ready')
    confirmedProfile.analysis_task_id = 'previous-profile-task'
    const failedProfile = { ...confirmedProfile, initialization_status: 'failed' as const, analysis_task_id: 'failed-profile-task', last_error: 'analysis failed' }
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue(confirmedProfile)
    vi.mocked(api.projects.profileAnalysis)
      .mockResolvedValueOnce({ status: 'completed', profile: confirmedProfile })
      .mockResolvedValueOnce({ status: 'failed', profile: failedProfile })
    vi.mocked(api.projects.refreshProfile).mockResolvedValueOnce({
      task: { id: 'failed-profile-task', status: 'pending' } as never,
      profile: { ...confirmedProfile, initialization_status: 'queued', analysis_task_id: 'failed-profile-task' } as never,
    })
    render(<ProjectsPage />)

    fireEvent.click(await screen.findByRole('button', { name: '项目画像：测试项目' }))
    const profileDialog = await screen.findByRole('dialog', { name: '项目画像' })
    fireEvent.click(await within(profileDialog).findByRole('button', { name: '编辑引导并更新画像' }))
    const guide = await screen.findByRole('dialog', { name: '项目画像引导' })
    for (let step = 0; step < 3; step += 1) fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.click(within(guide).getByRole('button', { name: '开始生成画像' }))

    expect(await within(guide).findByText('画像任务失败，已确认的画像保持不变，可以重试。')).toBeInTheDocument()
    expect(within(guide).queryByText('画像已更新并自动应用。')).not.toBeInTheDocument()
    expect(within(guide).getByRole('button', { name: '重试画像分析' })).toBeEnabled()
  })

  it('keeps the first completed profile editable as a draft until the user confirms it', async () => {
    const project: Project = { ...projectWithReference, id: 'draft-project', platform: '', name: '待确认项目' }
    const draft = profileFixture('draft', 'ready')
    draft.analysis_task_id = 'draft-task'
    draft.dimensions.identity.content = { name: '旧定位' }
    vi.mocked(api.projects.create).mockResolvedValueOnce({ project, profile_initialization: { status: 'not_started' } })
    vi.mocked(api.projects.refreshProfile).mockResolvedValueOnce({ task: { id: 'draft-task', status: 'pending' } as never, profile: { ...draft, initialization_status: 'queued' } })
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue(profileFixture())
    vi.mocked(api.projects.profileAnalysis).mockResolvedValue({ status: 'completed', profile: draft })
    vi.mocked(api.projects.confirmAccountProfile).mockImplementation(async (_, profile) => ({ ...profile, status: 'confirmed' }))
    window.history.pushState({}, '', '/projects?create=true')
    render(<ProjectsPage />)

    await screen.findByRole('dialog', { name: '新建项目' })
    fireEvent.change(screen.getByPlaceholderText('例如 我的科技博客'), { target: { value: '待确认项目' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))
    const guide = await screen.findByRole('dialog', { name: '项目画像引导' })
    for (let step = 0; step < 3; step += 1) fireEvent.click(within(guide).getByRole('button', { name: '下一步' }))
    fireEvent.click(within(guide).getByRole('button', { name: '开始生成画像' }))
    const identity = await within(guide).findByLabelText('定位画像内容')
    const style = within(guide).getByLabelText('风格画像内容')
    expect(identity).toHaveValue(JSON.stringify({ name: '旧定位' }, null, 2))
    fireEvent.change(style, { target: { value: '{invalid' } })
    fireEvent.change(identity, { target: { value: '{"name":"人工修订"}' } })
    expect(within(guide).getByRole('button', { name: '确认并应用画像' })).toBeDisabled()
    expect(api.projects.confirmAccountProfile).not.toHaveBeenCalled()
    fireEvent.change(style, { target: { value: '{"voice":"清晰"}' } })
    expect(within(guide).getByRole('button', { name: '确认并应用画像' })).toBeEnabled()
    fireEvent.click(within(guide).getByRole('button', { name: '确认并应用画像' }))

    await waitFor(() => expect(api.projects.confirmAccountProfile).toHaveBeenCalledWith('draft-project', expect.objectContaining({
      status: 'draft',
      dimensions: expect.objectContaining({
        identity: expect.objectContaining({ content: { name: '人工修订' } }),
        style: expect.objectContaining({ content: { voice: '清晰' } }),
      }),
    })))
    expect(within(guide).queryByText('画像已更新并自动应用。')).not.toBeInTheDocument()
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
    const guide = await screen.findByRole('dialog', { name: '项目画像引导' })
    fireEvent.click(within(guide).getByRole('button', { name: '暂时跳过' }))
    await waitFor(() => expect(window.location.pathname).toBe('/tasks'))
    const params = new URLSearchParams(window.location.search)
    expect(params.get('project_id')).toBe('shared-project')
    expect(params.get('type')).toBe('wechat-picture')
    expect(api.projects.upsertChannelConfig).not.toHaveBeenCalled()
  })
})
