import { act, fireEvent, render as renderWithoutProviders, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ChatPage from './ChatPage'
import { createTestQueryClient } from '@/test/test-utils'
import type { InteractiveStreamEvent } from '@/lib/interactive-events'

const interactiveMocks = vi.hoisted(() => ({
  agents: vi.fn(),
  createSession: vi.fn(),
  message: vi.fn(),
  answer: vi.fn(),
  commit: vi.fn(),
  close: vi.fn(),
}))
const streamMock = vi.hoisted(() => vi.fn())

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ token: 'test-token' }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: { ...actual.api, interactive: interactiveMocks } }
})

vi.mock('@/lib/interactive-events', async () => {
  const actual = await vi.importActual<typeof import('@/lib/interactive-events')>('@/lib/interactive-events')
  return { ...actual, streamInteractiveSession: streamMock }
})

const agent = {
  id: 'project-creator',
  display_name: '画像模式',
  description: '创建创作项目',
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

function renderChat(path = '/projects/create') {
  const queryClient = createTestQueryClient()
  return renderWithoutProviders(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="*" element={<ChatPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ChatPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    sessionStorage.clear()
    interactiveMocks.agents.mockResolvedValue([agent])
    interactiveMocks.createSession.mockResolvedValue({ id: 'session-1', agent_id: agent.id, status: 'running' })
    interactiveMocks.message.mockResolvedValue({})
    interactiveMocks.answer.mockResolvedValue({})
    interactiveMocks.commit.mockResolvedValue({ project: { id: 'project-1' } })
    interactiveMocks.close.mockResolvedValue({})
    streamMock.mockImplementation((sessionId: string, token: string, signal?: AbortSignal) => {
      void sessionId
      void token
      return holdStream([], signal)
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('defaults the project creation route to 画像模式 and starts a session', async () => {
    renderChat('/projects/create')

    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledWith('project-creator', 'create-project', undefined, expect.any(String)))
    expect(screen.getByRole('button', { name: /画像模式/ })).toBeInTheDocument()
  })

  it('resumes a persisted session from its last SSE event without creating a new runtime', async () => {
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    renderChat('/projects/create')

    await waitFor(() => expect(streamMock).toHaveBeenCalledWith(
      'session-1',
      'test-token',
      expect.any(AbortSignal),
      expect.objectContaining({ lastEventId: '4' }),
    ))
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('does not enable a resumed session from SSE connectivity alone', async () => {
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
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
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([{ event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: false, questions: [] }) }], signal)
    })
    renderChat('/projects/create')

    await waitFor(() => expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled())
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('keeps the composer disabled when a resumed session snapshot is busy', async () => {
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([{ event: 'session.snapshot', data: JSON.stringify({ status: 'running', busy: true, questions: [] }) }], signal)
    })
    renderChat('/projects/create')

    expect(await screen.findByText('进行中')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('restores pending questions from a resumed session snapshot', async () => {
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal, options?: { onConnected?: () => void }) => {
      options?.onConnected?.()
      return holdStream([{
        event: 'session.snapshot',
        data: JSON.stringify({ status: 'waiting_answer', busy: false, pending_call_id: 'call-1', questions: [{ id: 'topic', question: '创作主题是什么？' }] }),
      }], signal)
    })
    renderChat('/projects/create')

    expect(await screen.findByText('创作主题是什么？')).toBeInTheDocument()
    expect(screen.getByText('等待回答')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '发送消息' })).toBeDisabled()
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('keeps the session and offers reconnection after the SSE retry budget is exhausted', async () => {
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '4' }))
    streamMock
      .mockImplementationOnce(() => { throw new Error('SSE connection failed after 7 attempts') })
      .mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
        { event: 'session.ready', data: JSON.stringify({ session_id: 'session-1' }) },
      ], signal))
    renderChat('/projects/create')

    const reconnect = await screen.findByRole('button', { name: '重新连接' })
    expect(interactiveMocks.close).not.toHaveBeenCalled()
    expect(JSON.parse(sessionStorage.getItem('anban:interactive-session:project-creator:create-project:new') ?? '{}').sessionId).toBe('session-1')

    fireEvent.click(reconnect)
    await waitFor(() => expect(streamMock).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByRole('textbox', { name: '发送消息' })).toBeEnabled())
    expect(interactiveMocks.createSession).not.toHaveBeenCalled()
  })

  it('keeps a resumed composer disabled when the persisted session was mid-turn', async () => {
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '7', turnActive: true }))
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
    renderChat('/chat?agent=project-creator&intent=update-project-profile&project_id=project-9')

    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledWith('project-creator', 'update-project-profile', 'project-9', expect.any(String)))
    expect(await screen.findByText('更新现有项目画像')).toBeInTheDocument()
    expect(screen.getByText(/茶品牌/)).toBeInTheDocument()
  })

  it('allows only one user message request while the previous message is in flight', async () => {
    let resolveMessage: (() => void) | undefined
    interactiveMocks.message.mockImplementationOnce(() => new Promise<void>((resolve) => { resolveMessage = () => resolve() }))
    renderChat()
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
    renderChat()
    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())

    fireEvent.change(composer, { target: { value: '触发一轮' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })
    await waitFor(() => expect(interactiveMocks.message).toHaveBeenCalledWith('session-1', '触发一轮'))
    releaseTurn?.()
    await waitFor(() => expect(composer).toBeDisabled())
  })

  it('persists an idle status as the terminal turn state even without an event id', async () => {
    sessionStorage.setItem('anban:interactive-session:project-creator:create-project:new', JSON.stringify({ sessionId: 'session-1', lastEventId: '7', turnActive: true }))
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal: AbortSignal | undefined, options: { onEvent?: (event: InteractiveStreamEvent) => void }) => (async function* () {
      const event = { event: 'session.status', data: JSON.stringify({ status: 'idle' }) }
      options.onEvent?.(event)
      yield event
      await new Promise<void>((resolve) => signal?.addEventListener('abort', () => resolve(), { once: true }))
    })())
    renderChat()

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(JSON.parse(sessionStorage.getItem('anban:interactive-session:project-creator:create-project:new') ?? '{}').turnActive).toBe(false))
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
    expect(JSON.parse(sessionStorage.getItem('anban:interactive-session:project-creator:create-project:new') ?? '{}').turnActive).toBe(false)
  })

  it('clears the persisted turn lock when a message enqueue fails', async () => {
    interactiveMocks.message.mockRejectedValueOnce(new Error('message failed'))
    renderChat()

    const composer = await screen.findByRole('textbox', { name: '发送消息' })
    await waitFor(() => expect(composer).toBeEnabled())
    fireEvent.change(composer, { target: { value: '发送失败的消息' } })
    fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(interactiveMocks.message).toHaveBeenCalledWith('session-1', '发送失败的消息'))
    await waitFor(() => expect(screen.getByText('message failed')).toBeInTheDocument())
    expect(composer).toBeEnabled()
    expect(JSON.parse(sessionStorage.getItem('anban:interactive-session:project-creator:create-project:new') ?? '{}').turnActive).toBe(false)
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
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, confirmation_token: 'secret-token', project: { name: '测试项目', platform: 'wechat', instructions: '定位' } } }) },
    ], signal))
    renderChat()

    const commit = await screen.findByRole('button', { name: '确认创建创作项目' })
    expect(screen.queryByText('secret-token')).not.toBeInTheDocument()
    fireEvent.click(commit)
    await waitFor(() => expect(interactiveMocks.commit).toHaveBeenCalledTimes(1))
    expect(screen.getByText('commit failed')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认创建创作项目' }))
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
          project: { name: '茶品牌', platform: 'wechat', instructions: '面向年轻消费者的茶品牌' },
          profile: { dimensions: {
            identity: { content: { brand: '山岚茶作' } },
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

    await screen.findByRole('button', { name: '确认创建创作项目' })
    expect(screen.getByText(/brand: 山岚茶作/)).toBeInTheDocument()
    expect(screen.getByText(/tone: 克制、清新/)).toBeInTheDocument()
    expect(screen.getByText(/age: 25-35 岁/)).toBeInTheDocument()
    expect(screen.getByText(/primary: 微信公众号/)).toBeInTheDocument()
    expect(screen.getByText(/avoid: 夸大功效/)).toBeInTheDocument()
    expect(screen.getByText(/products: 春茶系列/)).toBeInTheDocument()
    expect(screen.getByText('线下门店地址')).toBeInTheDocument()
    expect(screen.getByText('主要受众为一二线城市白领')).toBeInTheDocument()
  })

  it('previews memory additions and uses update confirmation copy', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 2, ready: true, confirmation_token: 'secret-token', memory_append: '补充：春茶系列将在四月上线。', project: { name: '茶品牌', platform: 'wechat' } } }) },
    ], signal))
    renderChat('/chat?agent=project-creator&intent=update-project-profile&project_id=project-9')

    expect(await screen.findByRole('button', { name: '确认更新画像' })).toBeInTheDocument()
    expect(screen.getByText('将追加到项目记忆的内容')).toBeInTheDocument()
    expect(screen.getByText('补充：春茶系列将在四月上线。')).toBeInTheDocument()
  })

  it('keeps a replayed ready draft visible while waiting for its confirmation credential', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, project: { name: '测试项目', platform: 'wechat' } } }) },
    ], signal))
    renderChat()

    await waitFor(() => expect(screen.getByText('确认凭据尚未就绪，请等待会话继续同步。')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '确认创建创作项目' })).toBeDisabled()
    expect(interactiveMocks.close).not.toHaveBeenCalledWith('session-1')
  })

  it('prevents unload while the session is active and preserves it after unmount', async () => {
    const { unmount } = renderChat()
    await waitFor(() => expect(interactiveMocks.createSession).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByText('进行中')).toBeInTheDocument())
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
    expect(sessionStorage.getItem('anban:interactive-session:project-creator:create-project:new')).toContain('session-1')
    expect(screen.queryByText('会话需要重新开始')).not.toBeInTheDocument()
  })

  it('does not close a committed session during navigation', async () => {
    streamMock.mockImplementationOnce((_sessionId: string, _token: string, signal?: AbortSignal) => holdStream([
      { event: 'draft.ready', data: JSON.stringify({ draft: { revision: 1, ready: true, confirmation_token: 'secret-token', project: { name: '测试项目', platform: 'wechat' } } }) },
    ], signal))
    const { unmount } = renderChat()
    await screen.findByRole('button', { name: '确认创建创作项目' })
    fireEvent.click(screen.getByRole('button', { name: '确认创建创作项目' }))
    await waitFor(() => expect(interactiveMocks.commit).toHaveBeenCalledTimes(1))
    interactiveMocks.close.mockClear()
    unmount()
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)) })
    expect(interactiveMocks.close).not.toHaveBeenCalled()
  })
})
