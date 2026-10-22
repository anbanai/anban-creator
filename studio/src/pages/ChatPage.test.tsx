import { act, fireEvent, render as renderWithoutProviders, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ChatPage from './ChatPage'
import { createTestQueryClient } from '@/test/test-utils'
import type { InteractiveStreamEvent } from '@/lib/interactive-events'
import { assistantMessageHandoffKey, saveAssistantMessageHandoff } from '@/lib/assistant-message-handoff'

const interactiveMocks = vi.hoisted(() => ({
  agents: vi.fn(),
  createSession: vi.fn(),
  message: vi.fn(),
  answer: vi.fn(),
  commit: vi.fn(),
  close: vi.fn(),
}))
const assistantMocks = vi.hoisted(() => ({
  bootstrap: vi.fn(),
  latestCheckpoint: vi.fn(),
  works: vi.fn(),
  createSession: vi.fn(),
  message: vi.fn(),
  answer: vi.fn(),
  checkpoint: vi.fn(),
  commit: vi.fn(),
  close: vi.fn(),
}))
const streamMock = vi.hoisted(() => vi.fn())
const authState = vi.hoisted(() => ({ userId: 'test-user' }))

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ token: 'test-token', user: { id: authState.userId } }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, interactive: interactiveMocks, assistant: assistantMocks } }
})

vi.mock('@/lib/interactive-events', async () => {
  const actual = await vi.importActual<typeof import('@/lib/interactive-events')>('@/lib/interactive-events')
  return { ...actual, streamInteractiveSession: streamMock }
})

const agent = {
  id: 'project-creator',
  display_name: '画像模式',
  description: '创建人设',
  available: true,
}

function holdStream(events: InteractiveStreamEvent[], signal?: AbortSignal) {
  return (async function* () {
    for (const event of events) yield event
    await new Promise<void>((resolve) => {
      if (signal?.aborted) {
        resolve()
        return
      }
      signal?.addEventListener('abort', () => resolve(), { once: true })
    })
  })()
}

