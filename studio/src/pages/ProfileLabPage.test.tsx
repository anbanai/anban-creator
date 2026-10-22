import { StrictMode } from 'react'
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileLabPage from './ProfileLabPage'
import { api } from '@/lib/api'
import { createTestQueryClient, render } from '@/test/test-utils'
import type { Project, ProjectProfile, ProjectProfileSetup } from '@/types'

const { successMock, errorMock } = vi.hoisted(() => ({ successMock: vi.fn(), errorMock: vi.fn() }))

const project: Project = {
  id: 'project-1', user_id: 'user-1', platform: 'wechat', name: '云端手记', avatar_url: '',
  agent_ids: ['wechat-article'], keywords: '效率', instructions: '写真实经验', visual_style: '',
  writer: '', theme: '', author: '', image_ratio: '', max_concurrent_tasks: 1,
  status: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  profile_setup: {
    account_status: 'new', platform_accounts: [{ platform: 'wechat', profile_url: 'https://example.com/account' }],
    answers: { intent: { goals: '沉淀品牌', direction: '效率', differentiation: '真实实验', strengths: '', resources: '' }, content: { preferences: '实操', formats: '长文', tone: '克制', audience: '职场人', favorites: '', reference_accounts: '' }, boundaries: { exclusions: '夸大承诺', collaboration: '仅相关合作', compliance: '不涉及医疗' } },
    samples: ['https://example.com/sample'],
  },
}

const stats = { total_tasks: 0, completed_tasks: 0, failed_tasks: 0, running_tasks: 0, pending_tasks: 0, unused_topics: 0, success_rate: 0, last_activity_at: '' }

const profile: ProjectProfile = {
  schema_version: 1, status: 'confirmed', initialization_status: 'ready', version: 2,
  application_source: 'system_analysis',
  dimensions: {
    identity: { content: { name: '云端手记', positioning: '分享真实效率实验' }, sources: ['[用户确认]'], evidence: ['用户填写了人设定位'], missing_fields: ['核心栏目'] },
    style: { content: { tone: '克制、具体' }, sources: ['[链接分析]'], evidence: ['公开主页样本'], missing_fields: [] },
    audience: { content: { primary: '职场新人' }, sources: ['[推断待确认]'], evidence: ['依据用户方向推断'], missing_fields: [] },
    platforms: { content: { primary: 'wechat' }, sources: ['[用户确认]'], evidence: [], missing_fields: [] },
    preferences: { content: { exclusions: '夸大承诺' }, sources: ['[用户确认]'], evidence: [], missing_fields: [] },
    memory: { content: {}, sources: [], evidence: [], missing_fields: ['可沉淀的复盘经验'] },
  },
  analysis_limits: [], follow_up_questions: [],
}

vi.mock('sonner', () => ({ toast: { success: successMock, error: errorMock } }))
vi.mock('@/components/projects/ReferenceAssetUpload', () => ({
  ReferenceAssetUpload: ({ ariaLabel, onChange }: { ariaLabel?: string; onChange: (value: { upload_session_id: string } | null) => void }) => (
    <button type="button" aria-label={ariaLabel ?? '参考图文件'} onClick={() => onChange({ upload_session_id: '11111111-1111-4111-8111-111111111111' })}>上传参考图</button>
  ),
}))
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      projects: {
        ...actual.api.projects,
        get: vi.fn().mockImplementation(() => Promise.resolve({ project, stats })),
        create: vi.fn().mockImplementation(() => Promise.resolve({ project: { ...project, id: 'created-1' } })),
        update: vi.fn().mockImplementation(() => Promise.resolve(project)),
        getAccountProfile: vi.fn().mockImplementation(() => Promise.resolve(profile)),
        profileAnalysis: vi.fn().mockImplementation(() => Promise.resolve({ status: 'completed', profile })),
        profileRevisions: vi.fn().mockImplementation(() => Promise.resolve([
          { revision: 1, created_at: '2026-01-01T00:00:00Z', six_dimensions: profile.dimensions },
          { revision: 2, created_at: '2026-02-01T00:00:00Z', six_dimensions: profile.dimensions },
        ])),
        restoreProfileRevision: vi.fn().mockImplementation(() => Promise.resolve({ ...profile, version: 3 })),
        updateProfileDimension: vi.fn().mockImplementation(() => Promise.resolve(profile)),
        retryProfile: vi.fn().mockImplementation(() => Promise.resolve({ task: { id: 'task-retry', status: 'pending' }, profile: { ...profile, initialization_status: 'queued' } })),
        refreshProfile: vi.fn().mockImplementation(() => Promise.resolve({ task: { id: 'task-refresh', status: 'pending' }, profile: { ...profile, initialization_status: 'queued' } })),
      },
    },
  }
})

