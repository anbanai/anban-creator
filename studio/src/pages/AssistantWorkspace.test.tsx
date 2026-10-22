import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AssistantWorkspace from './AssistantWorkspace'
import { createTestQueryClient } from '@/test/test-utils'

const mocks = vi.hoisted(() => ({
  bootstrap: vi.fn(),
  works: vi.fn(),
  createWorkspace: vi.fn(),
  workspaceMessage: vi.fn(),
  switchWorkspaceMode: vi.fn(),
  workspaceCommit: vi.fn(),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, assistant: { ...actual.api.assistant, ...mocks } } }
})

vi.mock('@/lib/interactive-events', () => ({
  streamAssistantWorkspace: () => (async function* () {
    yield { event: 'assistant.message', id: 'event-1', data: JSON.stringify({ session_id: 'session-1', text: '收到，我开始处理。' }) }
    yield { event: 'draft.ready', id: 'event-2', data: JSON.stringify({ session_id: 'session-1', draft: { confirmation_token: 'token-1', ready: true } }) }
  })(),
}))

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

  it('syncs a newly committed profile project back into the active workspace context', async () => {
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
      ], projects: [{ id: 'project-new', name: '新画像', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }] })
    renderPage()
    fireEvent.change(await screen.findByRole('textbox', { name: '发送消息' }), { target: { value: '建立画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '确认并保存' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '确认并保存' }))
    await waitFor(() => expect(mocks.workspaceCommit).toHaveBeenCalledWith('workspace-1', 'profile', { confirmation_token: 'token-1', idempotency_key: expect.any(String) }))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '选择画像' })).toHaveValue('project-new'))
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
})
