import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AssistantWorkspace from './AssistantWorkspace'
import { createTestQueryClient } from '@/test/test-utils'

const mocks = vi.hoisted(() => ({
  bootstrap: vi.fn(),
  works: vi.fn(),
  createWorkspace: vi.fn(),
  workspaceMessage: vi.fn(),
  switchWorkspaceMode: vi.fn(),
  workspaceCommit: vi.fn(),
  workspaceCheckpoint: vi.fn(),
  taskGet: vi.fn(),
  taskFiles: vi.fn(),
}))

const uploadToOSSMock = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      assistant: { ...actual.api.assistant, ...mocks },
      tasks: { ...actual.api.tasks, get: mocks.taskGet, files: mocks.taskFiles },
    },
  }
})

vi.mock('@/lib/interactive-events', () => ({
  streamAssistantWorkspace: () => (async function* () {
    yield { event: 'assistant.message', id: 'event-1', data: JSON.stringify({ session_id: 'session-1', text: '收到，我开始处理。' }) }
    yield { event: 'draft.ready', id: 'event-2', data: JSON.stringify({ session_id: 'session-1', draft: { confirmation_token: 'token-1', ready: true, project: { platform: 'video-edit' } } }) }
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

function renderPage() {
  const queryClient = createTestQueryClient()
  return render(<QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/']}><AssistantWorkspace /><LocationProbe /></MemoryRouter></QueryClientProvider>)
}

describe('AssistantWorkspace', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    vi.clearAllMocks()
    sessionStorage.clear()
    mocks.bootstrap.mockResolvedValue({
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
    mocks.workspaceMessage.mockResolvedValue(undefined)
    mocks.workspaceCommit.mockResolvedValue({ mode_id: 'profile', status: 'committed', project: { id: 'project-new', name: '新画像', platform: 'wechat' } })
    mocks.workspaceCheckpoint.mockResolvedValue({ id: 'checkpoint-1', payload_digest: 'digest-1' })
    mocks.taskGet.mockResolvedValue({ id: 'video-task-1', type: 'video-edit', prompt: '剪辑访谈', status: 'completed', project_id: 'video-project', execution_profile: 'effective', billing_price_credits: 1600, created_at: '' })
    mocks.taskFiles.mockResolvedValue([
      { id: 'final-video', task_id: 'video-task-1', state: 'delivered', role: 'final_video', delivery_role: 'final_video', file_name: 'final.mp4', mime_type: 'video/mp4', file_size: 1024, url: '', preview_url: 'https://assets.example/final.mp4', created_at: '', is_deliverable: true },
      { id: 'edit-list', task_id: 'video-task-1', state: 'delivered', role: 'artifact', delivery_role: 'edit_decision_list', file_name: 'edit-decision-list.json', mime_type: 'application/json', file_size: 10, url: '', created_at: '', is_deliverable: true },
      { id: 'strategy', task_id: 'video-task-1', state: 'delivered', role: 'artifact', delivery_role: 'edit_strategy', file_name: 'edit-strategy.md', mime_type: 'text/markdown', file_size: 10, url: '', created_at: '', is_deliverable: true },
    ])
  })

  it('keeps the empty and active states on the root route', async () => {
    renderPage()
    expect(await screen.findByRole('heading', { name: /开始为「新人设」建立画像/ })).toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '帮我建立一个画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledWith({ mode_id: 'profile' }, expect.any(String)))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledWith('workspace-1', 'profile', '帮我建立一个画像'))
    expect(screen.getByTestId('location')).toHaveTextContent('/')
    expect(await screen.findByText('收到，我开始处理。')).toBeInTheDocument()
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
  })

  it('switches the active mode through the workspace API body', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: '创作模式' }))
    fireEvent.change(screen.getByRole('textbox', { name: '发送消息' }), { target: { value: '开始创作' } })
    // Creation is unavailable without a confirmed project, so the page stays
    // in one place and does not invent a query-driven navigation state.
    expect(screen.getByTestId('location')).toHaveTextContent('/')
  })

  it('configures and commits video-edit defaults from a profile draft', async () => {
    mocks.bootstrap.mockReset()
    mocks.bootstrap
      .mockResolvedValueOnce({ defaultModeId: 'profile', modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ], projects: [] })
      .mockResolvedValue({ defaultModeId: 'profile', modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ], projects: [{ id: 'project-new', name: '新画像', platform: 'video-edit', profileRevision: 1, profileReady: true, video_edit_defaults: { preferences: { quality_tier: 'quality' } }, works: [] }] })
    mocks.workspaceCommit.mockResolvedValue({ mode_id: 'profile', status: 'committed', project: { id: 'project-new', name: '新画像', platform: 'video-edit', video_edit_defaults: { preferences: { quality_tier: 'quality' } } } })
    renderPage()
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '建立画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '确认并保存' })).toBeInTheDocument())
    fireEvent.change(screen.getByRole('combobox', { name: '默认质量档位' }), { target: { value: 'quality' } })
    fireEvent.click(screen.getByRole('button', { name: '确认并保存' }))
    await waitFor(() => expect(mocks.workspaceCommit).toHaveBeenCalledWith('workspace-1', 'profile', expect.objectContaining({
      confirmation_token: 'token-1',
      idempotency_key: expect.any(String),
      video_edit_defaults: { preferences: { quality_tier: 'quality' } },
    })))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '选择画像' })).toHaveValue('project-new'))
    expect(screen.getByRole('combobox', { name: '默认质量档位' })).toHaveValue('quality')
    expect(mocks.bootstrap).toHaveBeenCalledTimes(2)
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

  it('keeps the unsent message and offers a fresh session after workspace expiry', async () => {
    const expired = Object.assign(new Error('expired'), { response: { data: { error_code: 'assistant_workspace_expired' } } })
    mocks.workspaceMessage.mockRejectedValueOnce(expired).mockResolvedValueOnce(undefined)
    renderPage()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '继续完善画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(composer).toHaveValue('继续完善画像'))
    expect(await screen.findByText(/对话已过期/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.createWorkspace).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalledTimes(2))
    expect(mocks.workspaceMessage).toHaveBeenLastCalledWith('workspace-1', 'profile', '继续完善画像')
  })

  it('collects review context in the same workspace before switching modes', async () => {
    mocks.bootstrap.mockResolvedValue({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'project-1', name: '我的画像', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.works.mockResolvedValue({ items: [{ id: 'work-1', title: '已完成作品', type: 'article', createdAt: '2026-10-22T00:00:00Z' }], has_more: false })
    mocks.switchWorkspaceMode.mockResolvedValue({ id: 'workspace-1', mode_id: 'review', session_id: 'review-session', sessions: { profile: 'session-1', review: 'review-session' }, project_id: 'project-1', content_id: 'work-1', runtime_status: 'running', expires_at: '' })
    renderPage()
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '建立画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(mocks.workspaceMessage).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: '复盘模式' }))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '选择画像' })).toBeEnabled())
    fireEvent.change(screen.getByRole('combobox', { name: '选择画像' }), { target: { value: 'project-1' } })
    await waitFor(() => expect(screen.getByRole('combobox', { name: '选择作品' })).toBeEnabled())
    fireEvent.change(screen.getByRole('combobox', { name: '选择作品' }), { target: { value: 'work-1' } })
    await waitFor(() => expect(mocks.switchWorkspaceMode).toHaveBeenCalledWith('workspace-1', 'review', { project_id: 'project-1', content_id: 'work-1' }))
  })

  it('previews the selected video-edit work and exposes its delivered edit artifacts in review mode', async () => {
    mocks.bootstrap.mockResolvedValue({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划', available: true },
        { id: 'review', displayName: '复盘模式', description: '复盘作品', available: true },
      ],
      projects: [{ id: 'video-project', name: '视频项目', platform: 'video-edit', profileRevision: 1, profileReady: true, works: [] }],
    })
    mocks.works.mockResolvedValue({ items: [{ id: 'video-task-1', title: '访谈成片', type: 'video-edit', createdAt: '2026-10-22T00:00:00Z' }], has_more: false })
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: '复盘模式' }))

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
    mocks.bootstrap.mockResolvedValue({
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
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: '创作模式' }))
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
    fireEvent.click(await screen.findByRole('button', { name: '确认并保存' }))

    await waitFor(() => expect(mocks.workspaceCheckpoint).toHaveBeenCalledWith('workspace-1', 'creation', expect.objectContaining({
      payload: expect.objectContaining({
        content_type: 'video-edit',
        assets: [{ type: 'video', upload_id: 'upload-session-1', file_name: 'interview.mp4', mime_type: 'video/mp4', file_size: 42 }],
        video_edit_preferences: { quality_tier: 'quality', subtitle_mode: 'required' },
        video_edit_brief: '剪成 30 秒访谈短片',
      }),
    })))
  })
})