function renderRoute(path: string, queryClient?: QueryClient, strictMode = false) {
  window.history.pushState({}, '', path)
  const routes = (
    <Routes>
      <Route path="/projects/new" element={<ProfileLabPage />} />
      <Route path="/projects/:projectId/profile-lab" element={<ProfileLabPage />} />
    </Routes>
  )
  const content = queryClient ? <QueryClientProvider client={queryClient}>{routes}</QueryClientProvider> : routes
  return render(strictMode ? <StrictMode>{content}</StrictMode> : content)
}

function nextStep() {
  fireEvent.click(screen.getByRole('button', { name: '下一步' }))
}

function finishCreation() {
  nextStep()
  nextStep()
  nextStep()
  fireEvent.click(screen.getByRole('button', { name: '创建人设' }))
}

function openEditSave() {
  nextStep()
  nextStep()
  nextStep()
  fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
}

describe('ProfileLabPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('creates one project with any selected creation objects and the grouped account setup', async () => {
    renderRoute('/projects/new')
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '新品牌' } })
    for (const label of ['公众号文章', '公众号贴图', '种草笔记', '视频生成', '视频复刻', '白板视频']) {
      fireEvent.click(screen.getByRole('checkbox', { name: label }))
    }
    nextStep()
    fireEvent.change(screen.getByLabelText('主页链接 · 公众号'), { target: { value: 'https://example.com/wechat' } })
    fireEvent.change(screen.getByLabelText('主页链接 · 种草笔记'), { target: { value: 'https://example.com/seednote' } })
    nextStep()
    fireEvent.change(screen.getByLabelText('运营目标'), { target: { value: '建立个人品牌' } })
    nextStep()
    fireEvent.change(screen.getByLabelText('内容偏好'), { target: { value: '案例和实操' } })
    fireEvent.change(screen.getByLabelText('内容红线'), { target: { value: '不做夸大承诺' } })
    fireEvent.click(screen.getByRole('button', { name: '创建人设' }))

    await waitFor(() => expect(api.projects.create).toHaveBeenCalledTimes(1))
    expect(api.projects.create).toHaveBeenCalledWith(expect.objectContaining({
      name: '新品牌',
      agent_ids: ['wechat-article', 'wechat-picture', 'seednote', 'montage', 'hypit', 'whiteboard-animation'],
      profile_setup: expect.objectContaining({
        account_status: 'new',
        platform_accounts: expect.arrayContaining([
          expect.objectContaining({ platform: 'wechat', profile_url: 'https://example.com/wechat' }),
          expect.objectContaining({ platform: 'seednote', profile_url: 'https://example.com/seednote' }),
        ]),
        answers: expect.objectContaining({
          intent: expect.objectContaining({ goals: '建立个人品牌' }),
          content: expect.objectContaining({ preferences: '案例和实操' }),
          boundaries: expect.objectContaining({ exclusions: '不做夸大承诺' }),
        }),
      }),
    }))
    expect(api.projects.create).not.toHaveBeenCalledWith(expect.objectContaining({
      profile_setup: expect.objectContaining({ analyze: false }),
    }))
  })

  it('sends analyze false when the user skips automatic profile analysis', async () => {
    renderRoute('/projects/new')
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '只建人设' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '公众号文章' }))
    nextStep()
    nextStep()
    nextStep()
    fireEvent.click(screen.getByRole('checkbox', { name: '跳过画像分析' }))
    fireEvent.click(screen.getByRole('button', { name: '创建人设' }))
    await waitFor(() => expect(api.projects.create).toHaveBeenCalled())
    expect(api.projects.create).toHaveBeenCalledWith(expect.objectContaining({
      profile_setup: expect.objectContaining({ analyze: false }),
    }))
  })

  it('navigates to the new project under React Strict Mode after creation succeeds', async () => {
    vi.mocked(api.projects.getAccountProfile).mockResolvedValueOnce({ ...profile, initialization_status: 'queued' })
    renderRoute('/projects/new', undefined, true)
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: 'Strict Mode 人设' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '公众号文章' }))
    finishCreation()

    await waitFor(() => expect(window.location.pathname).toBe('/projects/created-1/profile-lab'))
    expect(await screen.findByText('分析中')).toBeInTheDocument()
  })

  it('includes visual style and an uploaded project reference in the creation request', async () => {
    renderRoute('/projects/new')
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '视觉人设' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '公众号文章' }))
    nextStep()
    fireEvent.click(screen.getByText('更多资料（可选）'))
    fireEvent.change(screen.getByLabelText('视觉风格描述'), { target: { value: '自然光、低饱和' } })
    fireEvent.click(screen.getByRole('button', { name: '人设参考图文件' }))
    nextStep()
    nextStep()
    fireEvent.click(screen.getByRole('button', { name: '创建人设' }))

    await waitFor(() => expect(api.projects.create).toHaveBeenCalledWith(expect.objectContaining({
      visual_style: '自然光、低饱和',
      reference_image: { upload_session_id: '11111111-1111-4111-8111-111111111111' },
    })))
  })

  it('retains a legacy homepage URL returned in the migrated project setup when saving', async () => {
    const legacySetup: ProjectProfileSetup = {
      ...project.profile_setup!,
      account_status: 'existing',
      platform_accounts: [{ platform: 'wechat', profile_url: 'https://mp.weixin.qq.com/s/legacy', source: '[用户确认]' }],
    }
    vi.mocked(api.projects.get).mockResolvedValueOnce({ project: { ...project, profile_setup: legacySetup }, stats })
    renderRoute('/projects/project-1/profile-lab')
    await screen.findByRole('heading', { name: '云端手记' })
    openEditSave()

    await waitFor(() => expect(api.projects.update).toHaveBeenCalledWith('project-1', expect.objectContaining({
      profile_setup: expect.objectContaining({
        platform_accounts: [expect.objectContaining({ platform: 'wechat', profile_url: 'https://mp.weixin.qq.com/s/legacy' })],
      }),
    })))
  })

  it('edits profile values as readable fields and shows provenance instead of JSON', async () => {
    renderRoute('/projects/project-1/profile-lab')
    await screen.findByRole('heading', { name: '云端手记' })
    const identity = screen.getByRole('region', { name: '定位 identity' })
    expect(within(identity).getByLabelText('定位')).toHaveValue('分享真实效率实验')
    expect(within(identity).getByText('[用户确认]')).toBeInTheDocument()
    expect(within(identity).getByText('用户填写了人设定位')).toBeInTheDocument()
    expect(screen.queryByLabelText(/JSON|画像 JSON/i)).not.toBeInTheDocument()
    expect(screen.getAllByText('[推断待确认]').length).toBeGreaterThan(0)
    expect(screen.getByText(/preferences.*硬性约束/)).toBeInTheDocument()
  })

  it('renders a partial server profile with null arrays and missing dimensions', async () => {
    vi.mocked(api.projects.getAccountProfile).mockResolvedValueOnce({
      ...profile,
      dimensions: {
        identity: { content: null, sources: null, evidence: null, missing_fields: null },
      },
      analysis_limits: null,
      follow_up_questions: null,
    } as unknown as ProjectProfile)
    renderRoute('/projects/project-1/profile-lab')

    const identity = await screen.findByRole('region', { name: '定位 identity' })
    expect(identity).toBeInTheDocument()
    for (const [key, label] of [['style', '风格'], ['audience', '受众'], ['platforms', '平台'], ['preferences', '偏好与红线'], ['memory', '经验记忆']] as const) {
      expect(screen.getByRole('region', { name: `${label} ${key}` })).toBeInTheDocument()
    }
    expect(within(identity).getByText('[待补充] 当前没有已记录内容。', { exact: false })).toBeInTheDocument()
  })

  it('offers retry after analysis failure and keeps version restoration disabled while analysis runs', async () => {
    vi.mocked(api.projects.getAccountProfile).mockResolvedValueOnce({ ...profile, initialization_status: 'failed', last_error: '主页访问受限' })
    renderRoute('/projects/project-1/profile-lab')
    expect(await screen.findByText('主页访问受限')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重新分析' }))
    await waitFor(() => expect(api.projects.retryProfile).toHaveBeenCalledWith('project-1', 2))
    expect(await screen.findByRole('region', { name: '画像版本历史' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复版本 1' })).toBeDisabled()
  })

  it('restores a prior profile revision when analysis is idle', async () => {
    renderRoute('/projects/project-1/profile-lab')
    await screen.findByRole('region', { name: '画像版本历史' })
    fireEvent.click(screen.getByRole('button', { name: '恢复版本 1' }))
    await waitFor(() => expect(api.projects.restoreProfileRevision).toHaveBeenCalledWith('project-1', 1, 2))
  })

  it('uses a simple single-column workspace with a four-step progress line', async () => {
    renderRoute('/projects/new')
    const layout = screen.getByTestId('profile-lab-layout')
    expect(layout).toHaveClass('max-w-3xl')
    expect(screen.getByRole('navigation', { name: '创建进度' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '1. 基础信息' })).toHaveAttribute('aria-current', 'step')
    expect(screen.queryByLabelText('运营目标')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '简洁人设' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '公众号文章' }))
    nextStep()
    expect(screen.queryByLabelText('运营目标')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '你的公开账号' })).toBeInTheDocument()
  })

  it('keeps progress-line navigation sequential when an earlier step is invalid', async () => {
    renderRoute('/projects/new')
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '顺序校验人设' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '公众号文章' }))
    fireEvent.click(screen.getByRole('button', { name: '下一步' }))
    expect(screen.getByRole('button', { name: '2. 社媒链接' })).toHaveAttribute('aria-current', 'step')

    fireEvent.change(screen.getByLabelText('主页链接 · 公众号'), { target: { value: 'invalid' } })
    fireEvent.click(screen.getByRole('button', { name: '4. 偏好与红线' }))
    expect(screen.getByRole('alert')).toHaveTextContent('请填写有效的公开 HTTP 或 HTTPS 链接')
    expect(screen.getByRole('button', { name: '2. 社媒链接' })).toHaveAttribute('aria-current', 'step')
    expect(screen.queryByLabelText('内容偏好')).not.toBeInTheDocument()
  })

  it('requires a name and at least one object, validates optional URLs, and retains account details without a URL', async () => {
    renderRoute('/projects/new')
    fireEvent.click(screen.getByRole('button', { name: '下一步' }))
    expect(screen.getByRole('alert')).toHaveTextContent('请填写人设名称')
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '视频计划' } })
    fireEvent.click(screen.getByRole('button', { name: '下一步' }))
    expect(screen.getByRole('alert')).toHaveTextContent('至少选择一种创作对象')
    fireEvent.click(screen.getByRole('checkbox', { name: '视频生成' }))
    nextStep()
    fireEvent.change(screen.getByLabelText('主页链接 · 公众号'), { target: { value: 'invalid' } })
    nextStep()
    expect(screen.getByRole('alert')).toHaveTextContent('请填写有效的公开 HTTP 或 HTTPS 链接')
    expect(api.projects.create).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('主页链接 · 公众号'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('公众号名称'), { target: { value: '筹备中的账号' } })
    nextStep()
    nextStep()
    fireEvent.click(screen.getByRole('button', { name: '创建人设' }))
    await waitFor(() => expect(api.projects.create).toHaveBeenCalledWith(expect.objectContaining({
      agent_ids: ['montage'], platform: 'montage',
      profile_setup: expect.objectContaining({ platform_accounts: [expect.objectContaining({ account_name: '筹备中的账号', profile_url: '' })] }),
    })))
  })

  it('preserves nested objects, typed arrays, booleans and numbers while editing ordinary controls', async () => {
    const typedProfile = {
      ...profile,
      dimensions: { ...profile.dimensions, identity: { ...profile.dimensions.identity, content: {
        name: '云端手记', details: { goals: '长期创作', count: 3, enabled: true }, accounts: [{ name: '主账号', platform: 'wechat' }], weights: [1, 2],
      } } },
    }
    vi.mocked(api.projects.getAccountProfile).mockResolvedValueOnce(typedProfile)
    renderRoute('/projects/project-1/profile-lab')
    const identity = await screen.findByRole('region', { name: '定位 identity' })
    fireEvent.change(within(identity).getByLabelText('运营目标'), { target: { value: '更新方向' } })
    fireEvent.click(within(identity).getByRole('button', { name: '保存定位' }))
    await waitFor(() => expect(api.projects.updateProfileDimension).toHaveBeenCalledWith('project-1', 'identity', 2, expect.objectContaining({
      content: { name: '云端手记', details: { goals: '更新方向', count: 3, enabled: true }, accounts: [{ name: '主账号', platform: 'wechat' }], weights: [1, 2] },
    })))
  })

  it('removes a numeric profile field when the number input is cleared', async () => {
    const typedProfile = {
      ...profile,
      dimensions: { ...profile.dimensions, identity: { ...profile.dimensions.identity, content: {
        name: '云端手记', details: { goals: '长期创作', count: 3, enabled: true },
      } } },
    }
    vi.mocked(api.projects.getAccountProfile).mockResolvedValueOnce(typedProfile)
    renderRoute('/projects/project-1/profile-lab')
    const identity = await screen.findByRole('region', { name: '定位 identity' })
    fireEvent.change(within(identity).getByLabelText('Count'), { target: { value: '' } })
    fireEvent.click(within(identity).getByRole('button', { name: '保存定位' }))
    await waitFor(() => expect(api.projects.updateProfileDimension).toHaveBeenCalledWith('project-1', 'identity', 2, expect.objectContaining({
      content: { name: '云端手记', details: { count: null, goals: '长期创作', enabled: true } },
    })))
  })

  it('keeps unsaved changes when automatic analysis applies and rejects saving a stale revision', async () => {
    const queryClient = createTestQueryClient()
    vi.mocked(api.projects.getAccountProfile).mockResolvedValueOnce({ ...profile, initialization_status: 'running' })
    renderRoute('/projects/project-1/profile-lab', queryClient)
    const identity = await screen.findByRole('region', { name: '定位 identity' })
    expect(screen.getByText(/当前 v2 继续用于创作/)).toBeInTheDocument()
    fireEvent.change(within(identity).getByLabelText('定位'), { target: { value: '未保存的手动定位' } })
    await act(async () => {
      queryClient.setQueryData(['project-profile', 'project-1'], {
        ...profile, version: 3, dimensions: { ...profile.dimensions, identity: { ...profile.dimensions.identity, content: { ...profile.dimensions.identity.content, positioning: '后台分析的新定位' } } },
      })
    })
    expect(within(identity).getByLabelText('定位')).toHaveValue('未保存的手动定位')
    expect(await within(identity).findByText(/未保存内容已保留/)).toBeInTheDocument()
    expect(within(identity).getByRole('button', { name: '保存定位' })).toBeDisabled()
    fireEvent.click(within(identity).getByRole('button', { name: '载入最新定位' }))
    expect(within(identity).getByLabelText('定位')).toHaveValue('后台分析的新定位')
  })

  it('does not regress the profile cache when an older save response arrives after an automatic update', async () => {
    const queryClient = createTestQueryClient()
    let finishSave!: (value: ProjectProfile) => void
    vi.mocked(api.projects.updateProfileDimension).mockReturnValueOnce(new Promise((resolve) => { finishSave = resolve }))
    renderRoute('/projects/project-1/profile-lab', queryClient)
    const identity = await screen.findByRole('region', { name: '定位 identity' })
    fireEvent.change(within(identity).getByLabelText('定位'), { target: { value: '手动编辑' } })
    fireEvent.click(within(identity).getByRole('button', { name: '保存定位' }))
    const newestProfile = { ...profile, version: 4, dimensions: { ...profile.dimensions, identity: { ...profile.dimensions.identity, content: { positioning: '最新自动画像' } } } }
    await act(async () => {
      queryClient.setQueryData(['project-profile', 'project-1'], newestProfile)
      finishSave({ ...profile, version: 3 })
    })
    expect(queryClient.getQueryData<ProjectProfile>(['project-profile', 'project-1'])?.version).toBe(4)
    await waitFor(() => expect(within(identity).getByLabelText('定位')).toHaveValue('最新自动画像'))
  })

  it('updates selected objects and optional account settings without starting analysis when unchecked', async () => {
    renderRoute('/projects/project-1/profile-lab')
    await screen.findByRole('heading', { name: '云端手记' })
    fireEvent.click(screen.getByRole('checkbox', { name: '视频生成' }))
    nextStep()
    fireEvent.click(screen.getByRole('button', { name: '添加账号' }))
    fireEvent.change(screen.getByLabelText('账号 2 平台'), { target: { value: 'seednote' } })
    fireEvent.change(screen.getByLabelText('种草账号名称'), { target: { value: '新账号' } })
    nextStep()
    nextStep()
    fireEvent.click(screen.getByRole('checkbox', { name: '保存后重新分析画像' }))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(api.projects.update).toHaveBeenCalledWith('project-1', expect.objectContaining({
      agent_ids: ['wechat-article', 'montage'],
      profile_setup: expect.objectContaining({ platform_accounts: expect.arrayContaining([expect.objectContaining({ platform: 'seednote', account_name: '新账号' })]) }),
    })))
    expect(api.projects.refreshProfile).not.toHaveBeenCalled()
  })

  it('distinguishes automatic application and shows recoverable profile/history loading errors', async () => {
    vi.mocked(api.projects.profileRevisions).mockRejectedValueOnce(new Error('版本历史暂不可用'))
    renderRoute('/projects/project-1/profile-lab')
    expect(await screen.findByText('系统已应用')).toBeInTheDocument()
    expect(await screen.findByText('版本历史暂不可用')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新加载版本历史' })).toBeInTheDocument()
  })

  it('preserves cross-platform and repeated account records and accepts samples as text', async () => {
    vi.mocked(api.projects.get).mockResolvedValueOnce({ stats, project: {
      ...project,
      profile_setup: { ...project.profile_setup!, platform_accounts: [
        { platform: 'douyin', profile_url: 'https://example.com/douyin', account_name: '视频号甲' },
        { platform: 'douyin', profile_url: '', account_name: '视频号乙' },
        { platform: 'zhihu', profile_url: 'https://example.com/zhihu' },
      ] },
    } } as Awaited<ReturnType<typeof api.projects.get>>)
    renderRoute('/projects/project-1/profile-lab')
    await screen.findByRole('heading', { name: '云端手记' })
    nextStep()
    expect(screen.getByLabelText('抖音名称 · 1')).toHaveValue('视频号甲')
    expect(screen.getByLabelText('抖音名称 · 2')).toHaveValue('视频号乙')
    expect(screen.getByLabelText('主页链接 · 知乎')).toHaveValue('https://example.com/zhihu')
    fireEvent.click(screen.getByRole('button', { name: '删除账号 3' }))
    fireEvent.change(screen.getByLabelText('内容样本'), { target: { value: '第一个内容标题\n来自用户粘贴的内容片段' } })
    nextStep()
    nextStep()
    fireEvent.click(screen.getByRole('checkbox', { name: '保存后重新分析画像' }))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(api.projects.update).toHaveBeenCalledWith('project-1', expect.objectContaining({
      profile_setup: expect.objectContaining({ platform_accounts: [
        expect.objectContaining({ platform: 'douyin', account_name: '视频号甲' }),
        expect.objectContaining({ platform: 'douyin', account_name: '视频号乙', profile_url: '' }),
      ], samples: ['第一个内容标题', '来自用户粘贴的内容片段'] }),
    })))
    const request = vi.mocked(api.projects.update).mock.calls[0][1]
    expect(request).not.toHaveProperty('agent_ids')
    expect(request).not.toHaveProperty('platform')
  })

  it('keeps unsupported legacy projects in their basic settings flow', async () => {
    vi.mocked(api.projects.get).mockResolvedValueOnce({ stats, project: { ...project, platform: 'ecommerce', agent_ids: null } })
    renderRoute('/projects/project-1/profile-lab')
    expect(await screen.findByRole('heading', { name: '该人设使用专属创作设置' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回人设' })).toHaveAttribute('href', '/projects')
    expect(screen.queryByRole('button', { name: '保存设置' })).not.toBeInTheDocument()
  })

  it('does not add accounts back after every record is deliberately removed', async () => {
    vi.mocked(api.projects.get).mockResolvedValueOnce({ stats, project: { ...project, profile_setup: { ...project.profile_setup!, platform_accounts: [] } } })
    renderRoute('/projects/project-1/profile-lab')
    await screen.findByRole('heading', { name: '云端手记' })
    nextStep()
    expect(screen.queryByLabelText('主页链接 · 公众号')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('主页链接 · 种草笔记')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '添加账号' }))
    expect(screen.getByLabelText('账号 1 平台')).toHaveValue('wechat')
  })

  it('returns to the original task flow after full-page creation', async () => {
    renderRoute('/projects/new?type=wechat-picture&intent=new&return_to=/tasks')
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '贴图人设' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '公众号贴图' }))
    finishCreation()
    await waitFor(() => expect(window.location.pathname).toBe('/tasks'))
    expect(new URLSearchParams(window.location.search).get('project_id')).toBe('created-1')
    expect(new URLSearchParams(window.location.search).get('type')).toBe('wechat-picture')
  })

  it('does not redirect back into the new project after leaving while creation is pending', async () => {
    let resolveCreate!: (value: { project: Project }) => void
    vi.mocked(api.projects.create).mockReturnValueOnce(new Promise((resolve) => { resolveCreate = resolve }))
    renderRoute('/projects/new')
    fireEvent.change(screen.getByLabelText('人设名称'), { target: { value: '离开中的人设' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '公众号文章' }))
    finishCreation()
    fireEvent.click(screen.getByRole('link', { name: '人设' }))
    expect(window.location.pathname).toBe('/projects')
    await act(async () => resolveCreate({ project: { ...project, id: 'late-created' } }))
    await waitFor(() => expect(window.location.pathname).toBe('/projects'))
  })
})