function renderPage(path: string) {
  const queryClient = createTestQueryClient()
  return renderWithoutProviders(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="*" element={<><RouteLocation /><ChatPage /></>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function renderChat(path = '/projects/create', options: { initialMessage?: string | false } = {}) {
  const [pathname, search = ''] = path.split('?')
  const params = new URLSearchParams(search)
  params.set('assistant_session', '1')
  params.set('mode', params.get('mode') ?? 'profile')
  const view = renderPage(`${pathname}?${params.toString()}`)
  const mode = params.get('mode') ?? 'profile'
  const project = params.get('project_id') ?? 'new'
  const content = params.get('content_id') ?? 'none'
  const sessionKey = `anban:assistant-session:${authState.userId}:${mode}:${project}:${content}`
  let hasResumableSession = false
  try {
    hasResumableSession = Boolean(JSON.parse(sessionStorage.getItem(sessionKey) ?? '{}').sessionId)
  } catch {
    hasResumableSession = false
  }
  if (options.initialMessage !== false && !hasResumableSession) {
    const initialMessage = options.initialMessage ?? '测试首条消息'
    void screen.findByRole('textbox', { name: '发送消息' }).then(async (composer) => {
      await waitFor(() => expect(composer).toBeEnabled())
      fireEvent.change(composer, { target: { value: initialMessage } })
      fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    }).catch(() => undefined)
  }
  return view
}

function RouteLocation() {
  const location = useLocation()
  return <output aria-hidden="true" data-testid="route-location">{location.pathname}{location.search}</output>
}

describe('ChatPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authState.userId = 'test-user'
    sessionStorage.clear()
    localStorage.clear()
    interactiveMocks.agents.mockResolvedValue([agent])
    interactiveMocks.createSession.mockResolvedValue({ id: 'session-1', agent_id: agent.id, status: 'running' })
    interactiveMocks.message.mockResolvedValue({})
    interactiveMocks.answer.mockResolvedValue({})
    interactiveMocks.commit.mockResolvedValue({ project: { id: 'project-1' } })
    interactiveMocks.close.mockResolvedValue({})
    assistantMocks.bootstrap.mockResolvedValue({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: false, disabledReason: '先完成画像初始化' },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: false, disabledReason: '先完成一件作品' },
      ],
      projects: [],
    })
    assistantMocks.createSession.mockImplementation((request: { mode_id: string; project_id?: string }, key?: string) => interactiveMocks.createSession(
      request.mode_id === 'profile' ? 'project-creator' : request.mode_id,
      request.mode_id === 'profile' ? request.project_id ? 'update-project-profile' : 'create-project' : request.mode_id,
      request.project_id,
      key,
    ))
    assistantMocks.message.mockImplementation((...args) => interactiveMocks.message(...args))
    assistantMocks.answer.mockImplementation((...args) => interactiveMocks.answer(...args))
    assistantMocks.latestCheckpoint.mockResolvedValue(null)
    assistantMocks.works.mockResolvedValue({ items: [], has_more: false })
    assistantMocks.checkpoint.mockResolvedValue({ id: 'checkpoint-1', payload_digest: 'saved-payload-digest', summary: '已保存的结构化草稿', status: 'draft', updatedAt: '' })
    assistantMocks.commit.mockImplementation((sessionId: string, payload: Record<string, unknown>) => interactiveMocks.commit(
      sessionId,
      payload.confirmation_token,
      payload.idempotency_key,
      payload.memory_append,
    ))
    assistantMocks.close.mockImplementation((...args) => interactiveMocks.close(...args))
    streamMock.mockImplementation((sessionId: string, token: string, signal?: AbortSignal) => {
      void sessionId
      void token
      return holdStream([], signal)
    })
  })

  it('loads the Assistant bootstrap without creating a runtime session', async () => {
    renderPage('/chat')

    await waitFor(() => expect(assistantMocks.bootstrap).toHaveBeenCalledTimes(1))
    expect(await screen.findByRole('heading', { name: '探索未至之境' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '选择模式' })).toHaveTextContent('画像模式')
    fireEvent.click(screen.getByRole('button', { name: '选择模式' }))
    expect(screen.getByRole('menuitem', { name: /创作模式/ })).toHaveTextContent('先完成画像初始化')
    expect(screen.getByRole('menuitem', { name: /复盘模式/ })).toHaveTextContent('先完成一件作品')
    expect(assistantMocks.bootstrap).toHaveBeenCalledTimes(1)
    expect(assistantMocks.createSession).not.toHaveBeenCalled()
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
    expect(streamMock).not.toHaveBeenCalled()
  })

  it('sends the single-page composer message after creating the scoped runtime session', async () => {
    saveAssistantMessageHandoff(assistantMessageHandoffKey('test-user', 'profile'), '我想建立一个面向职场读者的画像')
    renderPage('/chat?assistant_session=1&mode=profile')

    expect(await screen.findByRole('textbox', { name: '发送消息' })).toHaveValue('我想建立一个面向职场读者的画像')
    expect(assistantMocks.createSession).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(assistantMocks.createSession).toHaveBeenCalledWith({ mode_id: 'profile' }, expect.any(String)))
    await waitFor(() => expect(assistantMocks.message).toHaveBeenCalledWith('session-1', '我想建立一个面向职场读者的画像'))
    expect(sessionStorage.getItem('anban:assistant-message-handoff:test-user:profile::')).toBeNull()
  })

  it('sends a one-time portrait idea into the scoped creation session', async () => {
    assistantMocks.bootstrap.mockResolvedValueOnce({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: true },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: false },
      ],
      projects: [{ id: 'project-1', name: '花艺', platform: 'wechat', profileReady: true, profileRevision: 1, works: [] }],
    })
    sessionStorage.setItem('anban:portrait-creation-handoff:project-1', JSON.stringify({ idea: '写周末插花指南' }))

    renderChat('/chat?mode=creation&project_id=project-1', { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect((composer as HTMLTextAreaElement).value).toContain('写周末插花指南'))
    expect(assistantMocks.message).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(assistantMocks.message).toHaveBeenCalledWith('session-1', expect.stringContaining('写周末插花指南')))
    expect(sessionStorage.getItem('anban:portrait-creation-handoff:project-1')).toBeNull()
    expect(assistantMocks.commit).not.toHaveBeenCalled()
  })

  it('offers only checkpoints that match the selected project scope', async () => {
    assistantMocks.bootstrap.mockResolvedValueOnce({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: true },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: false, disabledReason: '先完成一件作品' },
      ],
      projects: [
        { id: 'project-a', name: '画像 A', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] },
        { id: 'project-b', name: '画像 B', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] },
      ],
    })
    assistantMocks.latestCheckpoint.mockImplementation(async (scope: { project_id?: string }) => scope.project_id === 'project-a'
      ? { id: 'checkpoint-project-a', summary: '画像 A 草稿', status: 'draft', updatedAt: '' } : null)
    renderPage('/chat?mode=creation&project_id=project-a')

    expect(await screen.findByRole('button', { name: '继续草稿' })).toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox', { name: '选择画像' }), { target: { value: 'project-b' } })

    expect(screen.queryByRole('button', { name: '继续草稿' })).not.toBeInTheDocument()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps the first message available before starting a runtime session', async () => {
    renderChat('/projects/create', { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    expect(composer).toBeEnabled()
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
    fireEvent.change(composer, { target: { value: '我想先建立一个面向职场读者的画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledWith('project-creator', 'create-project', undefined, expect.any(String)))
    await waitFor(() => expect(interactiveMocks.message).toHaveBeenCalledWith('session-1', '我想先建立一个面向职场读者的画像'))
    expect(screen.getByRole('button', { name: /画像模式/ })).toBeInTheDocument()
  })

  it('locks the composer while a ready profile draft waits for confirmation', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      {
        id: '1',
        event: 'draft.ready',
        data: JSON.stringify({
          draft: {
            ready: true,
            confirmation_token: 'profile-confirmation',
            project: { name: '职场画像', platform: 'wechat', instructions: '面向职场读者' },
            profile: { dimensions: { identity: { content: { topic: '职场成长' } } } },
          },
          ready: true,
        }),
      },
    ], signal))
    renderChat('/projects/create', { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '我想建立一个面向职场读者的画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => expect(assistantMocks.message).toHaveBeenCalled())
    await screen.findByRole('button', { name: '确认创建人设' })
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
  })

  it('keeps the chat page inside the available app height', async () => {
    renderChat('/projects/create', { initialMessage: false })

    await screen.findByRole('textbox', { name: '发送消息' })
    const page = screen.getByTestId('chat-page')
    expect(page.className).toContain('min-h-0')
    expect(page.className).not.toContain('100dvh')
  })

  it('preserves history position and offers a jump when a message arrives above the bottom', async () => {
    let release!: () => void
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => (async function* () {
      yield { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '历史消息' }) }
      await new Promise<void>((resolve) => {
        release = resolve
        signal?.addEventListener('abort', () => resolve(), { once: true })
      })
      yield { id: '2', event: 'assistant.message', data: JSON.stringify({ text: '新消息' }) }
      await new Promise<void>((resolve) => signal?.addEventListener('abort', () => resolve(), { once: true }))
    })())
    renderChat('/projects/create')

    await screen.findByText('历史消息')
    const scrollRegion = await screen.findByTestId('chat-scroll-region')
    Object.defineProperties(scrollRegion, {
      clientHeight: { configurable: true, value: 100 },
      scrollHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })
    fireEvent.scroll(scrollRegion)
    release()

    expect(await screen.findByText('新消息')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /有 1 条新消息/ })).toBeInTheDocument()
    expect(scrollRegion.scrollTop).toBe(0)
  })

  it('follows new messages when the reader is already near the bottom', async () => {
    let release!: () => void
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => (async function* () {
      yield { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '第一条' }) }
      await new Promise<void>((resolve) => {
        release = resolve
        signal?.addEventListener('abort', () => resolve(), { once: true })
      })
      yield { id: '2', event: 'assistant.message', data: JSON.stringify({ text: '第二条' }) }
      await new Promise<void>((resolve) => signal?.addEventListener('abort', () => resolve(), { once: true }))
    })())
    renderChat('/projects/create')

    await screen.findByText('第一条')
    const scrollRegion = await screen.findByTestId('chat-scroll-region')
    const scrollTo = vi.fn()
    Object.defineProperties(scrollRegion, {
      clientHeight: { configurable: true, value: 100 },
      scrollHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, writable: true, value: 410 },
      scrollTo: { configurable: true, value: scrollTo },
    })
    fireEvent.scroll(scrollRegion)
    release()

    await screen.findByText('第二条')
    await waitFor(() => expect(scrollTo).toHaveBeenCalledWith({ top: 500, behavior: 'smooth' }))
    expect(screen.queryByRole('button', { name: /有 1 条新消息/ })).not.toBeInTheDocument()
  })

  it('lets the reader mute and restore new-message sounds', async () => {
    renderChat('/projects/create')

    const soundButton = await screen.findByRole('button', { name: '关闭新消息声音' })
    fireEvent.click(soundButton)
    expect(screen.getByRole('button', { name: '开启新消息声音' })).toBeInTheDocument()
    expect(localStorage.getItem('anban:chat:new-message-sound')).toBe('off')
  })

  it('asks before cancelling and preserves saved conversation material', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '已整理的人设方向' }) },
    ], signal))
    renderChat('/projects/create')

    expect(await screen.findByText('已整理的人设方向')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消对话' }))

    expect(screen.getByRole('alertdialog')).toHaveTextContent('Agent 会立即停止')
    expect(interactiveMocks.close).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '确认取消对话' }))

    await waitFor(() => expect(interactiveMocks.close).toHaveBeenCalledWith('session-1'))
    expect(assistantMocks.checkpoint).toHaveBeenCalledWith('session-1', expect.objectContaining({ status: 'draft' }))
    expect(assistantMocks.checkpoint.mock.invocationCallOrder[0]).toBeLessThan(assistantMocks.close.mock.invocationCallOrder[0]!)
    expect(screen.getByText('已整理的人设方向')).toBeInTheDocument()
    expect(screen.getByText('对话已取消')).toBeInTheDocument()
    expect(screen.getByText('已取消')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复已保存草稿' })).toBeInTheDocument()
    const saved = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')
    expect(saved.sessionId).toBeUndefined()
    expect(saved.terminalState).toBe('closed')
    expect(saved.messages).toEqual(expect.arrayContaining([expect.objectContaining({ text: '已整理的人设方向' })]))
  })

  it('keeps an Assistant session open when its cancellation checkpoint cannot be saved', async () => {
    assistantMocks.checkpoint.mockRejectedValueOnce(new Error('checkpoint unavailable'))
    renderChat('/projects/create')

    fireEvent.click(await screen.findByRole('button', { name: '取消对话' }))
    fireEvent.click(screen.getByRole('button', { name: '确认取消对话' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('checkpoint unavailable')
    expect(assistantMocks.close).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '继续对话' }))
    expect(screen.getByRole('button', { name: '取消对话' })).toBeInTheDocument()
  })

  it('saves and closes the current session before returning to the selected mode without starting it', async () => {
    assistantMocks.bootstrap.mockResolvedValueOnce({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: true },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: false, disabledReason: '先完成一件作品' },
      ],
      projects: [],
    })
    renderChat('/projects/create')

    await waitFor(() => expect(assistantMocks.createSession).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: /画像模式/ }))
    fireEvent.click(screen.getByRole('menuitem', { name: /创作模式/ }))

    await waitFor(() => expect(screen.getByTestId('route-location')).toHaveTextContent('/chat?assistant_session=1&mode=creation'))
    expect(assistantMocks.checkpoint).toHaveBeenCalledWith('session-1', expect.objectContaining({ status: 'draft' }))
    expect(assistantMocks.checkpoint.mock.invocationCallOrder[0]).toBeLessThan(assistantMocks.close.mock.invocationCallOrder[0]!)
    expect(assistantMocks.createSession).toHaveBeenCalledTimes(1)
  })

  it('offers the saved creation checkpoint after switching back to its scope', async () => {
    assistantMocks.bootstrap.mockResolvedValue({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: true },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: false },
      ],
      projects: [{ id: 'project-1', name: '画像 A', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    assistantMocks.checkpoint.mockImplementation(async () => {
      const saved = { id: 'creation-checkpoint', summary: '创作计划草稿', status: 'draft', updatedAt: '' }
      assistantMocks.latestCheckpoint.mockImplementation(async (scope: { mode_id: string }) => scope.mode_id === 'creation' ? saved : null)
      return saved
    })
    renderChat('/chat?mode=creation&project_id=project-1')
    fireEvent.click(await screen.findByRole('button', { name: /创作模式/ }))
    fireEvent.click(screen.getByRole('menuitem', { name: /画像模式/ }))
    expect(await screen.findByRole('button', { name: /画像模式/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /画像模式/ }))
    fireEvent.click(screen.getByRole('menuitem', { name: /创作模式/ }))
    await waitFor(() => expect(assistantMocks.latestCheckpoint).toHaveBeenCalledWith(expect.objectContaining({ mode_id: 'creation', project_id: 'project-1' })))
    fireEvent.click(await screen.findByRole('button', { name: '继续草稿' }))
    await waitFor(() => expect(assistantMocks.createSession).toHaveBeenLastCalledWith({ mode_id: 'creation', project_id: 'project-1', checkpoint_id: 'creation-checkpoint' }, expect.any(String)))
  })

  it('does not rewrite a committed checkpoint when switching modes', async () => {
    assistantMocks.bootstrap.mockResolvedValueOnce({
      defaultModeId: 'creation',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: true },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: false, disabledReason: '先完成一件作品' },
      ],
      projects: [{ id: 'project-1', name: '画像 A', platform: 'wechat', profileRevision: 1, profileReady: true, works: [] }],
    })
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { ready: true, fields: { goal: '春季新品发布' } } }) },
    ], signal))
    renderChat('/chat?mode=creation&project_id=project-1')

    fireEvent.click(await screen.findByRole('button', { name: '确认并创建作品任务' }))
    await waitFor(() => expect(assistantMocks.commit).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: /创作模式/ }))
    fireEvent.click(screen.getByRole('menuitem', { name: /画像模式/ }))

    await waitFor(() => expect(screen.getByTestId('route-location')).toHaveTextContent('/chat?assistant_session=1&mode=profile&project_id=project-1'))
    expect(assistantMocks.checkpoint).toHaveBeenCalledTimes(1)
    expect(assistantMocks.close).not.toHaveBeenCalled()
  })

  it.each([
    { mode: 'creation', payload: { plan: { goal: '春季新品发布' } }, text: '春季新品发布', label: '创作目标', button: '确认并创建作品任务' },
    { mode: 'review', payload: { review: { summary: '增加真实案例' } }, text: '增加真实案例', label: '复盘摘要', button: '确认复盘建议' },
  ])('displays and commits exactly the saved $mode draft digest', async ({ mode, payload, text, label, button }) => {
    assistantMocks.bootstrap.mockResolvedValue({ defaultModeId: mode, projects: [], modes: [{ id: mode, displayName: mode === 'creation' ? '创作模式' : '复盘模式', available: true }] })
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, payload } }) },
    ], signal))
    renderChat(`/chat?mode=${mode}&project_id=project-1${mode === 'review' ? '&content_id=work-1' : ''}`)
    await screen.findByRole('button', { name: button })
    expect(screen.getAllByText(label)).toHaveLength(1)
    expect(screen.getAllByText(text)).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: button }))
    await waitFor(() => expect(assistantMocks.commit).toHaveBeenCalledWith('session-1', {
      checkpoint_id: 'checkpoint-1', checkpoint_digest: 'saved-payload-digest', idempotency_key: expect.any(String),
    }))
    expect(assistantMocks.checkpoint).toHaveBeenCalledWith('session-1', expect.objectContaining({ payload, status: 'ready' }))
  })

  it('requires fresh confirmation when the draft changes while its checkpoint is saving', async () => {
    let resolveCheckpoint!: (value: unknown) => void
    let stageNextDraft!: () => void
    const nextDraft = new Promise<void>((resolve) => { stageNextDraft = resolve })
    assistantMocks.bootstrap.mockResolvedValue({ defaultModeId: 'creation', projects: [], modes: [{ id: 'creation', displayName: '创作模式', available: true }] })
    assistantMocks.checkpoint.mockImplementationOnce(() => new Promise((resolve) => { resolveCheckpoint = resolve }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => (async function* () {
      yield { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, payload: { plan: { goal: '用户看到的计划 A' } } } }) }
      await nextDraft
      yield { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 2, ready: true, payload: { plan: { goal: '随后生成的计划 B' } } } }) }
      yield* holdStream([], signal)
    })())
    renderChat('/chat?mode=creation&project_id=project-1')
    fireEvent.click(await screen.findByRole('button', { name: '确认并创建作品任务' }))
    expect(assistantMocks.checkpoint).toHaveBeenCalledWith('session-1', expect.objectContaining({ payload: { plan: { goal: '用户看到的计划 A' } } }))
    await act(async () => stageNextDraft())
    await waitFor(() => expect(screen.getAllByText('随后生成的计划 B')).toHaveLength(1))
    await act(async () => resolveCheckpoint({ id: 'checkpoint-1', payload_digest: 'plan-a-digest' }))
    expect(await screen.findByText('草稿已更新，请阅读最新内容后重新确认。')).toBeInTheDocument()
    expect(assistantMocks.commit).not.toHaveBeenCalled()
  })

  it('does not commit a checkpoint whose version digest is missing', async () => {
    assistantMocks.bootstrap.mockResolvedValue({ defaultModeId: 'creation', projects: [], modes: [{ id: 'creation', displayName: '创作模式', available: true }] })
    assistantMocks.checkpoint.mockResolvedValueOnce({ id: 'checkpoint-1' })
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { ready: true, payload: { plan: { goal: '新品发布' } } } }) },
    ], signal))
    renderChat('/chat?mode=creation&project_id=project-1')
    fireEvent.click(await screen.findByRole('button', { name: '确认并创建作品任务' }))
    expect(await screen.findByText('草稿确认信息未就绪，请重新保存后再确认。')).toBeInTheDocument()
    expect(assistantMocks.commit).not.toHaveBeenCalled()
  })

  it('shows the created work task and prevents confirming the same plan again', async () => {
    assistantMocks.bootstrap.mockResolvedValue({ defaultModeId: 'creation', projects: [], modes: [{ id: 'creation', displayName: '创作模式', available: true }] })
    assistantMocks.commit.mockResolvedValueOnce({ mode_id: 'creation', status: 'committed', task: { id: 'created-task' } })
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { ready: true, payload: { plan: { goal: '新品发布' } } } }) },
    ], signal))
    renderChat('/chat?mode=creation&project_id=project-1')
    fireEvent.click(await screen.findByRole('button', { name: '确认并创建作品任务' }))
    expect(await screen.findByText('作品任务已创建，可以查看创作进度。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认并创建作品任务' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '查看作品任务' }))
    await waitFor(() => expect(screen.getByTestId('route-location')).toHaveTextContent('/tasks/created-task'))
    expect(assistantMocks.commit).toHaveBeenCalledTimes(1)
  })

  it('blocks mode switching while an Assistant commit is in progress', async () => {
    assistantMocks.bootstrap.mockResolvedValueOnce({
      defaultModeId: 'profile',
      modes: [
        { id: 'profile', displayName: '画像模式', description: '建立或更新一个画像', available: true },
        { id: 'creation', displayName: '创作模式', description: '制定计划并创建作品任务', available: true },
        { id: 'review', displayName: '复盘模式', description: '分析已有作品', available: false, disabledReason: '先完成一件作品' },
      ],
      projects: [],
    })
    let resolveCommit!: (value: unknown) => void
    assistantMocks.commit.mockImplementationOnce(() => new Promise((resolve) => { resolveCommit = resolve }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { ready: true, confirmation_token: 'profile-token', project: { name: '测试画像', platform: 'wechat' } } }) },
    ], signal))
    renderChat('/projects/create')

    fireEvent.click(await screen.findByRole('button', { name: '确认创建人设' }))
    await waitFor(() => expect(assistantMocks.commit).toHaveBeenCalledTimes(1))

    expect(screen.getByRole('button', { name: /画像模式/ })).toBeDisabled()
    resolveCommit({})
  })

  it('preserves a submitted message when cancellation races its SSE event', async () => {
    let resolveMessage!: (value: unknown) => void
    let messageResolved = false
    interactiveMocks.message.mockImplementationOnce(() => new Promise((resolve) => {
      resolveMessage = (value) => { messageResolved = true; resolve(value) }
    }))
    renderChat('/projects/create', { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())
    fireEvent.change(composer, { target: { value: '刚提交但尚未同步的消息' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(interactiveMocks.message).toHaveBeenCalledWith('session-1', '刚提交但尚未同步的消息'))
    resolveMessage({})
    await waitFor(() => expect(messageResolved).toBe(true))

    fireEvent.click(screen.getByRole('button', { name: '取消对话' }))
    fireEvent.click(screen.getByRole('button', { name: '确认取消对话' }))
    await waitFor(() => expect(interactiveMocks.close).toHaveBeenCalledWith('session-1'))

    await waitFor(() => {
      const saved = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')
      expect(saved.sessionId).toBeUndefined()
      expect(saved.messages).toEqual(expect.arrayContaining([
        expect.objectContaining({ role: 'user', text: '刚提交但尚未同步的消息' }),
      ]))
    })
  })

  it('preserves a submitted answer when cancellation races its request', async () => {
    let resolveAnswer!: (value: unknown) => void
    interactiveMocks.answer.mockImplementationOnce(() => new Promise((resolve) => { resolveAnswer = resolve }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'question.opened', data: JSON.stringify({ call_id: 'call-1', questions: [{ id: 'platform', question: '选择平台', options: [{ label: '公众号' }] }] }) },
    ], signal))
    renderChat('/projects/create')

    fireEvent.click(await screen.findByRole('radio', { name: '公众号' }))
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))
    await waitFor(() => expect(interactiveMocks.answer).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: '取消对话' }))
    fireEvent.click(screen.getByRole('button', { name: '确认取消对话' }))
    await waitFor(() => expect(interactiveMocks.close).toHaveBeenCalledWith('session-1'))
    resolveAnswer({})

    await waitFor(() => {
      const saved = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')
      expect(saved.questionBatches).toEqual(expect.arrayContaining([
        expect.objectContaining({ callId: 'call-1', answers: [expect.objectContaining({ id: 'platform', selected: ['公众号'] })] }),
      ]))
    })
  })

  it('closes a session that finishes starting after cancellation', async () => {
    let resolveSession!: (session: { id: string; agent_id: string; status: string }) => void
    interactiveMocks.createSession.mockImplementationOnce(() => new Promise((resolve) => { resolveSession = resolve }))
    renderChat('/projects/create')

    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalled())
    fireEvent.click(await screen.findByRole('button', { name: '取消对话' }))
    fireEvent.click(screen.getByRole('button', { name: '确认取消对话' }))

    expect(interactiveMocks.close).not.toHaveBeenCalled()
    resolveSession({ id: 'session-starting', agent_id: agent.id, status: 'running' })

    await waitFor(() => expect(interactiveMocks.close).toHaveBeenCalledWith('session-starting'))
    expect(streamMock).not.toHaveBeenCalled()
    expect(screen.getByText('对话已取消')).toBeInTheDocument()
  })

  it('uses the Assistant mode route without exposing an Agent id', async () => {
    renderPage('/chat')

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())
    fireEvent.change(composer, { target: { value: '建立我的画像' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => expect(assistantMocks.createSession).toHaveBeenCalledWith({ mode_id: 'profile' }, expect.any(String)))
    expect(screen.getByTestId('route-location')).toHaveTextContent('/chat?assistant_session=1&mode=profile')
  })

  it('resumes a persisted session from its last SSE event without creating a new runtime', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    renderChat('/projects/create')

    await waitFor(() => expect(streamMock).toHaveBeenCalledWith(
      'session-1',
      'test-token',
      expect.any(AbortSignal),
      expect.objectContaining({ lastEventId: '4' }),
    ))
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('starts a checkpoint session instead of reconnecting the scoped browser session', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'stale-session', lastEventId: '9' }))
    renderChat('/chat?mode=profile&checkpoint_id=profile-checkpoint', { initialMessage: false })

    await waitFor(() => expect(assistantMocks.createSession).toHaveBeenCalledWith({ mode_id: 'profile', checkpoint_id: 'profile-checkpoint' }, expect.any(String)))
    expect(streamMock).toHaveBeenCalledWith('session-1', 'test-token', expect.any(AbortSignal), expect.any(Object))
    expect(streamMock).not.toHaveBeenCalledWith('stale-session', expect.anything(), expect.anything(), expect.anything())
  })

  it('recovers a lost runtime from the server checkpoint without replaying browser messages', async () => {
    const storageKey = 'anban:assistant-session:test-user:profile:new:none'
    sessionStorage.setItem(storageKey, JSON.stringify({
      sessionId: 'expired-runtime', lastEventId: '18', createIdempotencyKey: 'expired-create-key',
      messages: [{ id: 'message-1', role: 'user', text: '浏览器里的逐字聊天记录' }],
      draft: { revision: 3, ready: true, project: { name: '获客公众号' } },
    }))
    assistantMocks.latestCheckpoint.mockResolvedValue({ id: 'server-checkpoint', summary: '已保存的画像方向', status: 'draft', updatedAt: '' })
    streamMock.mockImplementationOnce(() => { throw new Error('SSE connection failed: 410') })
    renderChat('/projects/create')

    expect(await screen.findByText('原运行时已失效，正在检查服务端保存的结构化草稿')).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: '恢复已保存草稿' }))

    await waitFor(() => expect(assistantMocks.createSession).toHaveBeenCalledWith({ mode_id: 'profile', checkpoint_id: 'server-checkpoint' }, expect.any(String)))
    expect(assistantMocks.createSession.mock.calls[0]?.[1]).not.toBe('expired-create-key')
    expect(assistantMocks.message).not.toHaveBeenCalled()
    expect(assistantMocks.answer).not.toHaveBeenCalled()
    expect(assistantMocks.close).not.toHaveBeenCalled()
  })

  it('never resumes another work or user session from browser storage', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:review:project-1:work-a', JSON.stringify({ sessionId: 'work-a-session' }))
    sessionStorage.setItem('anban:assistant-session:other-user:review:project-1:work-b', JSON.stringify({ sessionId: 'other-user-session' }))
    assistantMocks.bootstrap.mockResolvedValue({ defaultModeId: 'review', projects: [], modes: [{ id: 'review', displayName: '复盘模式', available: true }] })
    renderChat('/chat?mode=review&project_id=project-1&content_id=work-b')
    await waitFor(() => expect(assistantMocks.createSession).toHaveBeenCalledWith({ mode_id: 'review', project_id: 'project-1', content_id: 'work-b' }, expect.any(String)))
    expect(streamMock.mock.calls.every((call) => call[0] === 'session-1')).toBe(true)
  })

  it('clears the visible conversation when navigating directly to another work', async () => {
    const queryClient = createTestQueryClient()
    function WorkSwitcher() {
      const navigate = useNavigate()
      return <><button onClick={() => navigate('/chat?assistant_session=1&mode=review&project_id=project-1&content_id=work-b')}>打开作品 B</button><ChatPage /></>
    }
    assistantMocks.bootstrap.mockResolvedValue({ defaultModeId: 'review', projects: [], modes: [{ id: 'review', displayName: '复盘模式', available: true }] })
    assistantMocks.createSession.mockResolvedValueOnce({ id: 'work-a-session' }).mockResolvedValueOnce({ id: 'work-b-session' })
    streamMock.mockImplementation((sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'assistant.message', data: JSON.stringify({ text: sessionId === 'work-a-session' ? '作品 A 的私有讨论' : '作品 B 的新讨论' }) },
    ], signal))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderWithoutProviders(<QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/chat?assistant_session=1&mode=review&project_id=project-1&content_id=work-a']}><WorkSwitcher /></MemoryRouter></QueryClientProvider>)
    const firstComposer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(firstComposer).toBeEnabled())
    fireEvent.change(firstComposer, { target: { value: '讨论作品 A' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    expect(await screen.findByText('作品 A 的私有讨论')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '打开作品 B' }))
    const secondComposer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(secondComposer).toBeEnabled())
    fireEvent.change(secondComposer, { target: { value: '讨论作品 B' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    expect(await screen.findByText('作品 B 的新讨论')).toBeInTheDocument()
    expect(screen.queryByText('作品 A 的私有讨论')).not.toBeInTheDocument()
    expect(assistantMocks.createSession).toHaveBeenLastCalledWith({ mode_id: 'review', project_id: 'project-1', content_id: 'work-b' }, expect.any(String))
  })

  it('clears the visible conversation when the signed-in user changes', async () => {
    const queryClient = createTestQueryClient()
    assistantMocks.createSession.mockResolvedValueOnce({ id: 'first-user-session' }).mockResolvedValueOnce({ id: 'second-user-session' })
    streamMock.mockImplementation((sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'assistant.message', data: JSON.stringify({ text: sessionId === 'first-user-session' ? '第一位用户的私有讨论' : '第二位用户的新讨论' }) },
    ], signal))
    const tree = () => <QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/chat?assistant_session=1&mode=profile']}><ChatPage /></MemoryRouter></QueryClientProvider>
    const view = renderWithoutProviders(tree())
    const firstComposer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(firstComposer).toBeEnabled())
    fireEvent.change(firstComposer, { target: { value: '建立第一位用户的画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    expect(await screen.findByText('第一位用户的私有讨论')).toBeInTheDocument()
    authState.userId = 'second-user'
    view.rerender(tree())
    const secondComposer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(secondComposer).toBeEnabled())
    fireEvent.change(secondComposer, { target: { value: '建立第二位用户的画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    expect(await screen.findByText('第二位用户的新讨论')).toBeInTheDocument()
    expect(screen.queryByText('第一位用户的私有讨论')).not.toBeInTheDocument()
    expect(sessionStorage.getItem('anban:assistant-session:second-user:profile:new:none')).toContain('second-user-session')
  })

  it('keeps a transient stream failure reconnectable without labeling the session as lost', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce(() => { throw new Error('SSE connection failed: 503') })

    renderChat('/projects/create')

    expect(await screen.findByRole('button', { name: '重新连接' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '恢复已保存草稿' })).not.toBeInTheDocument()
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').sessionId).toBe('session-1')
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
    expect(interactiveMocks.close).not.toHaveBeenCalled()
  })

  it('preserves structured recommendations from question events', async () => {
    streamMock.mockImplementationOnce((sessionId: string, token: string, signal?: AbortSignal) => {
      void sessionId
      void token
      return holdStream([{
        event: 'question.opened',
        data: JSON.stringify({
          call_id: 'call-recommendation',
          questions: [{
            id: 'platform', question: '选择主平台',
            options: [
              { label: '公众号', description: '适合沉淀深度内容', recommended: true },
              { label: '小红书' },
            ],
          }],
        }),
      }], signal)
    })
    renderChat('/projects/create')

    const recommended = await screen.findByRole('radio', { name: '公众号' })
    expect(recommended).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('推荐')).toBeInTheDocument()
    expect(screen.getByText('适合沉淀深度内容')).toBeInTheDocument()
  })

  it('does not enable a resumed session from SSE connectivity alone', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([], signal)
    })
    renderChat('/projects/create')

    await waitFor(() => expect(streamMock).toHaveBeenCalled())
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('restores an idle session from its authoritative SSE snapshot', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([{ event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: false, questions: [] }) }], signal)
    })
    renderChat('/projects/create')

    await waitFor(() => expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled())
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('keeps question cards in the same order as the streamed messages', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '第一条消息' }) },
      { id: '2', event: 'question.opened', data: JSON.stringify({ call_id: 'call-1', questions: [{ id: 'topic', question: '问题卡', options: [{ label: '回答' }] }] }) },
      { id: '3', event: 'assistant.message', data: JSON.stringify({ text: '第二条消息' }) },
    ], signal))
    const { container } = renderChat()

    await screen.findByText('问题卡')
    await screen.findByText('第二条消息')
    const content = container.textContent ?? ''
    expect(content.indexOf('第一条消息')).toBeLessThan(content.indexOf('问题卡'))
    expect(content.indexOf('问题卡')).toBeLessThan(content.indexOf('第二条消息'))
  })

  it('rebuilds a legacy session timeline in event order after refresh', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '3',
      messages: [
        { id: '1', role: 'assistant', text: '刷新前第一条消息' },
        { id: '3', role: 'assistant', text: '刷新前第二条消息' },
      ],
      questionBatches: [{ callId: 'call-1', questions: [{ id: 'topic', question: '刷新前的问题卡', options: [{ label: '回答' }] }] }],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '刷新前第一条消息' }) },
      { id: '2', event: 'question.opened', data: JSON.stringify({ call_id: 'call-1', questions: [{ id: 'topic', question: '刷新前的问题卡', options: [{ label: '回答' }] }] }) },
      { id: '3', event: 'assistant.message', data: JSON.stringify({ text: '刷新前第二条消息' }) },
    ], signal))
    const { container } = renderChat()

    await screen.findByText('刷新前的问题卡')
    await screen.findByText('刷新前第二条消息')
    const content = container.textContent ?? ''
    expect(content.indexOf('刷新前第一条消息')).toBeLessThan(content.indexOf('刷新前的问题卡'))
    expect(content.indexOf('刷新前的问题卡')).toBeLessThan(content.indexOf('刷新前第二条消息'))
    expect(streamMock.mock.calls[0]?.[3]).toEqual(expect.objectContaining({ lastEventId: undefined }))
  })

  it('orders legacy timeline items using message and question event IDs', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '3',
      messages: [
        { id: '1', role: 'assistant', text: '较早的消息' },
        { id: '3', role: 'assistant', text: '较晚的消息' },
      ],
      questionBatches: [{ callId: 'call-1', eventId: 2, answers: [{ id: 'topic', selected: ['回答'] }], questions: [{ id: 'topic', question: '中间的问题卡', options: [{ label: '回答' }] }] }],
      timeline: [
        { kind: 'message', message: { id: '3', role: 'assistant', text: '较晚的消息' } },
        { kind: 'question', callId: 'call-1' },
        { kind: 'message', message: { id: '1', role: 'assistant', text: '较早的消息' } },
      ],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '3', event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: false, questions: [] }) },
    ], signal))

    const { container } = renderChat('/projects/create')

    expect(await screen.findByText('较早的消息')).toBeInTheDocument()
    expect(screen.getByText('中间的问题卡')).toBeInTheDocument()
    expect(screen.getByText('较晚的消息')).toBeInTheDocument()
    const content = container.textContent ?? ''
    expect(content.indexOf('较早的消息')).toBeLessThan(content.indexOf('中间的问题卡'))
    expect(content.indexOf('中间的问题卡')).toBeLessThan(content.indexOf('较晚的消息'))
  })

  it('hydrates legacy messages without duplicating UUID-backed cache entries', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '1',
      messages: [{ id: 'legacy-message-uuid', role: 'assistant', text: '缓存里的历史消息' }],
      timeline: [{ kind: 'message', message: { id: 'legacy-message-uuid', role: 'assistant', text: '缓存里的历史消息' } }],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '缓存里的历史消息' }) },
    ], signal))

    renderChat('/projects/create')

    await screen.findByText('缓存里的历史消息')
    await waitFor(() => expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').messages).toHaveLength(1))
    expect(screen.getAllByText('缓存里的历史消息', { exact: true })).toHaveLength(1)
  })

  it('merges messages into an older partial timeline and replays its history', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '3',
      messages: [{ id: '1', role: 'assistant', text: '缓存里的消息' }],
      timeline: [{ kind: 'draft', id: 'draft' }],
      draft: { revision: 1, ready: true, project: { name: '人设' } },
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '缓存里的消息' }) },
      { id: '2', event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, project: { name: '人设' } } }) },
    ], signal))

    renderChat()

    expect(await screen.findByText('缓存里的消息')).toBeInTheDocument()
    expect(streamMock.mock.calls[0]?.[3]).toEqual(expect.objectContaining({ lastEventId: undefined }))
  })

  it('restores the visible message history after a refresh', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '4',
      messages: [{ id: 'message-1', role: 'assistant', text: '刷新前的消息' }],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([{ event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: false, questions: [] }) }], signal)
    })

    renderChat('/projects/create')

    expect(await screen.findByText('刷新前的消息')).toBeInTheDocument()
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').messages).toEqual([
      { id: 'message-1', role: 'assistant', text: '刷新前的消息' },
    ])
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('persists streamed messages for the next refresh', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '5', event: 'assistant.message', data: JSON.stringify({ text: '需要保留的消息' }) },
    ], signal))

    renderChat()

    await screen.findByText('需要保留的消息')
    await waitFor(() => expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').messages).toEqual([
      { id: '5', role: 'assistant', text: '需要保留的消息' },
    ]))
  })

  it('does not advance the persisted event cursor before the event projection is stored', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    let cursorAtReceipt: string | undefined
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onEvent?: (event: InteractiveStreamEvent) => void }) => (async function* () {
      const event = { id: '5', event: 'assistant.message', data: JSON.stringify({ text: '游标原子消息' }) }
      options?.onEvent?.(event)
      cursorAtReceipt = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').lastEventId
      yield event
      await new Promise<void>((resolve) => signal?.addEventListener('abort', () => resolve(), { once: true }))
    })())

    renderChat('/projects/create')

    expect(await screen.findByText('游标原子消息')).toBeInTheDocument()
    expect(cursorAtReceipt).toBe('4')
    await waitFor(() => {
      const persisted = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')
      expect(persisted.lastEventId).toBe('5')
      expect(persisted.timeline).toEqual(expect.arrayContaining([expect.objectContaining({ kind: 'message', message: expect.objectContaining({ id: '5', text: '游标原子消息' }) })]))
    })
  })

  it('replays a ready draft event after refresh without persisting its confirmation token', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '10',
      draftReadyEventId: '9',
      draft: { revision: 1, ready: true, project: { name: '测试人设', platform: 'wechat' } },
      timeline: [{ kind: 'draft', id: 'draft' }],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '9', event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, confirmation_token: 'secret-token', project: { name: '测试人设', platform: 'wechat' } } }) },
    ], signal))

    renderChat('/projects/create')

    const commit = await screen.findByRole('button', { name: '确认创建人设' })
    await waitFor(() => expect(streamMock.mock.calls[0]?.[3]).toEqual(expect.objectContaining({ lastEventId: '8' })))
    expect(commit).toBeEnabled()
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')).not.toHaveProperty('confirmation_token')
  })

  it('restores a ready draft and its confirmation credential from a compacted-history snapshot', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '40',
      draftReadyEventId: '9',
      draft: { revision: 1, ready: true, project: { name: '恢复人设', platform: 'wechat' } },
      timeline: [{ kind: 'draft', id: 'draft', eventId: 9 }],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      {
        id: '41',
        event: 'session.snapshot',
        data: JSON.stringify({
          status: 'running',
          busy: false,
          questions: [],
          draft_event_id: 9,
          draft: { revision: 1, ready: true, confirmation_token: 'snapshot-secret', project: { name: '恢复人设', platform: 'wechat' } },
        }),
      },
      { id: '42', event: 'assistant.message', data: JSON.stringify({ text: '草稿之后的新消息' }) },
    ], signal))

    renderChat('/projects/create')

    const commit = await screen.findByRole('button', { name: '确认创建人设' })
    expect(commit).toBeEnabled()
    const laterMessage = await screen.findByText('草稿之后的新消息')
    expect(commit.compareDocumentPosition(laterMessage) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fireEvent.click(commit)
    await waitFor(() => expect(interactiveMocks.commit).toHaveBeenCalledWith('session-1', 'snapshot-secret', expect.any(String), undefined))
    await waitFor(() => expect(screen.getByTestId('route-location')).toHaveTextContent('/chat?mode=creation&project_id=project-1'))
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')).not.toHaveProperty('confirmation_token')
  })

  it('shows live agent activity while a turn is running', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
    ], signal))

    renderChat()

    expect(await screen.findByText('Agent 本轮开始处理')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Agent 本轮开始处理')
  })

  it('shows the current agent tool activity when the runtime reports it', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'agent.activity', data: JSON.stringify({ name: 'get_project_profile' }) },
    ], signal))

    renderChat()

    expect(await screen.findByText('Agent 正在执行：get project profile')).toBeInTheDocument()
  })

  it('shows a failed draft staging call as an error instead of completed work', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'agent.activity', data: JSON.stringify({ call_id: 'call-stage', name: 'stage_project_creator_draft' }) },
      { id: '2', event: 'tool.result', data: JSON.stringify({ call_id: 'call-stage', ok: false, code: 'SESSION_NOT_FOUND', message: 'stage project creator draft: interactive session not found' }) },
      { id: '3', event: 'turn.end', data: JSON.stringify({ turn: 1, reason: { kind: 'completed' } }) },
    ], signal))

    renderChat()

    expect(await screen.findByText('画像草稿暂存失败')).toBeInTheDocument()
    expect(screen.getByText('创作会话已失效，人设尚未创建')).toBeInTheDocument()
    expect(screen.queryByText('Agent 正在执行：stage project creator draft')).not.toBeInTheDocument()
  })

  it('does not call a turn successful when one tool result failed', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'agent.activity', data: JSON.stringify({ call_id: 'call-read', name: 'get_project_profile' }) },
      { id: '2', event: 'tool.result', data: JSON.stringify({ call_id: 'call-read', ok: false, code: 'UPSTREAM_FAILED', message: 'profile read failed' }) },
      { id: '3', event: 'turn.end', data: JSON.stringify({ turn: 1, reason: { kind: 'completed' } }) },
    ], signal))

    renderChat()

    expect(await screen.findByText('Agent 本轮发生错误')).toBeInTheDocument()
    expect(screen.getByText('Agent 工具执行失败')).toBeInTheDocument()
    expect(screen.queryByText('Agent 本轮已完成')).not.toBeInTheDocument()
  })

  it('scopes tool failures to the turn that produced them', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'agent.activity', data: JSON.stringify({ call_id: 'call-failed', name: 'get_project_profile' }) },
      { id: '3', event: 'tool.result', data: JSON.stringify({ turn: 1, call_id: 'call-failed', ok: false, code: 'UPSTREAM_FAILED', message: 'profile read failed' }) },
      { id: '4', event: 'turn.end', data: JSON.stringify({ turn: 1, reason: { kind: 'completed' } }) },
      { id: '5', event: 'turn.start', data: JSON.stringify({ turn: 2 }) },
      { id: '6', event: 'agent.activity', data: JSON.stringify({ call_id: 'call-succeeded', name: 'update_project_profile' }) },
      { id: '7', event: 'tool.result', data: JSON.stringify({ turn: 2, call_id: 'call-succeeded', ok: true, message: 'profile updated' }) },
      { id: '8', event: 'turn.end', data: JSON.stringify({ turn: 2, reason: { kind: 'completed' } }) },
    ], signal))

    renderChat()

    await waitFor(() => {
      expect(screen.getAllByText('Agent 本轮发生错误')).toHaveLength(1)
      expect(screen.getAllByText('Agent 本轮已完成')).toHaveLength(1)
    })
  })

  it('marks a turn failed when a tool result has no activity entry', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'tool.result', data: JSON.stringify({ turn: 1, call_id: 'call-hidden', ok: false, code: 'UPSTREAM_FAILED', message: 'profile read failed' }) },
      { id: '3', event: 'turn.end', data: JSON.stringify({ turn: 1, reason: { kind: 'completed' } }) },
    ], signal))

    renderChat()

    expect(await screen.findByText('Agent 本轮发生错误')).toBeInTheDocument()
    expect(screen.queryByText('Agent 本轮已完成')).not.toBeInTheDocument()
  })

  it('keeps the agent activity history and completed state in the chat timeline', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'agent.activity', data: JSON.stringify({ name: 'get_project_profile' }) },
      { id: '3', event: 'agent.activity', data: JSON.stringify({ name: 'update_project_profile' }) },
      { id: '4', event: 'turn.end', data: JSON.stringify({ turn: 1, reason: { kind: 'completed' } }) },
    ], signal))
    const { container } = renderChat()

    await screen.findByText('Agent 本轮已完成')
    const content = container.textContent ?? ''
    expect(content.indexOf('Agent 本轮开始处理')).toBeLessThan(content.indexOf('Agent 正在执行：get project profile'))
    expect(content.indexOf('Agent 正在执行：get project profile')).toBeLessThan(content.indexOf('Agent 正在执行：update project profile'))
    expect(content.indexOf('Agent 正在执行：update project profile')).toBeLessThan(content.indexOf('Agent 本轮已完成'))
    await waitFor(() => expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').timeline).toEqual(expect.arrayContaining([
      expect.objectContaining({ kind: 'activity', eventId: 1, status: 'completed' }),
      expect.objectContaining({ kind: 'activity', eventId: 2, status: 'completed' }),
      expect.objectContaining({ kind: 'activity', eventId: 3, status: 'completed' }),
      expect.objectContaining({ kind: 'activity', eventId: 4, status: 'completed' }),
    ])))
  })

  it('shows aborted DSH turns as stopped', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'turn.end', data: JSON.stringify({ turn: 1, reason: { kind: 'aborted' } }) },
    ], signal))
    renderChat()

    expect(await screen.findByText('Agent 本轮已停止')).toBeInTheDocument()
    expect(screen.getByText('已停止')).toBeInTheDocument()
  })

  it('shows only the latest tool as running while the agent continues its turn', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'agent.activity', data: JSON.stringify({ name: 'get_project_profile' }) },
      { id: '3', event: 'agent.activity', data: JSON.stringify({ name: 'update_project_profile' }) },
    ], signal))
    renderChat()

    const previousActivity = await screen.findByText('Agent 正在执行：get project profile')
    const currentActivity = await screen.findByText('Agent 正在执行：update project profile')
    expect(previousActivity.parentElement).toHaveTextContent('已完成')
    expect(currentActivity.parentElement).toHaveTextContent('进行中')
  })

  it('keeps the current agent activity visible above the composer as history grows', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'agent.activity', data: JSON.stringify({ name: 'update_project_profile' }) },
      ...Array.from({ length: 20 }, (_, index) => ({
        id: String(index + 3), event: 'assistant.message', data: JSON.stringify({ text: `历史记录 ${index + 1}` }),
      })),
    ], signal))
    renderChat()

    const liveStatus = await screen.findByRole('status')
    expect(liveStatus).toHaveTextContent('Agent 正在执行：update project profile')
    expect(liveStatus.closest('[aria-live="polite"]')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' }).parentElement?.parentElement?.parentElement).toContainElement(liveStatus)
  })

  it('marks activity complete when an idle status is the runtime end signal', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'agent.activity', data: JSON.stringify({ name: 'get_project_profile' }) },
      { id: '3', event: 'session.status', data: JSON.stringify({ status: 'idle' }) },
    ], signal))
    renderChat()

    const activity = await screen.findByText('Agent 正在执行：get project profile')
    await waitFor(() => expect(activity.parentElement).toHaveTextContent('已完成'))
  })

  it('clears a stale running activity from a resumed session snapshot', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '4',
      timeline: [{ kind: 'activity', id: 'activity-3', eventId: 3, status: 'running', text: 'Agent 正在执行：get project profile' }],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '4', event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: false, questions: [] }) },
    ], signal))
    renderChat('/projects/create')

    const activity = await screen.findByText('Agent 正在执行：get project profile')
    await waitFor(() => expect(activity.parentElement).toHaveTextContent('已完成'))
    expect(activity.closest('[role="status"]')).not.toBeInTheDocument()
  })

  it('reconciles an idle snapshot as ready and completes stale activity', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({
      sessionId: 'session-1',
      lastEventId: '4',
      timeline: [{ kind: 'activity', id: 'activity-3', eventId: 3, status: 'running', text: 'Agent 正在执行：get project profile' }],
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '4', event: 'session.snapshot', data: JSON.stringify({ status: 'idle', busy: false, questions: [] }) },
    ], signal))
    renderChat('/projects/create')

    const activity = await screen.findByText('Agent 正在执行：get project profile')
    await waitFor(() => expect(activity.parentElement).toHaveTextContent('已完成'))
    expect(screen.getByText('准备就绪')).toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows a waiting state without a running spinner', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'question.opened', data: JSON.stringify({ call_id: 'call-1', questions: [{ id: 'topic', question: '创作主题是什么？' }] }) },
    ], signal))
    renderChat()

    const status = (await screen.findByText('Agent 正在等待你的回答')).closest('[role="status"]')
    expect(status).not.toBeNull()
    expect(status!).toHaveTextContent('Agent 正在等待你的回答')
    expect(status!).toHaveTextContent('等待中')
    expect(status!.querySelector('.animate-spin')).not.toBeInTheDocument()
  })

  it('transitions a waiting activity to a fresh running state after an answer', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'question.opened', data: JSON.stringify({ call_id: 'call-1', questions: [{ id: 'topic', question: '创作主题是什么？' }] }) },
      { id: '3', event: 'question.answered', data: JSON.stringify({ call_id: 'call-1', answers: [{ id: 'topic', selected: ['回答'] }] }) },
      { id: '4', event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: true, questions: [] }) },
    ], signal))
    renderChat()

    const question = await screen.findByText('创作主题是什么？')
    expect(question).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('Agent 正在工作…')).toBeInTheDocument())
    const content = document.body.textContent ?? ''
    expect(content).toContain('Agent 正在等待你的回答')
    expect(content.indexOf('Agent 正在等待你的回答')).toBeLessThan(content.indexOf('Agent 正在工作…'))
    await waitFor(() => {
      const timeline = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').timeline
      expect(timeline).toEqual(expect.arrayContaining([
        expect.objectContaining({ text: 'Agent 正在等待你的回答', status: 'completed' }),
        expect.objectContaining({ text: 'Agent 正在工作…', status: 'running' }),
      ]))
    })
    expect(screen.getByRole('status')).toHaveTextContent('Agent 正在工作…')
  })

  it('reconciles a started turn with a resumed busy snapshot without duplicating activity', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: true, questions: [] }) },
    ], signal))
    renderChat('/projects/create')

    await waitFor(() => {
      const timeline = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').timeline
      expect(timeline.filter((item: { kind: string; status?: string }) => item.kind === 'activity' && (item.status === 'started' || item.status === 'running'))).toHaveLength(1)
    })
  })

  it('reconciles a started turn with a waiting snapshot without duplicating activity', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'session.snapshot', data: JSON.stringify({ status: 'waiting_answer', busy: false, pending_call_id: 'call-1', questions: [{ id: 'topic', question: '创作主题是什么？' }] }) },
    ], signal))
    renderChat('/projects/create')

    await screen.findByText('创作主题是什么？')
    await waitFor(() => {
      const timeline = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').timeline
      expect(timeline.filter((item: { kind: string; status?: string }) => item.kind === 'activity' && item.status === 'waiting')).toHaveLength(1)
    })
  })

  it('marks active timeline entries as failed when the runtime reports an error', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'turn.start', data: JSON.stringify({ turn: 1 }) },
      { id: '2', event: 'agent.activity', data: JSON.stringify({ name: 'get_project_profile' }) },
      { id: '3', event: 'session.error', data: JSON.stringify({ message: '运行失败' }) },
    ], signal))
    renderChat()

    const activity = await screen.findByText('Agent 正在执行：get project profile')
    await waitFor(() => expect(activity.parentElement).toHaveTextContent('出错'))
  })

  it('preserves the conversation timeline after a terminal runtime error and refresh', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'assistant.message', data: JSON.stringify({ text: '错误前的聊天记录' }) },
      { id: '2', event: 'agent.activity', data: JSON.stringify({ name: 'get_project_profile' }) },
      { id: '3', event: 'session.error', data: JSON.stringify({ message: '运行失败' }) },
    ], signal))
    const first = renderChat()

    expect(await screen.findByText('运行失败')).toBeInTheDocument()
    first.unmount()
    streamMock.mockClear()
    renderChat('/projects/create')

    expect(await screen.findByText('错误前的聊天记录')).toBeInTheDocument()
    expect(screen.getByText('Agent 正在执行：get project profile')).toBeInTheDocument()
    expect(screen.getByText('出错')).toBeInTheDocument()
    expect(streamMock).not.toHaveBeenCalled()
  })

  it('offers server checkpoint recovery when a ready draft receives a terminal runtime event', async () => {
    assistantMocks.latestCheckpoint.mockResolvedValue({ id: 'terminal-checkpoint', summary: '已完成画像', status: 'ready', updatedAt: '' })
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { id: '1', event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, project: { name: '已完成画像' } } }) },
      { id: '2', event: 'session.error', data: JSON.stringify({ message: '运行失败' }) },
    ], signal))

    renderChat('/projects/create')

    expect(await screen.findByText('已完成画像')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '恢复已保存草稿' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新开始创建' })).toBeInTheDocument()
    const saved = JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')
    expect(saved).not.toHaveProperty('sessionId')
    expect(saved.draftNeedsReconfirmation).toBe(true)
  })

  it('keeps the composer disabled when a resumed session snapshot is busy', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([{ event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: true, questions: [] }) }], signal)
    })
    renderChat('/projects/create')

    expect(await screen.findByText('Agent 正在工作…')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Agent 正在工作…')
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('restores pending questions from a resumed session snapshot', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([{
        event: 'session.snapshot',
        data: JSON.stringify({ status: 'waiting_answer', busy: false, pending_call_id: 'call-1', questions: [{ id: 'topic', question: '创作主题是什么？' }] }),
      }], signal)
    })
    renderChat('/projects/create')

    expect(await screen.findByText('创作主题是什么？')).toBeInTheDocument()
    expect(screen.getAllByText('等待回答')).toHaveLength(2)
    expect(screen.getByText('Agent 正在等待你的回答')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('keeps the session and offers reconnection after the SSE retry budget is exhausted', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock
      .mockImplementationOnce(() => { throw new Error('SSE connection failed after 7 attempts') })
      .mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
        { event: 'session.ready', data: JSON.stringify({ session_id: 'session-1' }) },
      ], signal))
    renderChat('/projects/create')

    const reconnect = await screen.findByRole('button', { name: '重新连接' })
    expect(interactiveMocks.close).not.toHaveBeenCalled()
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').sessionId).toBe('session-1')

    fireEvent.click(reconnect)
    await waitFor(() => expect(streamMock).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled())
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('keeps a retryable session resumable after a refresh', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce(() => { throw new Error('SSE connection failed after 7 attempts') })
    const first = renderChat('/projects/create')

    await screen.findByRole('button', { name: '重新连接' })
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}')).not.toHaveProperty('terminalState')
    first.unmount()

    streamMock.mockClear()
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: false, questions: [] }) },
    ], signal))
    renderChat('/projects/create')

    await waitFor(() => expect(streamMock).toHaveBeenCalledWith('session-1', 'test-token', expect.any(AbortSignal), expect.objectContaining({ lastEventId: '4' })))
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('keeps a resumed composer disabled when the persisted session was mid-turn', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '7', turnActive: true }))
    let release: (() => void) | undefined
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => (async function* () {
      yield { event: 'session.ready', data: JSON.stringify({ session_id: 'session-1' }) }
      await new Promise<void>((resolve) => {
        release = resolve
        signal?.addEventListener('abort', () => resolve(), { once: true })
      })
    })())
    renderChat('/projects/create')

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(streamMock).toHaveBeenCalled())
    expect(composer).toBeDisabled()
    release?.()
  })

  it('reuses the persisted create key when a page refresh interrupts session creation', async () => {
    let resolveFirst: ((value: { id: string; agent_id: string; status: string }) => void) | undefined
    interactiveMocks.createSession
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ id: 'session-1', agent_id: agent.id, status: 'running' })

    const first = renderChat('/projects/create')
    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledTimes(1))
    const firstKey = interactiveMocks.createSession.mock.calls[0]?.[3]
    expect(typeof firstKey).toBe('string')
    first.unmount()

    renderChat('/projects/create')
    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledTimes(2))
    expect(interactiveMocks.createSession.mock.calls[1]?.[3]).toBe(firstKey)
    resolveFirst?.({ id: 'session-1', agent_id: agent.id, status: 'running' })
  })

  it('starts an existing project update session with its project context', async () => {
    interactiveMocks.createSession.mockResolvedValueOnce({ id: 'session-1', agent_id: agent.id, intent: 'update-project-profile', project_id: 'project-9', project_name: '茶品牌', status: 'running' })
    renderChat('/chat?mode=profile&project_id=project-9', { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    fireEvent.change(composer, { target: { value: '更新这个人设的画像' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledWith('project-creator', 'update-project-profile', 'project-9', expect.any(String)))
    expect(await screen.findByText('更新现有人设画像')).toBeInTheDocument()
    expect(screen.getByText(/茶品牌/)).toBeInTheDocument()
  })

  it('allows only one user message request while the previous message is in flight', async () => {
    let resolveMessage: (() => void) | undefined
    interactiveMocks.message.mockImplementationOnce(() => new Promise<void>((resolve) => { resolveMessage = () => resolve() }))
    renderChat(undefined, { initialMessage: false })
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())

    fireEvent.change(composer, { target: { value: '第一条消息' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })
    fireEvent.change(composer, { target: { value: '第二条消息' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(interactiveMocks.message).toHaveBeenCalledTimes(1))
    expect(interactiveMocks.message).toHaveBeenCalledWith('session-1', '第一条消息')
    resolveMessage?.()
  })

  it('keeps the composer disabled until the runtime reports the turn end', async () => {
    let releaseTurn: (() => void) | undefined
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => (async function* () {
      await new Promise<void>((resolve) => {
        releaseTurn = resolve
        signal?.addEventListener('abort', () => resolve(), { once: true })
      })
      yield { event: 'turn.start', data: JSON.stringify({ turn: 1 }) }
      await new Promise<void>((resolve) => signal?.addEventListener('abort', () => resolve(), { once: true }))
    })())
    renderChat(undefined, { initialMessage: false })
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())

    fireEvent.change(composer, { target: { value: '触发一轮' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })
    await waitFor(() => expect(interactiveMocks.message).toHaveBeenCalledWith('session-1', '触发一轮'))
    releaseTurn?.()
    await waitFor(() => expect(composer).toBeDisabled())
  })

  it('persists an idle status as the terminal turn state even without an event id', async () => {
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1', lastEventId: '7', turnActive: true }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal: AbortSignal | undefined, options: { onEvent?: (event: InteractiveStreamEvent) => void }) => (async function* () {
      const event = { event: 'session.status', data: JSON.stringify({ status: 'idle' }) }
      options.onEvent?.(event)
      yield event
      await new Promise<void>((resolve) => signal?.addEventListener('abort', () => resolve(), { once: true }))
    })())
    renderChat(undefined, { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').turnActive).toBe(false))
    expect(composer).toBeEnabled()
  })

  it('disables question controls optimistically and restores the card when answering fails', async () => {
    let rejectAnswer: ((error: Error) => void) | undefined
    interactiveMocks.answer.mockImplementationOnce(() => new Promise((_, reject) => {
      rejectAnswer = reject
    }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'question.opened', data: JSON.stringify({ call_id: 'call-1', questions: [{ id: 'choice', question: '选择平台', options: [{ label: '微信' }] }] }) },
    ], signal))
    renderChat()

    await waitFor(() => expect(screen.getByText('选择平台')).toBeInTheDocument())
    const radio = screen.getByRole('radio', { name: '微信' })
    fireEvent.click(radio)
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))
    await waitFor(() => expect(screen.getByRole('textbox', { name: '问题补充说明' })).toBeDisabled())
    rejectAnswer?.(new Error('answer failed'))
    await waitFor(() => expect(screen.getByText('选择平台')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByRole('button', { name: '提交回答' })).toBeInTheDocument())
    expect(screen.getByText('answer failed')).toBeInTheDocument()
    expect(screen.queryByText('会话需要重新开始')).not.toBeInTheDocument()
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').turnActive).toBe(false)
  })

  it('clears the persisted turn lock when a message enqueue fails', async () => {
    interactiveMocks.message.mockRejectedValueOnce(new Error('message failed'))
    renderChat(undefined, { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())
    fireEvent.change(composer, { target: { value: '发送失败的消息' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(interactiveMocks.message).toHaveBeenCalledWith('session-1', '发送失败的消息'))
    await waitFor(() => expect(screen.getByText('message failed')).toBeInTheDocument())
    expect(composer).toBeEnabled()
    expect(composer).toHaveValue('发送失败的消息')
    expect(JSON.parse(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none') ?? '{}').turnActive).toBe(false)
  })

  it('explains a busy-session conflict and restores the unsent message', async () => {
    interactiveMocks.message.mockRejectedValueOnce({
      message: 'Request failed with status code 409',
      response: { status: 409, data: { code: 40900, error_code: 'interactive_session_busy', msg: 'a question batch is pending' } },
    })
    renderChat(undefined, { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())
    fireEvent.change(composer, { target: { value: '我的补充说明' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })

    expect(await screen.findByText('上一条消息仍在处理中，当前输入没有发送成功，请稍后重试。')).toBeInTheDocument()
    expect(composer).toHaveValue('我的补充说明')
    expect(screen.queryByText('会话需要重新开始')).not.toBeInTheDocument()
  })

  it('describes checkpoint conflicts as session state conflicts, not profile updates', async () => {
    assistantMocks.message.mockRejectedValueOnce({
      message: 'Request failed with status code 409',
      response: { status: 409, data: { code: 40900, error_code: 'assistant_checkpoint_conflict', msg: 'assistant checkpoint changed' } },
    })
    renderChat(undefined, { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())
    fireEvent.change(composer, { target: { value: '继续补充画像' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })

    expect(await screen.findByText('会话草稿已在其他位置更新。你的输入已保留，请重新连接并检查最新进度后继续。')).toBeInTheDocument()
    expect(composer).toHaveValue('继续补充画像')
    expect(screen.queryByText(/画像已在其他位置更新/)).not.toBeInTheDocument()
  })

  it('offers recovery when a message is rejected because the session has closed', async () => {
    assistantMocks.message.mockRejectedValueOnce({
      message: 'Request failed with status code 410',
      response: { status: 410, data: { code: 41000, error_code: 'interactive_session_closed', msg: 'interactive session is closed' } },
    })
    renderChat(undefined, { initialMessage: false })

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())
    fireEvent.change(composer, { target: { value: '画像更新说明' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })

    expect(await screen.findByText('原会话已失效')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /恢复.*草稿|恢复本地材料/ })).toBeInTheDocument()
    expect(composer).toHaveValue('画像更新说明')
  })

  it('clears a single-select option when the user enters custom text', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'question.opened', data: JSON.stringify({ call_id: 'call-single', questions: [{ id: 'choice', question: '选择平台', options: [{ label: '微信' }] }] }) },
    ], signal))
    renderChat()

    await waitFor(() => expect(screen.getByText('选择平台')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('radio', { name: /微信/ }))
    fireEvent.change(screen.getByRole('textbox', { name: '问题补充说明' }), { target: { value: '小红书' } })
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))
    await waitFor(() => expect(interactiveMocks.answer).toHaveBeenCalledWith('session-1', 'call-single', [{ id: 'choice', selected: [], custom: '小红书' }]))
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
  })

  it('keeps the composer locked after an answer until the runtime turn ends', async () => {
    let releaseTurn: (() => void) | undefined
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => (async function* () {
      yield { event: 'question.opened', data: JSON.stringify({ call_id: 'call-lock', questions: [{ id: 'choice', question: '选择平台', options: [{ label: '微信' }] }] }) }
      await new Promise<void>((resolve) => {
        releaseTurn = resolve
        signal?.addEventListener('abort', () => resolve(), { once: true })
      })
      yield { event: 'turn.start', data: JSON.stringify({ turn: 1 }) }
      yield { event: 'turn.end', data: JSON.stringify({ turn: 1, reason: { kind: 'completed' } }) }
      await new Promise<void>((resolve) => signal?.addEventListener('abort', () => resolve(), { once: true }))
    })())
    renderChat()

    await waitFor(() => expect(screen.getByText('选择平台')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('radio', { name: '微信' }))
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))
    const composer = screen.getByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(interactiveMocks.answer).toHaveBeenCalledWith('session-1', 'call-lock', [{ id: 'choice', selected: ['微信'] }]))
    expect(composer).toBeDisabled()

    releaseTurn?.()
    await waitFor(() => expect(composer).toBeEnabled())
  })

  it('keeps earlier question batches reviewable when the agent opens another batch', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'question.opened', data: JSON.stringify({ call_id: 'call-first', questions: [{ id: 'first', question: '第一批问题', options: [{ label: '第一批答案' }] }] }) },
      { event: 'question.opened', data: JSON.stringify({ call_id: 'call-second', questions: [{ id: 'second', question: '第二批问题', options: [{ label: '第二批答案' }] }] }) },
    ], signal))
    renderChat()

    await waitFor(() => expect(screen.getByText('第一批问题')).toBeInTheDocument())
    expect(screen.getByText('第二批问题')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio', { name: '第一批答案' }))
    fireEvent.click(screen.getAllByRole('button', { name: '提交回答' })[0])

    await waitFor(() => expect(interactiveMocks.answer).toHaveBeenCalledWith('session-1', 'call-first', [{ id: 'first', selected: ['第一批答案'] }]))
    expect(screen.getByText('第一批问题')).toBeInTheDocument()
    expect(screen.getByText('第二批问题')).toBeInTheDocument()
  })

  it('disables unanswered cards after the temporary session closes', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'question.opened', data: JSON.stringify({ call_id: 'call-ended', questions: [{ id: 'choice', question: '会话结束前的问题', options: [{ label: '回答' }] }] }) },
      { event: 'session.status', data: JSON.stringify({ status: 'closed' }) },
    ], signal))
    renderChat()

    expect(await screen.findByText('会话已结束，重新开始后才能继续回答。')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: '回答' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '提交回答' })).toBeDisabled()
    expect(interactiveMocks.answer).not.toHaveBeenCalled()
  })

  it('retries a failed ready draft commit with the same token and idempotency key', async () => {
    interactiveMocks.commit
      .mockRejectedValueOnce(new Error('commit failed'))
      .mockResolvedValueOnce({ project: { id: 'project-1' } })
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'ready', data: '{}' },
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, confirmation_token: 'secret-token', project: { name: '测试人设', platform: 'wechat', instructions: '定位' } } }) },
    ], signal))
    sessionStorage.setItem('anban:assistant-session:test-user:profile:new:none', JSON.stringify({ sessionId: 'session-1' }))
    renderChat(undefined, { initialMessage: false })

    const commit = await screen.findByRole('button', { name: '确认创建人设' })
    expect(screen.queryByText('secret-token')).not.toBeInTheDocument()
    fireEvent.click(commit)
    await waitFor(() => expect(interactiveMocks.commit).toHaveBeenCalledTimes(1))
    expect(screen.getByText('commit failed')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认创建人设' }))
    await waitFor(() => expect(interactiveMocks.commit).toHaveBeenCalledTimes(2))
    expect(interactiveMocks.commit.mock.calls[1][1]).toBe('secret-token')
    expect(interactiveMocks.commit.mock.calls[1][2]).toBe(interactiveMocks.commit.mock.calls[0][2])
  })

  it('shows six profile summaries and missing or inferred fields before commit', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      {
        event: 'draft.ready',
        data: JSON.stringify({ draft: {
          revision: 1,
          ready: true,
          confirmation_token: 'secret-token',
          project: { name: '茶品牌', platform: 'wechat', instructions: '## 定位\n面向年轻消费者的茶品牌' },
          profile: { dimensions: {
            identity: { content: { brand: '**山岚茶作**' } },
            style: { content: { tone: '克制、清新' } },
            audience: { content: { age: '25-35 岁' } },
            platforms: { content: { primary: '微信公众号' } },
            preferences: { content: { avoid: '夸大功效' } },
            memory: { content: { products: '春茶系列' } },
          } },
          missing_fields: ['线下门店地址'],
          inferred_fields: ['主要受众为一二线城市白领'],
        } }),
      },
    ], signal))
    renderChat()

    await screen.findByRole('button', { name: '确认创建人设' })
    expect(screen.getByRole('heading', { name: '定位' })).toBeInTheDocument()
    expect(screen.getByText('山岚茶作')).toBeInTheDocument()
    expect(screen.queryByText('**山岚茶作**')).not.toBeInTheDocument()
    expect(screen.getByText('公众号')).toBeInTheDocument()
    expect(screen.queryByText('wechat')).not.toBeInTheDocument()
    expect(screen.getByText(/tone: 克制、清新/)).toBeInTheDocument()
    expect(screen.getByText(/age: 25-35 岁/)).toBeInTheDocument()
    expect(screen.getByText(/primary: 微信公众号/)).toBeInTheDocument()
    expect(screen.getByText(/avoid: 夸大功效/)).toBeInTheDocument()
    expect(screen.getByText(/products: 春茶系列/)).toBeInTheDocument()
    expect(screen.getByText('线下门店地址')).toBeInTheDocument()
    expect(screen.getByText('主要受众为一二线城市白领')).toBeInTheDocument()
  })

  it.each([
    ['', '/projects?edit=project-9'],
    ['&return_to=portraits', '/portraits/project-9'],
  ])('returns to the requested destination after confirming a profile update: %s', async (returnQuery, destination) => {
    interactiveMocks.commit.mockResolvedValueOnce({})
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 2, ready: true, confirmation_token: 'secret-token', memory_append: '补充：春茶系列将在四月上线。', project: { name: '茶品牌', platform: 'wechat' } } }) },
    ], signal))
    sessionStorage.setItem('anban:assistant-session:test-user:profile:project-9:none', JSON.stringify({ sessionId: 'session-1' }))
    renderChat(`/chat?agent=project-creator&intent=update-project-profile&project_id=project-9${returnQuery}`, { initialMessage: false })

    expect(await screen.findByText('将追加到人设记忆的内容')).toBeInTheDocument()
    expect(screen.getByText('补充：春茶系列将在四月上线。')).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: '确认更新画像' }))
    await waitFor(() => expect(interactiveMocks.commit).toHaveBeenCalledTimes(1))
    expect(assistantMocks.commit).toHaveBeenCalledWith('session-1', {
      confirmation_token: 'secret-token', idempotency_key: expect.any(String),
    })
    await waitFor(() => expect(screen.getByTestId('route-location')).toHaveTextContent(destination))
  })

  it('preserves a profile draft and offers recovery when confirmation hits a revision conflict', async () => {
    interactiveMocks.commit.mockRejectedValueOnce({
      message: 'Request failed with status code 409',
      response: { status: 409, data: { code: 40900, error_code: 'project_profile_revision_conflict', msg: 'profile revision changed' } },
    })
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 2, ready: true, confirmation_token: 'stale-token', project: { name: '茶品牌', platform: 'wechat' } } }) },
    ], signal))
    sessionStorage.setItem('anban:assistant-session:test-user:profile:project-9:none', JSON.stringify({ sessionId: 'session-1' }))
    renderChat('/chat?agent=project-creator&intent=update-project-profile&project_id=project-9', { initialMessage: false })

    fireEvent.click(await screen.findByRole('button', { name: '确认更新画像' }))

    expect(await screen.findAllByText(/画像版本已在其他位置更新/)).not.toHaveLength(0)
    expect(await screen.findByRole('button', { name: /恢复.*草稿|恢复本地材料/ })).toBeInTheDocument()
    expect(screen.getByText('已保存的画像草稿')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '等待重新确认' })).toBeDisabled()
  })

  it('keeps a replayed ready draft visible while waiting for its confirmation credential', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, project: { name: '测试人设', platform: 'wechat' } } }) },
    ], signal))
    renderChat()

    await waitFor(() => expect(screen.getByText('确认凭据尚未就绪，请等待会话继续同步。')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '确认创建人设' })).toBeDisabled()
    expect(interactiveMocks.close).not.toHaveBeenCalledWith('session-1')
  })

  it('prevents unload while the session is active and preserves it after unmount', async () => {
    const { unmount } = renderChat()
    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('进行中'))
    const event = new Event('beforeunload', { cancelable: true })
    const preventDefault = vi.spyOn(event, 'preventDefault')
    window.dispatchEvent(event)
    expect(preventDefault).toHaveBeenCalled()

    unmount()
    await waitFor(() => expect(interactiveMocks.close).not.toHaveBeenCalledWith('session-1'))
  })

  it('keeps the session persisted when the SSE stream has a transient network error', async () => {
    streamMock.mockImplementationOnce(async function* () {
      throw new Error('network error')
    })
    renderChat()

    await waitFor(() => expect(screen.getByText('network error')).toBeInTheDocument())
    expect(interactiveMocks.close).not.toHaveBeenCalledWith('session-1')
    expect(sessionStorage.getItem('anban:assistant-session:test-user:profile:new:none')).toContain('session-1')
    expect(screen.queryByText('会话需要重新开始')).not.toBeInTheDocument()
  })

  it('does not close a committed session during navigation', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, confirmation_token: 'secret-token', project: { name: '测试人设', platform: 'wechat' } } }) },
    ], signal))
    const { unmount } = renderChat()
    await screen.findByRole('button', { name: '确认创建人设' })
    fireEvent.click(screen.getByRole('button', { name: '确认创建人设' }))
    await waitFor(() => expect(interactiveMocks.commit).toHaveBeenCalledTimes(1))
    interactiveMocks.close.mockClear()
    unmount()
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)) })
    expect(interactiveMocks.close).not.toHaveBeenCalled()
  })
})
