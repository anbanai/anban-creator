import { QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AssistantWorkspace from './AssistantWorkspace'
import ProjectsPage from './ProjectsPage'
import { createTestQueryClient } from '@/test/test-utils'
import { mockProjects } from '@/test/mocks/handlers'
import type { Project } from '@/types'
import type { AssistantBootstrap } from '@/lib/api/assistant'

const mocks = vi.hoisted(() => ({
  bootstrap: vi.fn(),
  projectsList: vi.fn(),
  projectUpdate: vi.fn(),
  projectArchive: vi.fn(),
  projectRestore: vi.fn(),
  projectStats: vi.fn(),
  works: vi.fn(),
  createWorkspace: vi.fn(),
  workspaceMessage: vi.fn(),
  workspaceAnswer: vi.fn(),
  switchWorkspaceMode: vi.fn(),
  closeWorkspace: vi.fn(),
  getInteractionSnapshot: vi.fn(),
  workspaceCommit: vi.fn(),
  workspaceCheckpoint: vi.fn(),
  taskGet: vi.fn(),
  taskFiles: vi.fn(),
  agentProfiles: vi.fn(),
  imageCapabilities: vi.fn(),
  platformConfigs: vi.fn(),
  userMediaList: vi.fn(),
  streamEvents: [] as { event: string; id: string; data: string }[],
}))

const uploadToOSSMock = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      assistant: { ...actual.api.assistant, ...mocks },
      agentProfiles: { ...actual.api.agentProfiles, list: mocks.agentProfiles },
      imageCapabilities: { ...actual.api.imageCapabilities, list: mocks.imageCapabilities },
      projects: { ...actual.api.projects, list: mocks.projectsList, update: mocks.projectUpdate, platformConfigs: mocks.platformConfigs, archive: mocks.projectArchive, restore: mocks.projectRestore, stats: mocks.projectStats },
      userMedia: { ...actual.api.userMedia, list: mocks.userMediaList },
      tasks: { ...actual.api.tasks, get: mocks.taskGet, files: mocks.taskFiles },
    },
  }
})

vi.mock('@/lib/interactive-events', () => ({
  streamAssistantWorkspace: (workspaceId: string, _token: string, _signal: AbortSignal, options?: { expectedSessionId?: string }) => (async function* () {
    const sessionId = workspaceId === 'locked-workspace' ? 'locked-session' : options?.expectedSessionId ?? 'session-1'
    const terminalStream = mocks.streamEvents.some((event) => ['session.expired', 'expired'].includes(event.event))
    if (!terminalStream) {
      yield { event: 'assistant.message', id: `event-${sessionId}-1`, data: JSON.stringify({ session_id: sessionId, text: '收到，我开始处理。' }) }
      if (sessionId === 'session-1' && !mocks.streamEvents.some((event) => event.event === 'question.opened')) yield { event: 'draft.ready', id: `event-${sessionId}-2`, data: JSON.stringify({ session_id: sessionId, confirmation_token: 'token-1', ready: true, project: { platform: 'video-edit' } }) }
      yield { event: 'turn.end', id: `event-${sessionId}-end`, data: JSON.stringify({ session_id: sessionId }) }
    }
    for (const event of mocks.streamEvents) yield event
  })(),
}))

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ token: 'token-1' }) }))

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname}{location.search}</output>
}

function renderPage(state?: Partial<{ modeId: 'profile' | 'creation' | 'review'; projectId: string; contentId: string; checkpointId: string; initialMessage: string }>, queryClient = createTestQueryClient()) {
  return { ...render(<QueryClientProvider client={queryClient}><MemoryRouter initialEntries={[{ pathname: '/', state }]}><AssistantWorkspace /><LocationProbe /></MemoryRouter></QueryClientProvider>), queryClient }
}

function activePersona(id: string, name: string, overrides: Partial<Project> = {}): Project {
  return { ...mockProjects[0], id, name, platform: 'wechat', image_ratio: '', status: 'active', ...overrides }
}

function setBootstrap(value: AssistantBootstrap) {
  mocks.bootstrap.mockResolvedValue(value)
  mocks.projectsList.mockResolvedValue(value.projects.map((project) => activePersona(project.id, project.name, { platform: project.platform as Project['platform'] })))
}

function emptyDimensions(): NonNullable<Project['profile']>['dimensions'] {
  const dimension = { content: {}, sources: [], evidence: [], missing_fields: [] }
  return { identity: dimension, style: dimension, audience: dimension, platforms: dimension, preferences: dimension, memory: dimension }
}

describe('AssistantWorkspace', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    vi.clearAllMocks()
    mocks.streamEvents = []
    sessionStorage.clear()
    mocks.projectsList.mockResolvedValue([])
    mocks.projectStats.mockResolvedValue({})
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [],
    })
    mocks.works.mockResolvedValue({ items: [], has_more: false })
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'profile', session_id: 'session-1', sessions: { profile: 'session-1' }, runtime_status: 'starting', expires_at: '' })
    mocks.getInteractionSnapshot.mockResolvedValue({
      interaction_id: 'workspace-1', mode_id: 'profile', project_id: 'draft-ready', lifecycle: 'needs_confirmation', generation: 1,
      runtime_status: 'running', expires_at: '', last_activity_at: '', available_actions: ['confirm_draft'],
      drafts: [{ draft_id: 'draft-1', mode_id: 'profile', revision: 1, payload: { project: { name: '恢复后的人设', platform: 'wechat' } }, payload_digest: 'digest-1', ready: true, confirmation_state: 'pending' }],
    })
    mocks.workspaceMessage.mockResolvedValue(undefined)
    mocks.workspaceAnswer.mockResolvedValue(undefined)
    mocks.closeWorkspace.mockResolvedValue(undefined)
    mocks.workspaceCommit.mockResolvedValue({ mode_id: 'profile', status: 'committed', project: { id: 'project-new', name: '新画像', platform: 'wechat' } })
    mocks.workspaceCheckpoint.mockResolvedValue({ id: 'checkpoint-1', payload_digest: 'digest-1' })
    mocks.taskGet.mockResolvedValue({ id: 'video-task-1', type: 'video-edit', prompt: '剪辑访谈', status: 'completed', project_id: 'video-project', execution_profile: 'effective', billing_price_credits: 1600, created_at: '' })
    mocks.taskFiles.mockResolvedValue([
      { id: 'final-video', task_id: 'video-task-1', state: 'delivered', role: 'final_video', delivery_role: 'final_video', file_name: 'final.mp4', mime_type: 'video/mp4', file_size: 1024, url: '', preview_url: 'https://assets.example/final.mp4', created_at: '', is_deliverable: true },
      { id: 'edit-list', task_id: 'video-task-1', state: 'delivered', role: 'artifact', delivery_role: 'edit_decision_list', file_name: 'edit-decision-list.json', mime_type: 'application/json', file_size: 10, url: '', created_at: '', is_deliverable: true },
      { id: 'strategy', task_id: 'video-task-1', state: 'delivered', role: 'artifact', delivery_role: 'edit_strategy', file_name: 'edit-strategy.md', mime_type: 'text/markdown', file_size: 10, url: '', created_at: '', is_deliverable: true },
    ])
    mocks.agentProfiles.mockResolvedValue([{ id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-flash', description: '日常创作', min_tier: 'free', available: true }])
    mocks.imageCapabilities.mockResolvedValue({ tier: 'free', default_capability: 'standard', items: [{ key: 'standard', display_name: '标准图像', description: '适合日常配图', enabled: true, price_available: true, price_credits: 300 }] })
    mocks.platformConfigs.mockResolvedValue([{ id: 'wechat', supported_image_ratios: ['16:9', '1:1'] }])
    mocks.userMediaList.mockResolvedValue({ items: [], total: 0, offset: 0, limit: 30 })
  })

  const fivePersonas = [
    activePersona('draft-ready', '已保存草稿', { profile: { schema_version: 1, status: 'draft', initialization_status: 'ready', version: 2, dimensions: emptyDimensions(), analysis_limits: [], follow_up_questions: [] } }),
    activePersona('draft-empty', '空画像', { profile: undefined }),
    activePersona('draft-zero', '未初始化画像', { profile: { schema_version: 1, status: 'draft', initialization_status: 'not_started', version: 0, dimensions: emptyDimensions(), analysis_limits: [], follow_up_questions: [] } }),
    activePersona('confirmed', '已确认画像', { profile: { schema_version: 1, status: 'confirmed', initialization_status: 'ready', version: 1, dimensions: emptyDimensions(), analysis_limits: [], follow_up_questions: [] } }),
    activePersona('missing-bootstrap', '新增人设'),
  ]

  function bootstrapFivePersonas() {
    setBootstrap({ defaultModeId: 'creation', modes: [
      { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
      { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
      { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
    ], projects: [
      { id: 'confirmed', name: '旧名称', platform: 'seednote', profileRevision: 1, profileReady: true, works: [] },
      { id: 'draft-zero', name: '未初始化画像', platform: 'wechat', profileRevision: 0, profileReady: false, works: [] },
      { id: 'draft-empty', name: '空画像', platform: 'wechat', profileRevision: 0, profileReady: false, works: [] },
      { id: 'draft-ready', name: '已保存草稿', platform: 'wechat', profileRevision: 2, profileReady: false, works: [] },
      { id: 'archived', name: '已归档', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] },
      { id: 'unauthorized', name: '无权访问', platform: 'seednote', profileRevision: 1, profileReady: true, works: [] },
    ] })
    mocks.projectsList.mockResolvedValue(fivePersonas)
  }

  it.each(fivePersonas.map(({ id, name }) => ({ id, name })))('selects and sends active persona $id regardless of profile readiness', async ({ id, name }) => {
    bootstrapFivePersonas()
    renderPage()

    const rail = await screen.findByRole('region', { name: '首页人设选择' })
    await waitFor(() => expect(within(rail).getByTestId('persona-rail-header')).toHaveTextContent('5 个可用人设'))
    expect(within(rail).getAllByRole('button', { name: /^选择人设：/ }).map((button) => button.getAttribute('aria-label'))).toEqual([
      '选择人设：已保存草稿', '选择人设：空画像', '选择人设：未初始化画像', '选择人设：已确认画像', '选择人设：新增人设',
    ])
    expect(within(rail).getByRole('button', { name: '选择人设：已保存草稿' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(within(rail).getByRole('button', { name: `选择人设：${name}` }))
    expect(within(screen.getByRole('region', { name: '当前人设' })).getByRole('heading')).toHaveTextContent(name)
    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent(name)
    expect(within(rail).getByRole('button', { name: `选择人设：${name}` })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '开始创作' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'creation', project_id: id }, expect.any(String)))
    expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'creation', '开始创作\n\n创作类型：wechat-article')
    expect(mocks.projectsList).toHaveBeenCalledWith({ status: 'active' })
  })

  it('uses an active persona when every bootstrap profile is unready', async () => {
    bootstrapFivePersonas()
    const bootstrap: AssistantBootstrap = await mocks.bootstrap()
    setBootstrap({ ...bootstrap, defaultModeId: 'profile', projects: bootstrap.projects.filter((project) => !project.profileReady) })
    mocks.projectsList.mockResolvedValue([fivePersonas[0]])
    renderPage()
    expect(await screen.findByRole('button', { name: '选择人设：已保存草稿' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('当前模式：创作模式')
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled()
  })

  it.each(['pending', 'failed'] as const)('does not admit bootstrap-only personas while the active list is $0', async (status) => {
    bootstrapFivePersonas()
    mocks.projectsList.mockImplementation(() => status === 'pending' ? new Promise(() => {}) : Promise.reject(new Error('list failed')))
    renderPage({ modeId: 'creation', projectId: 'confirmed' })
    await screen.findByRole('region', { name: '首页人设选择' })
    if (status === 'failed') expect(await screen.findByRole('alert')).toHaveTextContent('人设暂时无法加载')
    expect(screen.queryByRole('button', { name: /^选择人设：/ })).not.toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '不能发送' } })
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    expect(mocks.createWorkspace).not.toHaveBeenCalled()
  })

  it('removes a stale selection after the active list refreshes', async () => {
    bootstrapFivePersonas()
    const { queryClient } = renderPage({ modeId: 'profile', projectId: 'confirmed' })
    await screen.findByRole('button', { name: '选择人设：已确认画像' })
    mocks.projectsList.mockResolvedValue([fivePersonas[0], fivePersonas[1]])
    await act(async () => { await queryClient.invalidateQueries({ queryKey: ['projects'] }) })
    expect(screen.queryByRole('button', { name: '选择人设：已确认画像' })).not.toBeInTheDocument()
    expect(screen.getByTestId('assistant-context-bar')).not.toHaveTextContent('已确认画像')
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '不要更新归档人设' } })
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    expect(mocks.createWorkspace).not.toHaveBeenCalled()
  })

  it('reconciles cached creation prerequisites with active-list archive and restore refreshes', async () => {
    bootstrapFivePersonas()
    const bootstrap = await mocks.bootstrap()
    setBootstrap({ ...bootstrap, modes: bootstrap.modes.map((mode: AssistantBootstrap['modes'][number]) => mode.id === 'creation' ? { ...mode, available: false, disabledReason: '先创建一个人设，之后即可澄清创作需求' } : mode) })
    mocks.projectsList.mockResolvedValue([])
    const { queryClient } = renderPage({ modeId: 'creation' })
    await screen.findByRole('button', { name: '创建新人设' })
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    mocks.projectsList.mockResolvedValue([fivePersonas[0]])
    await act(async () => { await queryClient.invalidateQueries({ queryKey: ['projects'] }) })
    fireEvent.click(screen.getByRole('button', { name: '选择人设：已保存草稿' }))
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '恢复后创作' } })
    expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled()
    mocks.projectsList.mockResolvedValue([])
    await act(async () => { await queryClient.invalidateQueries({ queryKey: ['projects'] }) })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled())
    expect(screen.queryByRole('button', { name: '选择人设：已保存草稿' })).not.toBeInTheDocument()
  })

  it('refreshes homepage membership after archiving and restoring on the personas page', async () => {
    bootstrapFivePersonas()
    let archived = false
    mocks.projectsList.mockImplementation(async ({ status }: { status: string }) => status === 'archived'
      ? archived ? [{ ...fivePersonas[0], status: 'archived' }] : []
      : archived ? [] : [fivePersonas[0]])
    mocks.projectArchive.mockImplementation(async () => { archived = true })
    mocks.projectRestore.mockImplementation(async () => { archived = false })
    const queryClient = createTestQueryClient()
    queryClient.setDefaultOptions({ queries: { retry: false, gcTime: Infinity, staleTime: 30_000 } })
    render(<QueryClientProvider client={queryClient}><MemoryRouter><nav><Link to="/">返回首页</Link><Link to="/projects">管理人设</Link></nav><Routes><Route path="/" element={<AssistantWorkspace />} /><Route path="/projects" element={<ProjectsPage />} /></Routes></MemoryRouter></QueryClientProvider>)

    await screen.findByRole('button', { name: '选择人设：已保存草稿' })
    fireEvent.click(screen.getByRole('link', { name: '管理人设' }))
    fireEvent.click(await screen.findByRole('button', { name: '归档人设：已保存草稿' }))
    await waitFor(() => expect(mocks.projectArchive).toHaveBeenCalledWith('draft-ready'))
    fireEvent.click(screen.getByRole('link', { name: '返回首页' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: '选择人设：已保存草稿' })).not.toBeInTheDocument())
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '已归档不能发送' } })
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()

    fireEvent.click(screen.getByRole('link', { name: '管理人设' }))
    fireEvent.click(await screen.findByRole('button', { name: '已归档' }))
    fireEvent.click(await screen.findByRole('button', { name: '取消归档人设：已保存草稿' }))
    await waitFor(() => expect(mocks.projectRestore).toHaveBeenCalledWith('draft-ready'))
    fireEvent.click(screen.getByRole('link', { name: '返回首页' }))
    expect(await screen.findByRole('button', { name: '选择人设：已保存草稿' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '恢复后继续创作' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'creation', project_id: 'draft-ready' }, expect.any(String)))
    expect(mocks.bootstrap).toHaveBeenCalledTimes(1)
  })

  it('keeps the existing workspace locked to its persona when the active list changes', async () => {
    bootstrapFivePersonas()
    sessionStorage.setItem('anban:assistant-workspace', JSON.stringify({
      workspace: { id: 'locked-workspace', mode_id: 'creation', project_id: 'draft-ready', session_id: 'locked-session', sessions: { creation: 'locked-session' }, runtime_status: 'ready', expires_at: '' },
      mode: 'creation', projectId: 'draft-ready',
    }))
    mocks.projectsList.mockResolvedValue([fivePersonas[3]])
    renderPage()
    await waitFor(() => expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('已保存草稿'))
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '继续当前对话' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledWith('locked-workspace', 'creation', '继续当前对话\n\n创作类型：wechat-article'))
    expect(mocks.createWorkspace).not.toHaveBeenCalled()
    expect(mocks.switchWorkspaceMode).not.toHaveBeenCalled()
  })

  it('starts creation for a newly saved persona when cached bootstrap still describes an empty homepage', async () => {
    setBootstrap({ defaultModeId: 'profile', modes: [
      { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
      { id: 'creation', displayName: '创作模式', description: '制定计划', available: false, disabledReason: '先创建一个人设，之后即可澄清创作需求' },
    ], projects: [] })
    mocks.projectsList.mockResolvedValue([fivePersonas[0]])
    renderPage()
    await screen.findByRole('button', { name: '选择人设：已保存草稿' })
    await waitFor(() => expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('当前模式：创作模式'))
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '使用新增人设' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'creation', project_id: 'draft-ready' }, expect.any(String)))
  })

  it('can adjust a saved unready persona after starting a new profile', async () => {
    bootstrapFivePersonas()
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: '创建新人设' }))
    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('当前模式：人设模式')
    fireEvent.click(screen.getByRole('button', { name: '调整人设：已保存草稿' }))
    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('当前模式：人设模式')
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '更新已保存内容' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'profile', project_id: 'draft-ready' }, expect.any(String)))
  })

  it.each(['membership changes', 'failed refresh recovers'] as const)('keeps explicit profile adjustment when the active list $0', async (refresh) => {
    bootstrapFivePersonas()
    const { queryClient } = renderPage()
    fireEvent.click(await screen.findByRole('button', { name: '调整人设：已保存草稿' }))
    const composer = screen.getByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '调整人设的表达风格' } })
    if (refresh === 'failed refresh recovers') {
      mocks.projectsList.mockRejectedValue(new Error('refresh failed'))
      await act(async () => { await queryClient.invalidateQueries({ queryKey: ['projects'] }) })
      expect(await screen.findByRole('alert')).toHaveTextContent('人设暂时无法加载')
      expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    }
    mocks.projectsList.mockResolvedValue([fivePersonas[0], fivePersonas[1]])
    await act(async () => { await queryClient.invalidateQueries({ queryKey: ['projects'] }) })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled())
    expect(composer).toHaveValue('调整人设的表达风格')
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'profile', project_id: 'draft-ready' }, expect.any(String)))
    expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'profile', '调整人设的表达风格')
  })

  it('allows drafting while bootstrap is pending and enables sending when it is ready', async () => {
    let resolveBootstrap!: (value: Awaited<ReturnType<typeof mocks.bootstrap>>) => void
    mocks.bootstrap.mockReturnValue(new Promise((resolve) => { resolveBootstrap = resolve }))
    renderPage()

    const composer = screen.getByRole('textbox', { name: '发送消息' })
    expect(composer).toBeEnabled()
    fireEvent.change(composer, { target: { value: '先写下需求' } })
    expect(composer).toHaveValue('先写下需求')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()

    resolveBootstrap({
      defaultModeId: 'profile',
      modes: [{ id: 'profile', displayName: '画像模式', description: '建立画像', available: true }],
      projects: [],
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled())
    expect(composer).toHaveValue('先写下需求')
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'profile' }, expect.any(String)))
  })

  it('retries bootstrap after an initial failure and enables sending when it recovers', async () => {
    mocks.bootstrap.mockRejectedValueOnce(new Error('temporary failure'))
    renderPage()

    const composer = screen.getByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '失败时保留这条需求' } })
    expect(await screen.findByRole('alert')).toHaveTextContent('首页暂时无法加载')
    expect(composer).toHaveValue('失败时保留这条需求')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()

    mocks.bootstrap.mockResolvedValueOnce({
      defaultModeId: 'profile',
      modes: [{ id: 'profile', displayName: '画像模式', description: '建立画像', available: true }],
      projects: [],
    })
    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled())
    expect(composer).toHaveValue('失败时保留这条需求')
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'profile' }, expect.any(String)))
  })

  it('keeps creation unavailable until a persona exists and offers the create card', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: false, disabledReason: '先创建一个人设，之后即可澄清创作需求' },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: false, disabledReason: '先创建一个人设并完成一件作品，之后即可复盘' },
      ],
      projects: [],
    })
    renderPage({ modeId: 'creation' })

    expect(await screen.findByText('先创建一个人设，之后即可澄清创作需求')).toHaveAttribute('role', 'status')
    expect(screen.getByRole('button', { name: '创建新人设' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '创作模式' })).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toHaveAttribute('placeholder', '先完成新人设创建后即可输入消息')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: '创建新人设' }))
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled()
    expect(screen.queryByText('先创建一个人设，之后即可澄清创作需求')).not.toBeInTheDocument()
  })

  it('keeps modes disabled when the assistant runtime is unavailable', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: false, disabledReason: '助手运行环境暂不可用，请稍后重试' },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: false, disabledReason: '助手运行环境暂不可用，请稍后重试' },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: false, disabledReason: '助手运行环境暂不可用，请稍后重试' },
      ],
      projects: [{ id: 'active-persona', name: '可用人设', platform: 'wechat', profileRevision: 0, profileReady: false, works: [] }],
    })
    renderPage({ modeId: 'creation' })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeDisabled())
    expect(screen.queryByRole('button', { name: '创作模式' })).not.toBeInTheDocument()
  })

  it('selects the first active persona by default for creation', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [
        { id: 'project-1', name: '画像一', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] },
        { id: 'project-2', name: '画像二', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] },
      ],
    })
    renderPage()

    expect(await screen.findByRole('button', { name: '选择人设：画像一' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled()
  })

  it('does not append the redundant seednote creation type to the message', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-seednote', name: '种草人设', platform: 'seednote', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.projectsList.mockResolvedValue([{ id: 'project-seednote', name: '种草人设', platform: 'seednote', avatar_url: '', status: 'active' }])
    renderPage({ modeId: 'creation', projectId: 'project-seednote' })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '写一篇种草笔记' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'creation', '写一篇种草笔记'))
    expect(screen.queryByText('创作类型：seednote')).not.toBeInTheDocument()
  })

  it('uses the selected persona avatar for user messages', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-seednote', name: '种草人设', platform: 'seednote', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.projectsList.mockResolvedValue([{ id: 'project-seednote', name: '种草人设', platform: 'seednote', avatar_url: 'https://example.com/persona.png', status: 'active' }])
    renderPage({ modeId: 'creation', projectId: 'project-seednote' })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '写一篇种草笔记' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findByRole('img', { name: '当前人设头像' })).toHaveAttribute('src', 'https://example.com/persona.png')
  })

  it('blocks creation when the selected persona has no supported creation type', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-unsupported', name: '未支持项目', platform: 'local-life', profileRevision: 1, profileReady: true, works: [] }],
    })
    renderPage()

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toHaveAttribute('placeholder', '此类型请进入创作页使用，或调整创作类型'))
    expect(composer).toBeDisabled()
    expect(composer).toHaveAttribute('placeholder', '此类型请进入创作页使用，或调整创作类型')
    expect(within(screen.getByTestId('assistant-context-bar')).getByRole('status')).toHaveTextContent('此类型请进入创作页使用，或调整创作类型')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '前往创作页' }))
    expect(screen.getByTestId('location')).toHaveTextContent('/tasks/new?project_id=project-unsupported')
  })

  function setupUnconfiguredPersona() {
    const persona = activePersona('needs-types', '待设置人设', { platform: '' as Project['platform'], agent_ids: [] })
    setBootstrap({ defaultModeId: 'creation', modes: [
      { id: 'creation', displayName: '创作模式', description: '创作', available: true },
    ], projects: [{ ...persona, profileReady: true, profileRevision: 1, works: [] }] })
    mocks.projectsList.mockResolvedValue([persona])
    return persona
  }

  it('saves creation types inline, retains the brief and can create after a refresh', async () => {
    const persona = setupUnconfiguredPersona()
    const saved = { ...persona, platform: 'wechat' as const, agent_ids: ['wechat-picture'] }
    mocks.projectUpdate.mockImplementation(async () => {
      mocks.projectsList.mockResolvedValue([saved])
      return saved
    })
    const view = renderPage({ modeId: 'creation', projectId: persona.id, initialMessage: '做一组茶知识图片' })
    fireEvent.click(await screen.findByRole('button', { name: '选择创作类型' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByRole('button', { name: '保存并继续创作' })).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '公众号贴图' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '保存并继续创作' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(mocks.projectUpdate).toHaveBeenCalledWith(persona.id, { agent_ids: ['wechat-picture'], platform: 'wechat' })
    expect(screen.getByRole('textbox', { name: '发送消息' })).toHaveValue('做一组茶知识图片')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled()
    expect(screen.getByRole('combobox', { name: '本次创作类型' })).toHaveValue('wechat-picture')
    expect(mocks.createWorkspace).not.toHaveBeenCalled()
    expect(mocks.workspaceMessage).not.toHaveBeenCalled()
    view.unmount()
    renderPage()
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled())
    expect(screen.getByRole('textbox', { name: '发送消息' })).toHaveValue('做一组茶知识图片')
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'creation', '做一组茶知识图片\n\n创作类型：wechat-picture'))
  })

  it('does not modify settings when cancelled and keeps a failed save retryable', async () => {
    const persona = setupUnconfiguredPersona()
    mocks.projectUpdate.mockRejectedValue(new Error('保存失败'))
    renderPage({ modeId: 'creation', projectId: persona.id, initialMessage: '保留这段需求' })
    fireEvent.click(await screen.findByRole('button', { name: '选择创作类型' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }))
    expect(mocks.projectUpdate).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '选择创作类型' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '视频生成' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '保存并继续创作' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toBeInTheDocument()
    expect(within(screen.getByRole('dialog')).getByRole('button', { name: '保存并继续创作' })).toBeEnabled()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }))
    expect(screen.getByRole('textbox', { name: '发送消息' })).toHaveValue('保留这段需求')
    expect(mocks.createWorkspace).not.toHaveBeenCalled()
  })

  it('allows selecting between the configured article and picture types before sending', async () => {
    const persona = setupUnconfiguredPersona()
    mocks.projectsList.mockResolvedValue([{ ...persona, platform: 'wechat', agent_ids: ['wechat-article', 'wechat-picture'] }])
    renderPage({ modeId: 'creation', projectId: persona.id, initialMessage: '图片创作' })
    fireEvent.change(await screen.findByRole('combobox', { name: '本次创作类型' }), { target: { value: 'wechat-picture' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'creation', '图片创作\n\n创作类型：wechat-picture'))
  })

  it('only exposes creation parameters in creation mode', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '我的人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    renderPage({ modeId: 'profile' })

    await screen.findByRole('button', { name: '选择人设：我的人设' })
    expect(screen.queryByRole('button', { name: /^选择参数：/ })).not.toBeInTheDocument()
  })

  it('only exposes attachment upload in creation mode', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '我的人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    renderPage({ modeId: 'profile' })

    await screen.findByRole('button', { name: '选择人设：我的人设' })
    expect(screen.queryByLabelText('选择附件文件')).not.toBeInTheDocument()
  })

  it('blocks input until a work is selected for review', async () => {
    setBootstrap({
      defaultModeId: 'review',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '画像一', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.works.mockResolvedValue({ items: [
      { id: 'work-1', title: '作品一', type: 'article', createdAt: '2026-10-22T00:00:00Z' },
      { id: 'work-2', title: '作品二', type: 'article', createdAt: '2026-10-21T00:00:00Z' },
    ], has_more: false })
    renderPage({ modeId: 'review' })
    const composer = screen.getByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeDisabled())
    expect(composer).toHaveAttribute('placeholder', '先选择作品后即可输入消息')

    const workSelect = await screen.findByRole('combobox', { name: '选择作品' })
    await screen.findByRole('option', { name: '作品一' })
    fireEvent.change(workSelect, { target: { value: 'work-1' } })
    await waitFor(() => expect(composer).toBeEnabled())
  })

  it('prevents duplicate commands from two immediate sends', async () => {
    let resolveMessage!: () => void
    mocks.workspaceMessage.mockReturnValue(new Promise<void>((resolve) => { resolveMessage = resolve }))
    renderPage()

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '只发送一次' } })
    const send = screen.getByRole('button', { name: '发送消息' })
    fireEvent.click(send)
    fireEvent.click(send)

    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(1))
    expect(mocks.workspaceMessage).toHaveBeenCalledTimes(1)
    resolveMessage()
  })

  it('uses the DSH-style inline composer and removes setup controls after chat starts', async () => {
    renderPage()
    expect(screen.getByRole('textbox', { name: '发送消息' }).closest('[data-slot="input-group"]')).not.toHaveClass('before:bg-gradient-to-b')
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '开始聊天' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findByText('收到，我开始处理。')).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: '助手模式' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '创作模式' })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: '选择画像' })).not.toBeInTheDocument()
    expect(screen.getByTestId('assistant-context-bar')).toHaveAttribute('data-phase', 'locked')
    expect(screen.getByTestId('assistant-context-bar')).not.toHaveClass('border-b', 'border-border/50')
    expect(screen.queryByRole('button', { name: '人设配置' })).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' }).closest('[data-slot="input-group"]')).toHaveClass('before:bg-gradient-to-b')
    expect(screen.getByRole('button', { name: '发送消息' }).parentElement).toHaveAttribute('data-align', 'inline-end')
  })

  it('keeps the draft and shows a useful message for an interactive busy conflict', async () => {
    const busy = Object.assign(new Error('Request failed with status code 409'), {
      response: { status: 409, data: { error_code: 'interactive_session_busy' } },
    })
    mocks.workspaceMessage.mockRejectedValueOnce(busy)
    renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '等待上一条完成' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findByText('助手正在处理上一条消息，输入已保留，请稍后重试。')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toHaveValue('等待上一条完成')
    expect(within(screen.getByTestId('assistant-conversation')).queryByText('等待上一条完成')).not.toBeInTheDocument()
    expect(screen.queryByText('Request failed with status code 409')).not.toBeInTheDocument()
  })

  it('distinguishes workspace mode conflicts from generic conversation conflicts', async () => {
    const conflict = Object.assign(new Error('Request failed with status code 409'), {
      response: { status: 409, data: { error_code: 'assistant_workspace_mode_mismatch' } },
    })
    mocks.workspaceMessage.mockRejectedValueOnce(conflict)
    renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '保留这条输入' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findByText('当前对话模式已变化，输入已保留，请重新建立对话后重试。')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toHaveValue('保留这条输入')
    expect(screen.queryByText('对话状态刚刚发生变化，输入已保留，请稍后重试。')).not.toBeInTheDocument()
  })

  it('explains profile revision conflicts while preserving the staged draft', async () => {
    const conflict = Object.assign(new Error('Request failed with status code 409'), {
      response: { status: 409, data: { error_code: 'project_profile_revision_conflict' } },
    })
    mocks.workspaceCommit.mockRejectedValueOnce(conflict)
    renderPage()
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '建立画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    fireEvent.click(await screen.findByRole('button', { name: '保存人设' }))

    expect(await screen.findByText('人设已在其他窗口更新，输入已保留，请刷新后核对再继续。')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '保存人设' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存人设' })).toBeInTheDocument()
  })

  it('restores a deferred message when the runtime reports an asynchronous send failure', async () => {
    mocks.streamEvents = [{
      event: 'session.error',
      id: 'event-session-error',
      data: JSON.stringify({ status: 'runtime_failed', retry_text: '保留这条延迟消息' }),
    }]
    renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '保留这条延迟消息' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findByText('消息发送失败，输入已保留，请重试。')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('textbox', { name: '发送消息' })).toHaveValue('保留这条延迟消息'))
  })

  it('keeps the send action disabled while the runtime turn is active', async () => {
    mocks.streamEvents = [{ event: 'turn.start', id: 'event-3', data: JSON.stringify({ session_id: 'session-1' }) }]
    renderPage()
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '开始处理' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findAllByText('AI 正在处理…')).toHaveLength(1)
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled())
  })

  it('hides mode controls and omits the duplicate page title', async () => {
    renderPage()
    await screen.findByRole('textbox', { name: '发送消息' })
    expect(screen.queryByText('首页', { exact: true })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '画像模式' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '创作模式' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '复盘模式' })).not.toBeInTheDocument()
  })

  it('keeps the creation type selector out of the composer', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '寻茗好茶', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    renderPage({ modeId: 'creation', projectId: 'project-1' })

    await screen.findByRole('textbox', { name: '发送消息' })
    expect(screen.queryByRole('combobox', { name: '创作类型' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^选择参数：/ })).toBeInTheDocument()
  })

  it('places model and parameter context in the composer without a persona picker', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '寻茗好茶', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    renderPage({ modeId: 'creation', projectId: 'project-1' })

    const contextBar = await screen.findByTestId('assistant-context-bar')
    await waitFor(() => expect(contextBar).toHaveTextContent('寻茗好茶'))
    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('当前模式：创作模式')
    const parameters = await screen.findByRole('button', { name: /^选择参数：/ })
    expect(parameters).toHaveTextContent('性价比 / 智能适配 / 标准图像')
    expect(parameters).not.toHaveTextContent('选择参数')
    fireEvent.click(parameters)
    const parameterDialog = await screen.findByRole('dialog', { name: '选择参数' })
    const imageCapabilities = within(parameterDialog).getByRole('group', { name: '图像能力' })
    expect(imageCapabilities).toBeInTheDocument()
    expect(imageCapabilities).toHaveClass('grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]')
    expect(within(parameterDialog).getByRole('button', { name: /^标准图像/ })).toBeInTheDocument()
    expect(within(parameterDialog).getByRole('button', { name: '16:9' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '人设配置' })).not.toBeInTheDocument()
  })

  it('labels a new persona conversation as profile mode', async () => {
    renderPage()

    const contextBar = await screen.findByTestId('assistant-context-bar')
    expect(contextBar).toHaveTextContent('当前模式：人设模式')
    await screen.findByText('还没有可用人设')
    fireEvent.click(screen.getByRole('button', { name: '创建新人设' }))
    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('当前模式：人设模式')
  })

  it('exposes the current WeChat creation type beside persona settings', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '寻茗好茶', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    renderPage({ modeId: 'creation', projectId: 'project-1' })

    await screen.findByRole('textbox', { name: '发送消息' })
    expect(screen.queryByRole('combobox', { name: '创作类型' })).not.toBeInTheDocument()
    expect(await screen.findByRole('combobox', { name: '本次创作类型' })).toHaveValue('wechat-article')
  })

  it('keeps the empty and active states on the root route', async () => {
    renderPage()
    expect(await screen.findByRole('heading', { name: '神秘人' })).toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '帮我建立一个人设' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'profile' }, expect.any(String)))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'profile', '帮我建立一个人设'))
    expect(screen.getByTestId('location')).toHaveTextContent('/')
    expect(await screen.findByText('收到，我开始处理。')).toBeInTheDocument()
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
  })

  it('renders question cards in the order they arrive between assistant messages', async () => {
    mocks.streamEvents = [
      { event: 'question.opened', id: 'event-question-1', data: JSON.stringify({ session_id: 'session-1', call_id: 'call-1', questions: [{ id: 'q1', question: '先确定作品主题？', options: [{ label: '主题一' }] }] }) },
      { event: 'assistant.message', id: 'event-assistant-2', data: JSON.stringify({ session_id: 'session-1', text: '根据你的回答，我接着问受众。' }) },
      { event: 'question.opened', id: 'event-question-2', data: JSON.stringify({ session_id: 'session-1', call_id: 'call-2', questions: [{ id: 'q2', question: '这篇作品面向谁？', options: [{ label: '新手' }] }] }) },
    ]
    renderPage()
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '开始创作' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    const conversation = await screen.findByTestId('assistant-conversation')
    await within(conversation).findByRole('heading', { name: '先确定作品主题？' })
    const entries = [
      within(conversation).getByText('收到，我开始处理。'),
      within(conversation).getByRole('heading', { name: '先确定作品主题？' }),
      within(conversation).getByText('根据你的回答，我接着问受众。'),
      within(conversation).getByRole('heading', { name: '这篇作品面向谁？' }),
    ]
    expect(entries[0].compareDocumentPosition(entries[1]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(entries[1].compareDocumentPosition(entries[2]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(entries[2].compareDocumentPosition(entries[3]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(conversation.parentElement).toHaveClass('max-w-6xl')
  })

  it('removes answered question cards from the home conversation', async () => {
    mocks.streamEvents = [
      { event: 'question.opened', id: 'event-question-1', data: JSON.stringify({ session_id: 'session-1', call_id: 'call-1', questions: [{ id: 'q1', question: '先确定作品主题？', options: [{ label: '主题一' }] }] }) },
    ]
    renderPage()
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '开始创作' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    const conversation = await screen.findByTestId('assistant-conversation')
    expect(await within(conversation).findByRole('heading', { name: '先确定作品主题？' })).toBeInTheDocument()
    fireEvent.click(within(conversation).getByRole('radio', { name: '主题一' }))
    fireEvent.click(within(conversation).getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(mocks.workspaceAnswer).toHaveBeenCalledWith('workspace-1', 'profile', 'call-1', [{ id: 'q1', selected: ['主题一'] }]))
    await waitFor(() => expect(within(conversation).queryByRole('heading', { name: '先确定作品主题？' })).not.toBeInTheDocument())
  })

  it('opens profile setup from the create persona card before starting a workspace', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: '创建新人设' }))
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '开始创作' } })
    expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled()
    expect(mocks.createWorkspace).not.toHaveBeenCalled()
    expect(screen.getByTestId('location')).toHaveTextContent('/')
  })

  it('configures and commits video-edit defaults from a profile draft', async () => {
    mocks.projectsList.mockResolvedValue([activePersona('project-new', '新画像', { platform: 'video-edit' })])
    mocks.bootstrap.mockReset()
    mocks.bootstrap
      .mockResolvedValueOnce({ defaultModeId: 'profile', modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ], projects: [{ id: 'project-new', name: '新画像', platform: 'video-edit', profileRevision: 1, profileReady: true, works: [] }] })
      .mockResolvedValue({ defaultModeId: 'profile', modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ], projects: [{ id: 'project-new', name: '新画像', platform: 'video-edit', profileRevision: 1, profileReady: true, video_edit_defaults: { preferences: { quality_tier: 'quality' } }, works: [] }] })
    mocks.workspaceCommit.mockResolvedValue({ mode_id: 'profile', status: 'committed', project: { id: 'project-new', name: '新画像', platform: 'video-edit', video_edit_defaults: { preferences: { quality_tier: 'quality' } } } })
    renderPage({ modeId: 'profile', projectId: 'project-new' })
    await screen.findByRole('combobox', { name: '默认质量档位' })
    fireEvent.change(await screen.findByRole('combobox', { name: '默认质量档位' }), { target: { value: 'quality' } })
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '建立画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '保存人设' })).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '继续深入访谈' })).toBeEnabled()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '保存人设' }))
    await waitFor(() => expect(mocks.workspaceCommit).toHaveBeenCalledWith('workspace-1', 'profile', expect.objectContaining({
      confirmation_token: 'token-1',
      idempotency_key: expect.any(String),
      video_edit_defaults: { preferences: { quality_tier: 'quality' } },
    })))
    expect(screen.queryByRole('combobox', { name: '默认质量档位' })).not.toBeInTheDocument()
    expect(mocks.bootstrap).toHaveBeenCalledTimes(2)
    await waitFor(() => expect(mocks.projectsList).toHaveBeenCalledTimes(2))
  })

  it('reuses the workspace creation idempotency key after a failed response', async () => {
    mocks.createWorkspace
      .mockRejectedValueOnce(new Error('temporary network failure'))
      .mockResolvedValueOnce({ id: 'workspace-1', mode_id: 'profile', session_id: 'session-1', sessions: { profile: 'session-1' }, runtime_status: 'starting', expires_at: '' })
    renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '第一次发送' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(1))
    expect(composer).toHaveValue('第一次发送')
    fireEvent.change(composer, { target: { value: '重试发送' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(2))
    const firstKey = mocks.createWorkspace.mock.calls[0][1]
    const secondKey = mocks.createWorkspace.mock.calls[1][1]
    expect(firstKey).toBeTruthy()
    expect(secondKey).toBe(firstKey)
  })

  it('keeps the unsent message and retries the durable interaction after expiry', async () => {
    const expired = Object.assign(new Error('expired'), { response: { data: { error_code: 'assistant_workspace_expired' } } })
    mocks.workspaceMessage.mockRejectedValueOnce(expired).mockResolvedValueOnce(undefined)
    renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '继续完善画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(composer).toHaveValue('继续完善画像'))
    expect(screen.queryByText(/对话已过期/)).not.toBeInTheDocument()
    expect(screen.queryByText(/SSE connection failed/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledTimes(2))
    expect(mocks.workspaceMessage).toHaveBeenLastCalledWith('workspace-1', 'profile', '继续完善画像')
  })

  it('applies a recovery snapshot returned with a failed command', async () => {
    const snapshot = {
      interaction_id: 'workspace-1', mode_id: 'profile', lifecycle: 'reconnecting', generation: 2,
      runtime_status: 'running', expires_at: '', last_activity_at: '', available_actions: ['reload_snapshot'], drafts: [],
    }
    const reconnecting = Object.assign(new Error('reconnecting'), { response: { status: 409, data: { error_code: 'assistant_interaction_reconnecting', data: snapshot } } })
    mocks.workspaceMessage.mockRejectedValueOnce(reconnecting)
    renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '恢复这个需求' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findByText('正在恢复连接')).toBeInTheDocument()
    expect(screen.queryByText(/session not found/i)).not.toBeInTheDocument()
  })

  it('silently returns to the home state when the event stream reports an expired session', async () => {
    mocks.streamEvents = [{ event: 'session.expired', id: 'event-session-expired', data: JSON.stringify({ status: 'expired' }) }]
    renderPage()

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '保留这段需求' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledTimes(1))
    expect(screen.queryByText(/对话已过期/)).not.toBeInTheDocument()
    expect(screen.queryByText(/SSE connection failed/)).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getAllByRole('textbox', { name: '发送消息' }).map((item) => (item as HTMLTextAreaElement).value)).toContain('保留这段需求'))
  })

  it('abandons the current interaction before starting over from recovery state', async () => {
    mocks.streamEvents = [{ event: 'session.expired', id: 'event-session-expired', data: JSON.stringify({ status: 'expired' }) }]
    renderPage()

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '这段内容需要保留到恢复' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await screen.findByRole('button', { name: '重新开始' })

    fireEvent.click(screen.getByRole('button', { name: '重新开始' }))
    await waitFor(() => expect(mocks.closeWorkspace).toHaveBeenCalledWith('workspace-1'))
    expect(screen.queryByRole('button', { name: '重新开始' })).not.toBeInTheDocument()
  })

  it('restores the conversation and confirmation card after a page refresh', async () => {
    const firstRender = renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '刷新后继续保留这段需求' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    expect(await screen.findByText('刷新后继续保留这段需求')).toBeInTheDocument()
    expect(await screen.findByRole('region', { name: '保存人设' })).toBeInTheDocument()
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(1))
    firstRender.unmount()

    const secondRender = renderPage()
    expect(await screen.findByText('刷新后继续保留这段需求')).toBeInTheDocument()
    expect(screen.getByText('收到，我开始处理。')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '保存人设' })).toBeInTheDocument()
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(1))
    secondRender.unmount()
  })

  it('renders the homepage persona rail with a create card and selects the first active persona', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [
        { id: 'project-1', name: '第一个人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] },
        { id: 'project-2', name: '第二个人设', platform: 'seednote', profileRevision: 1, profileReady: true, works: [] },
      ],
    })
    mocks.projectsList.mockResolvedValue([
      { id: 'project-1', name: '第一个人设', platform: 'wechat', avatar_url: 'https://example.com/one.png', description: '公众号内容创作', status: 'active' },
      { id: 'project-2', name: '第二个人设', platform: 'seednote', avatar_url: '', description: '种草内容创作', status: 'active' },
    ])
    renderPage()

    const rail = await screen.findByRole('region', { name: '首页人设选择' })
    const railHeader = within(rail).getByTestId('persona-rail-header')
    expect(railHeader).toHaveTextContent('选择人设2 个可用人设')
    expect(railHeader.firstElementChild).toHaveTextContent('选择人设')
    expect(railHeader.lastElementChild).toHaveTextContent('2 个可用人设')
    expect(within(rail).getByTestId('persona-card-scroll')).toHaveClass('min-h-[5.25rem]', 'max-h-[14.125rem]', 'grid-cols-2', 'min-[700px]:grid-cols-3', 'min-[1600px]:grid-cols-4', 'min-[1900px]:grid-cols-5', 'min-[2200px]:grid-cols-6', 'auto-rows-[5.25rem]')
    expect(within(rail).getByRole('button', { name: '创建新人设' })).toBeInTheDocument()
    expect(within(rail).getByRole('button', { name: '选择人设：第一个人设' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(rail).getByRole('button', { name: '选择人设：第二个人设' })).toHaveAttribute('aria-pressed', 'false')
    expect(within(rail).getByRole('img', { name: '公众号文章' })).toBeInTheDocument()
    expect(within(rail).getByRole('img', { name: '种草笔记' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '画像模式' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '创作模式' })).not.toBeInTheDocument()
  })

  it('uses a platform introduction when a persona has no custom description', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-seednote', name: '种草人设', platform: 'seednote', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.projectsList.mockResolvedValue([{ id: 'project-seednote', name: '种草人设', platform: 'seednote', avatar_url: '', status: 'active' }])

    const rail = await (async () => {
      renderPage()
      return screen.findByRole('region', { name: '首页人设选择' })
    })()

    expect(within(rail).getByText('笔记文案与多页内容')).toBeInTheDocument()
    expect(within(rail).queryByText('已准备好开始创作')).not.toBeInTheDocument()
  })

  it('hides the persona rail fade after scrolling to the last row', async () => {
    renderPage()
    const scrollRegion = await screen.findByTestId('persona-card-scroll')
    Object.defineProperties(scrollRegion, {
      clientHeight: { configurable: true, value: 200 },
      scrollHeight: { configurable: true, value: 300 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })
    fireEvent.resize(window)

    expect(screen.queryByTestId('persona-card-fade-top')).not.toBeInTheDocument()
    expect(screen.getByTestId('persona-card-fade-bottom')).toHaveClass('bg-gradient-to-t')

    Object.defineProperty(scrollRegion, 'scrollTop', { configurable: true, writable: true, value: 100 })
    fireEvent.scroll(scrollRegion)

    expect(screen.getByTestId('persona-card-fade-top')).toHaveClass('bg-gradient-to-b')
    expect(screen.queryByTestId('persona-card-fade-bottom')).not.toBeInTheDocument()

    fireEvent.scroll(scrollRegion, { target: { scrollTop: 40 } })
    expect(screen.getByTestId('persona-card-fade-top')).toHaveClass('bg-gradient-to-b')
    expect(screen.getByTestId('persona-card-fade-bottom')).toHaveClass('bg-gradient-to-t')

    Object.defineProperty(scrollRegion, 'clientHeight', { configurable: true, value: 300 })
    fireEvent.resize(window)
    expect(screen.getByTestId('persona-card-fade-top')).toHaveClass('bg-gradient-to-b')
    expect(screen.queryByTestId('persona-card-fade-bottom')).not.toBeInTheDocument()

    Object.defineProperty(scrollRegion, 'scrollTop', { configurable: true, writable: true, value: 0 })
    fireEvent.scroll(scrollRegion)
    expect(screen.queryByTestId('persona-card-fade-top')).not.toBeInTheDocument()
  })

  it('exposes persona actions for works and profile updates', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '第一个人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.projectsList.mockResolvedValue([{ id: 'project-1', name: '第一个人设', platform: 'wechat', avatar_url: '', description: '公众号内容创作', status: 'active' }])
    renderPage()

    const rail = await screen.findByRole('region', { name: '首页人设选择' })
    fireEvent.click(within(rail).getByRole('button', { name: '查看作品：第一个人设' }))
    expect(await screen.findByTestId('location')).toHaveTextContent('/tasks?project_id=project-1')
  })

  it('shows the mysterious-person guidance when no active persona exists', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: false },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: false },
      ],
      projects: [],
    })
    mocks.projectsList.mockResolvedValue([])
    renderPage()

    expect(await screen.findByText('为了创建更好的作品，需要先创建您的人设')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '创建新人设' })).toBeInTheDocument()
  })

  it('collects review context before the workspace starts', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '我的画像', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.works.mockResolvedValue({ items: [{ id: 'work-1', title: '已完成作品', type: 'article', createdAt: '2026-10-22T00:00:00Z' }], has_more: false })
    renderPage({ modeId: 'review' })
    await waitFor(() => expect(screen.getByRole('combobox', { name: '选择人设' })).toBeEnabled())
    fireEvent.change(screen.getByRole('combobox', { name: '选择人设' }), { target: { value: 'project-1' } })
    await waitFor(() => expect(screen.getByRole('combobox', { name: '选择作品' })).toBeEnabled())
    fireEvent.change(screen.getByRole('combobox', { name: '选择作品' }), { target: { value: 'work-1' } })
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '复盘这篇作品' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'review', project_id: 'project-1', content_id: 'work-1' }, expect.any(String)))
    expect(mocks.switchWorkspaceMode).not.toHaveBeenCalled()
  })

  it('previews the selected video-edit work and exposes its delivered edit artifacts in review mode', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'video-project', name: '视频项目', platform: 'video-edit', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.works.mockResolvedValue({ items: [{ id: 'video-task-1', title: '访谈成片', type: 'video-edit', createdAt: '2026-10-22T00:00:00Z' }], has_more: false })
    renderPage({ modeId: 'review', projectId: 'video-project' })

    expect(await screen.findByRole('heading', { name: '你的作品' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '查看作品' })).toBeInTheDocument()
    expect(screen.getByText(/剪辑清单 · edit-decision-list\.json/)).toBeInTheDocument()
    expect(screen.getByText(/剪辑策略 · edit-strategy\.md/)).toBeInTheDocument()
    await waitFor(() => {
      expect(mocks.taskGet).toHaveBeenCalledWith('video-task-1')
      expect(mocks.taskFiles).toHaveBeenCalledWith('video-task-1')
    })
  })

  it('freezes selected video-edit type, uploaded assets, brief, and preferences into the creation checkpoint', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'video-project', name: '视频项目', platform: 'video-edit', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'creation', session_id: 'session-1', sessions: { creation: 'session-1' }, project_id: 'video-project', runtime_status: 'starting', expires_at: '' })
    uploadToOSSMock.mockResolvedValue({ uploadSessionId: 'upload-session-1', uploadId: 'upload-session-1', key: 'staging/video.mp4', previewUrl: '/preview.mp4', publicUrl: '/video.mp4', contentType: 'video/mp4', size: 42 })
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:assistant-video')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    renderPage({ modeId: 'creation', projectId: 'video-project' })
    fireEvent.change(await screen.findByRole('combobox', { name: '创作类型' }), { target: { value: 'video-edit' } })
    fireEvent.change(screen.getByRole('textbox', { name: '剪辑要求' }), { target: { value: '剪成 30 秒访谈短片' } })
    fireEvent.change(screen.getByRole('combobox', { name: '质量档位' }), { target: { value: 'quality' } })
    fireEvent.change(screen.getByRole('combobox', { name: '字幕' }), { target: { value: 'required' } })
    fireEvent.change(screen.getByLabelText('添加参考素材'), { target: { files: [new File(['video'], 'interview.mp4', { type: 'video/mp4' })] } })
    await waitFor(() => expect(uploadToOSSMock).toHaveBeenCalledWith(expect.objectContaining({ purpose: 'video_edit_asset' })))
    await screen.findByText('interview.mp4')
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '自动剪辑我的访谈素材' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'creation', expect.stringContaining('自动剪辑我的访谈素材')))
    fireEvent.click(await screen.findByRole('button', { name: '创作作品' }))

    await waitFor(() => expect(mocks.workspaceCheckpoint).toHaveBeenCalledWith('workspace-1', 'creation', expect.objectContaining({
      payload: expect.objectContaining({
        content_type: 'video-edit',
        assets: [{ type: 'video', upload_id: 'upload-session-1', file_name: 'interview.mp4', mime_type: 'video/mp4', file_size: 42 }],
        video_edit_preferences: { quality_tier: 'quality', subtitle_mode: 'required' },
        video_edit_brief: '剪成 30 秒访谈短片',
      }),
    })))
  })

  it('shows a creation confirmation card and opens the created task after approval', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '我的人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'creation', session_id: 'session-1', sessions: { creation: 'session-1' }, project_id: 'project-1', runtime_status: 'starting', expires_at: '' })
    mocks.workspaceCommit.mockResolvedValue({ mode_id: 'creation', status: 'committed', task: { id: 'task-created-1', project_id: 'project-1', status: 'pending' } })
    mocks.streamEvents = [{
      event: 'draft.ready',
      id: 'event-3',
      data: JSON.stringify({
        mode_id: 'creation',
        session_id: 'session-1',
        draft: {
          revision: 1,
          ready: true,
          payload: { plan: { goal: '发布春季新品', content_type: 'wechat-article', target_audience: '关注茶饮的年轻人' } },
        },
      }),
    }]
    renderPage({ modeId: 'creation', projectId: 'project-1' })

    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '帮我制定春季新品内容计划' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    const card = await screen.findByRole('region', { name: '确认创作' })
    expect(within(card).getByText('发布春季新品')).toBeInTheDocument()
    expect(within(card).getByText('公众号文章')).toBeInTheDocument()
    expect(within(card).getByRole('button', { name: '创作作品' })).toBeEnabled()
    expect(within(card).getByRole('button', { name: '继续讨论' })).toBeEnabled()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    fireEvent.click(within(card).getByRole('button', { name: '创作作品' }))

    await waitFor(() => expect(mocks.workspaceCheckpoint).toHaveBeenCalledWith('workspace-1', 'creation', expect.objectContaining({
      payload: { plan: { goal: '发布春季新品', content_type: 'wechat-article', execution_profile: 'effective', image_ratio: 'auto', image_capability_key: 'standard', target_audience: '关注茶饮的年轻人' } },
      status: 'ready',
    })))
    await waitFor(() => expect(mocks.workspaceCommit).toHaveBeenCalledWith('workspace-1', 'creation', expect.objectContaining({
      checkpoint_id: 'checkpoint-1',
      checkpoint_digest: 'digest-1',
      idempotency_key: expect.any(String),
    })))
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/tasks/task-created-1'))
  })

  it('uses the selected persona image ratio as the creation default', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [{ id: 'creation', displayName: '创作模式', description: '制定计划', available: true }],
      projects: [{ id: 'project-ratio', name: '画幅人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.projectsList.mockResolvedValue([{ id: 'project-ratio', name: '画幅人设', platform: 'wechat', image_ratio: '1:1', status: 'active' }])
    mocks.platformConfigs.mockResolvedValue([{ id: 'wechat', default_image_ratio: '16:9', supported_image_ratios: ['16:9', '1:1'] }])
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-ratio', mode_id: 'creation', session_id: 'session-1', sessions: { creation: 'session-1' }, project_id: 'project-ratio', runtime_status: 'starting', expires_at: '' })
    mocks.streamEvents = [{ event: 'draft.ready', id: 'event-ratio', data: JSON.stringify({ mode_id: 'creation', session_id: 'session-1', draft: { ready: true, payload: { plan: { goal: '按人设画幅创作', content_type: 'wechat-article', topic: '春茶冲泡', angle: '新手常见问题', expression_form: '步骤教程' } } } }) }]
    renderPage({ modeId: 'creation', projectId: 'project-ratio' })

    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '按人设画幅创作' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    const card = await screen.findByRole('region', { name: '确认创作' })
    expect(within(card).getByText('1:1')).toBeInTheDocument()
    fireEvent.click(within(card).getByRole('button', { name: '创作作品' }))

    await waitFor(() => expect(mocks.workspaceCheckpoint).toHaveBeenCalledWith('workspace-ratio', 'creation', expect.objectContaining({
      payload: { plan: expect.objectContaining({ image_ratio: '1:1' }) },
    })))
  })

  it('disables the composer while a creation confirmation is committing', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '我的人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'creation', session_id: 'session-1', sessions: { creation: 'session-1' }, project_id: 'project-1', runtime_status: 'starting', expires_at: '' })
    let resolveCommit!: (value: unknown) => void
    mocks.workspaceCommit.mockReturnValue(new Promise((resolve) => { resolveCommit = resolve }))
    mocks.streamEvents = [{
      event: 'draft.ready',
      id: 'event-commit-lock',
      data: JSON.stringify({ mode_id: 'creation', session_id: 'session-1', draft: { ready: true, payload: { plan: { goal: '锁定提交', content_type: 'wechat-article', topic: '春季内容计划', angle: '围绕真实变化展开', expression_form: '案例型' } } } }),
    }]
    renderPage({ modeId: 'creation', projectId: 'project-1' })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '开始创作' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    const card = await screen.findByRole('region', { name: '确认创作' })
    fireEvent.change(composer, { target: { value: '不应并发发送' } })
    fireEvent.click(within(card).getByRole('button', { name: '创作作品' }))

    await waitFor(() => expect(mocks.workspaceCommit).toHaveBeenCalled())
    expect(composer).toBeDisabled()
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()

    resolveCommit({ mode_id: 'creation', status: 'committed', task: { id: 'task-created-2', project_id: 'project-1', status: 'pending' } })
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/tasks/task-created-2'))
  })

  it('keeps uploaded composer attachments in the creation confirmation plan', async () => {
    setBootstrap({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '我的人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'creation', session_id: 'session-1', sessions: { creation: 'session-1' }, project_id: 'project-1', runtime_status: 'starting', expires_at: '' })
    mocks.streamEvents = [{
      event: 'draft.ready',
      id: 'event-attachment-draft',
      data: JSON.stringify({ mode_id: 'creation', session_id: 'session-1', draft: { ready: true, payload: { plan: { goal: '基于资料创作', content_type: 'wechat-article', topic: '资料中的核心信息', angle: '用户实际问题', expression_form: '深度文章' } } } }),
    }]
    uploadToOSSMock.mockResolvedValue({ uploadSessionId: 'upload-session-1', uploadId: 'upload-session-1', key: 'staging/brief.pdf', previewUrl: '', publicUrl: '', contentType: 'application/pdf', size: 16 })
    renderPage({ modeId: 'creation', projectId: 'project-1' })

    await waitFor(() => expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled())
    fireEvent.change(await screen.findByLabelText('选择附件文件'), { target: { files: [new File(['brief'], 'brief.pdf', { type: 'application/pdf' })] } })
    await waitFor(() => expect(uploadToOSSMock).toHaveBeenCalledWith(expect.objectContaining({ purpose: 'ai_entry_attachment' })))
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '按照资料完成文章' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    const card = await screen.findByRole('region', { name: '确认创作' })
    fireEvent.click(within(card).getByRole('button', { name: '创作作品' }))

    await waitFor(() => expect(mocks.workspaceCheckpoint).toHaveBeenCalledWith('workspace-1', 'creation', expect.objectContaining({
      payload: { plan: {
        goal: '基于资料创作',
        content_type: 'wechat-article',
        topic: '资料中的核心信息',
        angle: '用户实际问题',
        expression_form: '深度文章',
        execution_profile: 'effective',
        image_ratio: 'auto',
        image_capability_key: 'standard',
        assets: [{ type: 'document', upload_id: 'upload-session-1', file_name: 'brief.pdf', mime_type: 'application/pdf', file_size: 16 }],
      } },
    })))
  })

  it('offers local upload and media-library selection from the creation plus menu', async () => {
    mocks.bootstrap.mockResolvedValue({
      defaultModeId: 'creation',
      modes: [{ id: 'creation', displayName: '创作模式', description: '制定计划', available: true }],
      projects: [{ id: 'project-1', name: '我的人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.projectsList.mockResolvedValue([{ id: 'project-1', name: '我的人设', platform: 'wechat', status: 'active' }])
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'creation', session_id: 'session-1', sessions: { creation: 'session-1' }, project_id: 'project-1', runtime_status: 'starting', expires_at: '' })
    uploadToOSSMock.mockResolvedValue({ uploadSessionId: 'upload-1', uploadId: 'upload-1', key: 'staging/cover.png', previewUrl: '', publicUrl: '', contentType: 'image/png', size: 4 })
    renderPage({ modeId: 'creation', projectId: 'project-1' })

    const mediaPicker = await screen.findByRole('button', { name: '从素材库选择' })
    await waitFor(() => expect(mediaPicker).toBeEnabled())
    fireEvent.click(mediaPicker)
    await waitFor(() => expect(mocks.userMediaList).toHaveBeenCalledWith({ q: undefined, status: 'available', limit: 30 }))
    expect(screen.getByRole('button', { name: '上传素材' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '从素材库选择' })).toHaveLength(2)
    fireEvent.change(screen.getByLabelText('上传创作素材'), { target: { files: [new File(['png'], 'cover.png', { type: 'image/png' })] } })
    await waitFor(() => expect(uploadToOSSMock).toHaveBeenCalledWith(expect.objectContaining({ purpose: 'ai_entry_attachment' })))
  })

  it('keeps persona context in the composer header without a profile-mode entry action', async () => {
    setBootstrap({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '已有的人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.projectsList.mockResolvedValue([{ id: 'project-1', name: '已有的人设', platform: 'wechat', avatar_url: '', description: '公众号内容创作', status: 'active' }])
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'profile', session_id: 'session-1', sessions: { profile: 'session-1' }, project_id: 'project-1', runtime_status: 'starting', expires_at: '' })
    renderPage({ modeId: 'profile', projectId: 'project-1' })

    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '继续完善已有的人设' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.queryByText('AI 正在处理…')).not.toBeInTheDocument())

    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('已有的人设')
    expect(screen.queryByRole('combobox', { name: '人设配置' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '进入人设模式' })).not.toBeInTheDocument()
  })

  it('synchronizes an active review persona change after selecting a work', async () => {
    setBootstrap({
      defaultModeId: 'review',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [
        { id: 'project-1', name: '第一个人设', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] },
        { id: 'project-2', name: '第二个人设', platform: 'seednote', profileRevision: 1, profileReady: true, works: [] },
      ],
    })
    mocks.projectsList.mockResolvedValue([
      { id: 'project-1', name: '第一个人设', platform: 'wechat', avatar_url: '', description: '公众号内容创作', status: 'active' },
      { id: 'project-2', name: '第二个人设', platform: 'seednote', avatar_url: '', description: '种草内容创作', status: 'active' },
    ])
    mocks.works.mockImplementation(async (projectId: string) => projectId === 'project-2'
      ? { items: [{ id: 'work-2', title: '第二个人设的作品', type: 'note', createdAt: '2026-10-22T00:00:00Z' }], has_more: false }
      : { items: [{ id: 'work-1', title: '第一个人设的作品', type: 'article', createdAt: '2026-10-22T00:00:00Z' }], has_more: false })
    mocks.createWorkspace.mockResolvedValue({ id: 'workspace-1', mode_id: 'review', session_id: 'session-1', sessions: { review: 'session-1' }, project_id: 'project-1', content_id: 'work-1', runtime_status: 'starting', expires_at: '' })
    mocks.switchWorkspaceMode.mockResolvedValue({ id: 'workspace-1', mode_id: 'review', session_id: 'session-2', sessions: { review: 'session-2' }, project_id: 'project-2', content_id: 'work-2', runtime_status: 'starting', expires_at: '' })
    renderPage({ modeId: 'review', projectId: 'project-1', contentId: 'work-1' })

    await screen.findByRole('option', { name: '第一个人设的作品' })
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '复盘第一个作品' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.queryByText('AI 正在处理…')).not.toBeInTheDocument())

    expect(screen.getByTestId('assistant-context-bar')).toHaveTextContent('第一个人设')
    expect(screen.queryByRole('combobox', { name: '人设配置' })).not.toBeInTheDocument()
    expect(screen.getByRole('region', { name: '确认复盘结论' })).toBeInTheDocument()
  })
})
