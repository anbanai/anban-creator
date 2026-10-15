import { useCallback, useContext, useEffect, useRef, useState, type MutableRefObject } from 'react'
import { UNSAFE_NavigationContext, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Streamdown } from 'streamdown'
import {
  AlertTriangle,
  ArrowRight,
  Check,
  ChevronDown,
  Loader2,
  MessageSquare,
  RotateCcw,
  Send,
  Sparkles,
  SquarePen,
  UserRound,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { api } from '@/lib/api'
import type {
  InteractiveAgent,
  InteractiveDraft,
} from '@/lib/api/interactive'
import { streamInteractiveSession } from '@/lib/interactive-events'
import { useAuth } from '@/contexts/AuthContext'
import { DshQuestionComposer, type DshQuestion, type DshQuestionAnswer, type DshQuestionOption } from '@/components/dsh-user-questions/DshQuestionComposer'

type Message = { id: string; role: 'assistant' | 'user'; text: string }
type QuestionBatch = { callId: string; questions: DshQuestion[]; answers?: DshQuestionAnswer['answers'] }
type RunState = 'idle' | 'starting' | 'running' | 'waiting_answer' | 'error' | 'expired' | 'closed'
type JsonRecord = Record<string, unknown>

interface BlockableNavigator {
  push?: (...args: unknown[]) => unknown
  replace?: (...args: unknown[]) => unknown
  go?: (...args: unknown[]) => unknown
}

const DIMENSIONS = [
  ['identity', '定位'],
  ['style', '风格'],
  ['audience', '受众'],
  ['platforms', '平台'],
  ['preferences', '偏好与红线'],
  ['memory', '记忆'],
] as const

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function stringValue(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

function parseData(eventData: string): JsonRecord {
  try {
    const value: unknown = JSON.parse(eventData)
    return isRecord(value) ? value : { text: String(value) }
  } catch {
    return { text: eventData }
  }
}

type PersistedInteractiveSession = { sessionId: string; lastEventId?: string }

function interactiveSessionStorageKey(agentId: string, intent: string, projectId?: string): string {
  return `anban:interactive-session:${agentId}:${intent}:${projectId ?? 'new'}`
}

function readPersistedInteractiveSession(key: string): PersistedInteractiveSession | null {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return null
    const value: unknown = JSON.parse(raw)
    if (!isRecord(value) || typeof value.sessionId !== 'string' || !value.sessionId) return null
    return { sessionId: value.sessionId, lastEventId: typeof value.lastEventId === 'string' ? value.lastEventId : undefined }
  } catch {
    return null
  }
}

function persistInteractiveSession(key: string, session: PersistedInteractiveSession): void {
  try {
    sessionStorage.setItem(key, JSON.stringify(session))
  } catch {
    // Session persistence is an enhancement; the live SSE stream remains authoritative.
  }
}

function clearPersistedInteractiveSession(key: string): void {
  try {
    sessionStorage.removeItem(key)
  } catch {
    // Ignore storage policies such as private browsing.
  }
}

function normalizeDraft(raw: JsonRecord): InteractiveDraft {
  const draft = isRecord(raw.draft) ? raw.draft : raw
  const name = stringValue(draft.name)
  const fields = isRecord(draft.fields) ? draft.fields : undefined
  const project = isRecord(draft.project)
    ? draft.project
    : name
      ? { name, ...(fields ?? {}) }
      : undefined
  const rawProgress = isRecord(draft.progress) ? draft.progress : undefined
  const progress = rawProgress
    ? Object.fromEntries(Object.entries(rawProgress).filter((entry): entry is [string, string] => typeof entry[1] === 'string'))
    : undefined
  return {
    revision: typeof draft.revision === 'number' ? draft.revision : Number(draft.revision ?? 0),
    project,
    profile: isRecord(draft.profile) ? draft.profile : undefined,
    progress,
    missing_fields: stringArray(draft.missing_fields),
    inferred_fields: stringArray(draft.inferred_fields),
    ready: Boolean(draft.ready),
    name,
    fields,
    memory_append: stringValue(draft.memory_append),
  }
}

function parseQuestion(value: unknown): DshQuestion | null {
  if (!isRecord(value)) return null
  const id = stringValue(value.id)
  const question = stringValue(value.question)
  if (!id || !question) return null
  const options = Array.isArray(value.options)
    ? value.options.flatMap((option): DshQuestionOption[] => {
      if (!isRecord(option)) return []
      const label = stringValue(option.label)
      return label ? [{ label, description: stringValue(option.description) }] : []
    })
    : undefined
  return {
    id,
    question,
    header: stringValue(value.header),
    detail: stringValue(value.detail),
    options,
    multiSelect: typeof value.multiSelect === 'boolean' ? value.multiSelect : undefined,
    multi_select: typeof value.multi_select === 'boolean' ? value.multi_select : undefined,
  }
}

function parseAnswers(value: unknown): DshQuestionAnswer['answers'] {
  if (!Array.isArray(value)) return []
  return value.flatMap((answer): DshQuestionAnswer['answers'] => {
    if (!isRecord(answer)) return []
    const id = stringValue(answer.id)
    if (!id) return []
    return [{ id, selected: stringArray(answer.selected), custom: stringValue(answer.custom) }]
  })
}

function isRunState(value: unknown): value is RunState {
  return value === 'idle' || value === 'starting' || value === 'running' || value === 'waiting_answer' || value === 'error' || value === 'expired' || value === 'closed'
}

function useNavigationWarning(when: boolean, committedRef: MutableRefObject<boolean>, message: string) {
  const { navigator } = useContext(UNSAFE_NavigationContext)
  useEffect(() => {
    const blockable = navigator as unknown as BlockableNavigator
    if (!when) return
    const original = { push: blockable.push, replace: blockable.replace, go: blockable.go }
    const allow = () => committedRef.current || window.confirm(message)
    if (original.push) blockable.push = (...args) => allow() ? original.push?.(...args) : undefined
    if (original.replace) blockable.replace = (...args) => allow() ? original.replace?.(...args) : undefined
    if (original.go) blockable.go = (...args) => allow() ? original.go?.(...args) : undefined
    return () => {
      if (original.push && blockable.push) blockable.push = original.push
      if (original.replace && blockable.replace) blockable.replace = original.replace
      if (original.go && blockable.go) blockable.go = original.go
    }
  }, [committedRef, message, navigator, when])
}

function dimensionStatus(value?: string) {
  if (value === 'confirmed') return { label: '已确认', className: 'text-emerald-600 dark:text-emerald-400' }
  if (value === 'pending_confirmation') return { label: '待确认', className: 'text-amber-600 dark:text-amber-400' }
  if (value === 'collected') return { label: '已收集', className: 'text-primary' }
  return { label: '待开始', className: 'text-muted-foreground' }
}

export default function ChatPage() {
  const { token } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const requestedAgent = params.get('agent') || ''
  const intent = params.get('intent') || 'create-project'
  const projectId = params.get('project_id') || undefined
  const isProjectUpdate = intent === 'update-project-profile'
  const [selectedAgent, setSelectedAgent] = useState(requestedAgent || (location.pathname === '/projects/create' ? 'project-creator' : ''))
  const sessionStorageKey = interactiveSessionStorageKey(selectedAgent, intent, projectId)
  const [persistedSessionKey] = useState(sessionStorageKey)
  const [persistedSession] = useState(() => readPersistedInteractiveSession(sessionStorageKey))
  const [sessionId, setSessionId] = useState<string | null>(persistedSession?.sessionId ?? null)
  const [runState, setRunState] = useState<RunState>('idle')
  const [messages, setMessages] = useState<Message[]>([])
  const [questionBatches, setQuestionBatches] = useState<QuestionBatch[]>([])
  const [draft, setDraft] = useState<InteractiveDraft | null>(null)
  const [composer, setComposer] = useState('')
  const [streamError, setStreamError] = useState<string | null>(null)
  const [commitError, setCommitError] = useState<string | null>(null)
  const [committing, setCommitting] = useState(false)
  const [committed, setCommitted] = useState(false)
  const [sessionProject, setSessionProject] = useState<{ id?: string; name?: string } | null>(null)
  const [modeMenuOpen, setModeMenuOpen] = useState(false)
  const [restartNonce, setRestartNonce] = useState(0)
  const idempotencyKeyRef = useRef(crypto.randomUUID())
  const sessionRef = useRef<string | null>(persistedSession?.sessionId ?? null)
  const startPromiseRef = useRef<{ key: string; promise: Promise<void> } | null>(null)
  const runGenerationRef = useRef(0)
  const streamAbortRef = useRef<AbortController | null>(null)
  const disposedRef = useRef(false)
  const cleanupGenerationRef = useRef(0)
  const committedRef = useRef(false)
  const hasInputRef = useRef(false)
  const confirmationTokenRef = useRef<string | null>(null)

  const agentsQuery = useQuery({
    queryKey: ['interactive-agents'],
    queryFn: api.interactive.agents,
    staleTime: 30_000,
  })
  const agents = agentsQuery.data ?? []
  const selectableAgents = agents.filter((agent) => agent.available !== false)
  const activeAgent = selectableAgents.find((agent) => agent.id === selectedAgent)
  const pendingQuestions = questionBatches.filter((batch) => !batch.answers)
  const dirty = Boolean(sessionId && !committed)

  useNavigationWarning(dirty, committedRef, isProjectUpdate ? '当前项目画像更新尚未完成，离开后将丢失，确定离开吗？' : '当前创作项目创建尚未完成，离开后将丢失，确定离开吗？')

  const resetConversation = useCallback(() => {
    runGenerationRef.current += 1
    streamAbortRef.current?.abort()
    const previousId = sessionRef.current
    if (previousId && !committedRef.current) void api.interactive.close(previousId).catch(() => undefined)
    clearPersistedInteractiveSession(sessionStorageKey)
    sessionRef.current = null
    setSessionId(null)
    setRunState('idle')
    setMessages([])
    setQuestionBatches([])
    setDraft(null)
    setSessionProject(null)
    confirmationTokenRef.current = null
    setComposer('')
    setStreamError(null)
    setCommitError(null)
    setCommitted(false)
    committedRef.current = false
    hasInputRef.current = false
    setCommitting(false)
    idempotencyKeyRef.current = crypto.randomUUID()
  }, [sessionStorageKey])

  const terminateConversation = useCallback((state: Extract<RunState, 'error' | 'expired' | 'closed'>, message: string) => {
    runGenerationRef.current += 1
    streamAbortRef.current?.abort()
    const previousId = sessionRef.current
    sessionRef.current = null
    startPromiseRef.current = null
    setSessionId(null)
    setRunState(state)
    setStreamError(message)
    confirmationTokenRef.current = null
    clearPersistedInteractiveSession(sessionStorageKey)
    if (previousId && !committedRef.current) void api.interactive.close(previousId).catch(() => undefined)
  }, [isProjectUpdate, sessionStorageKey])

  const processEvent = useCallback((event: { event: string; data: string }) => {
    const data = parseData(event.data)
    switch (event.event) {
      case 'assistant.message':
        setMessages((items) => [...items, { id: stringValue(data.id) ?? crypto.randomUUID(), role: 'assistant', text: stringValue(data.text) ?? '' }])
        break
      case 'user.message':
        setMessages((items) => [...items, { id: stringValue(data.id) ?? crypto.randomUUID(), role: 'user', text: stringValue(data.text) ?? '' }])
        break
      case 'session.status':
        setRunState(isRunState(data.status) ? data.status : 'running')
        break
      case 'ready':
      case 'session.ready':
        setRunState('running')
        break
      case 'question.opened':
        setRunState('waiting_answer')
        {
          const callId = stringValue(data.call_id) ?? stringValue(data.callId) ?? ''
          const questions = Array.isArray(data.questions) ? data.questions.flatMap((value) => {
            const question = parseQuestion(value)
            return question ? [question] : []
          }) : []
          setQuestionBatches((batches) => batches.some((batch) => batch.callId === callId)
            ? batches
            : [...batches, { callId, questions }])
        }
        break
      case 'question.answered':
        setQuestionBatches((batches) => batches.map((batch) => batch.callId === (stringValue(data.call_id) ?? stringValue(data.callId) ?? '')
          ? { ...batch, answers: parseAnswers(data.answers ?? data.answer) }
          : batch))
        setRunState('running')
        break
      case 'draft.updated':
      case 'draft.ready':
        {
          const payload = isRecord(data.draft) ? data.draft : data
          const token = stringValue(payload.confirmation_token)
          const notReady = payload.ready === false || data.ready === false
          if (token) {
            confirmationTokenRef.current = token
          } else if (notReady) {
            confirmationTokenRef.current = null
          }
        }
        setDraft((previous) => {
          const next = normalizeDraft(data)
          return event.event === 'draft.ready' ? { ...next, ready: data.ready !== false } : { ...previous, ...next }
        })
        break
      case 'session.error':
        terminateConversation('error', stringValue(data.message) ?? '创作会话发生错误')
        break
      case 'expired':
      case 'session.expired':
        terminateConversation('expired', `创作会话已过期，请重新开始${isProjectUpdate ? '更新' : '创建'}。`)
        break
      case 'closed':
      case 'session.closed':
        if (!committedRef.current) terminateConversation('closed', `创作会话已结束，请重新开始${isProjectUpdate ? '更新' : '创建'}。`)
        break
      default:
        break
    }
  }, [isProjectUpdate, terminateConversation])

  const connectStream = useCallback(async (id: string, generation: number) => {
    streamAbortRef.current?.abort()
    const controller = new AbortController()
    streamAbortRef.current = controller
    try {
      if (!token) throw new Error('登录状态已失效，请重新登录。')
      const persisted = readPersistedInteractiveSession(sessionStorageKey)
      for await (const event of streamInteractiveSession(id, token, controller.signal, {
        lastEventId: persisted?.sessionId === id ? persisted.lastEventId : undefined,
        onEvent: (received) => {
          if (received.id) persistInteractiveSession(sessionStorageKey, { sessionId: id, lastEventId: received.id })
        },
      })) {
        if (disposedRef.current || generation !== runGenerationRef.current) return
        processEvent(event)
      }
      if (!controller.signal.aborted && generation === runGenerationRef.current) {
        terminateConversation('error', `与创作会话的连接已断开，请重新开始${isProjectUpdate ? '更新' : '创建'}。`)
      }
    } catch (error) {
      if (controller.signal.aborted || disposedRef.current || generation !== runGenerationRef.current) return
      terminateConversation('error', error instanceof Error ? error.message : '与创作会话连接失败')
    }
  }, [isProjectUpdate, processEvent, sessionStorageKey, terminateConversation, token])

  const startSession = useCallback((agent: InteractiveAgent) => {
    const key = `${agent.id}:${intent}:${projectId ?? ''}:${restartNonce}`
    if (startPromiseRef.current?.key === key) return startPromiseRef.current.promise
    const generation = ++runGenerationRef.current
    disposedRef.current = false
    setRunState('starting')
    setStreamError(null)
    const promise = (projectId
      ? api.interactive.createSession(agent.id, intent, projectId)
      : api.interactive.createSession(agent.id, intent)
    ).then(async (session) => {
      if (disposedRef.current || generation !== runGenerationRef.current) {
        await api.interactive.close(session.id).catch(() => undefined)
        return
      }
      sessionRef.current = session.id
      persistInteractiveSession(sessionStorageKey, { sessionId: session.id })
      setSessionProject({ id: session.project_id ?? projectId, name: session.project_name })
      setSessionId(session.id)
      setRunState((session.status as RunState) || 'running')
      await connectStream(session.id, generation)
    }).catch((error) => {
      if (disposedRef.current || generation !== runGenerationRef.current) return
      setStreamError(error instanceof Error ? error.message : `无法启动创作${isProjectUpdate ? '更新' : '创建'}会话`)
      setRunState('error')
    })
    startPromiseRef.current = { key, promise }
    return promise
  }, [connectStream, intent, isProjectUpdate, projectId, restartNonce, sessionStorageKey])

  useEffect(() => {
    if (sessionStorageKey !== persistedSessionKey || !persistedSession?.sessionId || !token) return
    const currentPersisted = readPersistedInteractiveSession(sessionStorageKey)
    if (currentPersisted?.sessionId !== persistedSession.sessionId) return
    const generation = ++runGenerationRef.current
    disposedRef.current = false
    setRunState('starting')
    sessionRef.current = persistedSession.sessionId
    void connectStream(persistedSession.sessionId, generation)
  }, [connectStream, persistedSession, persistedSessionKey, sessionStorageKey, token])

  useEffect(() => {
    if (!selectedAgent || agentsQuery.isLoading || !activeAgent || sessionId || ['starting', 'error', 'expired', 'closed'].includes(runState)) return
    void startSession(activeAgent)
  }, [activeAgent, agentsQuery.isLoading, runState, selectedAgent, sessionId, startSession])

  useEffect(() => {
    const handler = (event: BeforeUnloadEvent) => {
      if (!dirty) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [dirty])

  useEffect(() => {
    const cleanupGeneration = ++cleanupGenerationRef.current
    disposedRef.current = false
    return () => {
      disposedRef.current = true
      // Keep the server-owned session alive across refreshes. The next mount
      // resumes it from the persisted ID and Last-Event-ID.
      if (cleanupGeneration !== cleanupGenerationRef.current) return
      runGenerationRef.current += 1
      streamAbortRef.current?.abort()
    }
  }, [])

  const selectAgent = (agent: InteractiveAgent) => {
    if (agent.id === selectedAgent) return
    if ((hasInputRef.current || draft) && !window.confirm(`切换 Agent 会丢弃当前${isProjectUpdate ? '更新' : '创建'}草稿，确定继续吗？`)) return
    resetConversation()
    startPromiseRef.current = null
    setSelectedAgent(agent.id)
    setModeMenuOpen(false)
  }

  useEffect(() => {
    if (!agentsQuery.isLoading && selectedAgent && !activeAgent) {
      setSelectedAgent('')
    }
  }, [activeAgent, agentsQuery.isLoading, selectedAgent])

  const sendMessage = async () => {
    const text = composer.trim()
    if (!text || !sessionId || pendingQuestions.length > 0 || runState === 'starting' || committing) return
    setComposer('')
    hasInputRef.current = true
    confirmationTokenRef.current = null
    setDraft((value) => value ? { ...value, ready: false } : value)
    setCommitError(null)
    await api.interactive.message(sessionId, text).catch((error) => {
      setStreamError(error instanceof Error ? error.message : '消息发送失败')
    })
  }

  const submitAnswers = async (batch: QuestionBatch, answer: DshQuestionAnswer) => {
    if (batch.answers) return
    if (!sessionId || runState === 'expired' || runState === 'closed' || runState === 'error') {
      throw new Error('会话已结束，请重新开始后再回答。')
    }
    hasInputRef.current = true
    await api.interactive.answer(sessionId, batch.callId, answer.answers)
    setQuestionBatches((batches) => batches.map((item) => item.callId === batch.callId
      ? { ...item, answers: answer.answers }
      : item))
  }

  const commitProject = async () => {
    const confirmationToken = confirmationTokenRef.current
    if (!sessionId || !draft?.ready || !confirmationToken || committing) return
    setCommitting(true)
    setCommitError(null)
    try {
      const result = await api.interactive.commit(sessionId, confirmationToken, idempotencyKeyRef.current, draft?.memory_append)
      committedRef.current = true
      setCommitted(true)
      clearPersistedInteractiveSession(sessionStorageKey)
      const id = result.project?.id ?? (isProjectUpdate ? projectId : undefined)
      navigate(id ? `/projects?edit=${encodeURIComponent(id)}` : '/projects')
    } catch (error) {
      setCommitError(error instanceof Error ? error.message : `创作项目${isProjectUpdate ? '更新' : '创建'}失败，请重试。`)
    } finally {
      setCommitting(false)
    }
  }

  const canCompose = Boolean(sessionId) && pendingQuestions.length === 0 && runState !== 'starting' && !committing && !committed
  const emptyRegistry = !agentsQuery.isLoading && (agentsQuery.isError || selectableAgents.length === 0)

  return (
    <div className="mx-auto flex min-h-[calc(100dvh-4rem)] max-w-[1500px] flex-col gap-4 lg:min-h-[calc(100dvh-4rem)]">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-4">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground"><Sparkles className="size-4" /></div>
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold">创作工作台</p>
            <p className="truncate text-xs text-muted-foreground">与 Agent 对话，整理一个可长期使用的创作项目画像</p>
          </div>
        </div>
        <div className="relative flex items-center gap-2">
          <Badge variant={runState === 'error' || runState === 'expired' ? 'destructive' : 'outline'}>
            {runState === 'starting' ? '正在启动' : runState === 'waiting_answer' ? '等待回答' : runState === 'running' ? '进行中' : runState === 'error' ? '连接异常' : runState === 'expired' ? '已过期' : '准备就绪'}
          </Badge>
          <Button variant="outline" size="sm" disabled={selectableAgents.length === 0} onClick={() => setModeMenuOpen((open) => !open)} aria-expanded={modeMenuOpen}>
            <UserRound className="size-4" />
            {activeAgent?.display_name ?? '选择 Agent'}
            <ChevronDown className="size-3.5" />
          </Button>
          {modeMenuOpen && (
            <div className="absolute right-0 top-full z-20 mt-2 w-72 rounded-xl border border-border bg-popover p-2 shadow-lg" role="menu">
              {selectableAgents.map((agent) => (
                <button key={agent.id} type="button" role="menuitem" className="flex w-full items-start gap-3 rounded-lg px-3 py-2 text-left hover:bg-muted" onClick={() => selectAgent(agent)}>
                  <Sparkles className="mt-0.5 size-4 shrink-0 text-primary" />
                  <span className="min-w-0"><span className="block text-sm font-medium">{agent.display_name}</span><span className="mt-0.5 block text-xs text-muted-foreground">{agent.description}</span></span>
                </button>
              ))}
            </div>
          )}
        </div>
      </header>

      {isProjectUpdate && (
        <Alert className="border-primary/25 bg-primary/[0.035]">
          <SquarePen className="size-4" />
          <AlertTitle>更新现有项目画像</AlertTitle>
          <AlertDescription>
            正在更新 {sessionProject?.name || (projectId ? `项目 ${projectId}` : '当前创作项目')}，确认后会保存画像变更。
          </AlertDescription>
        </Alert>
      )}

      {emptyRegistry ? (
        <Card className="mx-auto mt-16 w-full max-w-xl">
          <CardHeader><CardTitle>创作项目创建 Agent 暂不可用</CardTitle><CardDescription>当前没有可用的交互式 Agent，可以继续使用已有的创作项目表单。</CardDescription></CardHeader>
          <CardContent><Button onClick={() => navigate('/projects?create=true')}><SquarePen className="size-4" />使用表单创建</Button></CardContent>
        </Card>
      ) : !selectedAgent ? (
        <section className="mx-auto flex w-full max-w-3xl flex-1 flex-col justify-center py-8">
          <div className="mb-8 max-w-xl"><p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">画像模式</p><h1 className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">从一段对话开始，建立你的创作底座</h1><p className="mt-3 text-sm leading-6 text-muted-foreground">选择一个 Agent。选定后会自动启动会话，你可以随时补充或修改。</p></div>
          <div className="grid gap-3 sm:grid-cols-2">
            {selectableAgents.map((agent) => <button key={agent.id} type="button" className="group rounded-xl border border-border bg-card p-5 text-left transition hover:border-primary/60 hover:shadow-sm" onClick={() => selectAgent(agent)}><div className="flex items-center justify-between"><span className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary"><Sparkles className="size-4" /></span><ArrowRight className="size-4 text-muted-foreground transition group-hover:translate-x-1 group-hover:text-primary" /></div><p className="mt-5 font-medium">{agent.display_name}</p><p className="mt-1 text-sm leading-5 text-muted-foreground">{agent.description}</p></button>)}
          </div>
        </section>
      ) : (
        <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
          <section className="flex min-h-0 flex-col rounded-xl border border-border bg-card">
            <div className="min-h-0 flex-1 space-y-5 overflow-y-auto p-4 sm:p-6" aria-live="polite">
              {messages.length === 0 && runState === 'starting' && <div className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在准备对话…</div>}
              {messages.map((message) => <div key={message.id} className={`flex gap-3 ${message.role === 'user' ? 'justify-end' : ''}`}><div className={`flex max-w-[min(720px,90%)] gap-3 ${message.role === 'user' ? 'flex-row-reverse' : ''}`}><div className={`mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}>{message.role === 'user' ? <UserRound className="size-3.5" /> : <MessageSquare className="size-3.5" />}</div><div className={`rounded-2xl px-4 py-3 text-sm leading-6 ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted/70'}`}><Streamdown>{message.text}</Streamdown></div></div></div>)}
              {questionBatches.map((batch) => <DshQuestionComposer key={batch.callId} questions={batch.questions} answers={batch.answers} submitted={Boolean(batch.answers)} disabled={!sessionId || runState === 'starting' || runState === 'expired' || runState === 'closed' || runState === 'error'} onSubmit={(answer) => submitAnswers(batch, answer)} />)}
              {draft?.ready && <ConfirmCard draft={draft} isProjectUpdate={isProjectUpdate} canCommit={Boolean(confirmationTokenRef.current)} committing={committing} error={commitError} onCommit={() => void commitProject()} onContinue={async () => { if (!sessionId) return; confirmationTokenRef.current = null; setDraft((value) => value ? { ...value, ready: false } : value); await api.interactive.message(sessionId, '继续编辑创作项目画像').catch(() => undefined) }} />}
              {streamError && <Alert variant="destructive"><AlertTriangle className="size-4" /><AlertTitle>会话需要重新开始</AlertTitle><AlertDescription className="flex flex-wrap items-center gap-3">{streamError}<Button size="sm" variant="outline" onClick={() => { resetConversation(); startPromiseRef.current = null; setRestartNonce((value) => value + 1) }}><RotateCcw className="size-4" />重新开始{isProjectUpdate ? '更新' : '创建'}</Button></AlertDescription></Alert>}
              {committed && <Alert><Check className="size-4 text-emerald-600" /><AlertTitle>{isProjectUpdate ? '创作项目画像已更新' : '创作项目已创建'}</AlertTitle><AlertDescription>正在打开创作项目详情。</AlertDescription></Alert>}
            </div>
            <div className="border-t border-border p-3 sm:p-4"><div className="flex items-end gap-2 rounded-xl border border-input bg-background p-2 focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/20"><Textarea value={composer} onChange={(event) => setComposer(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void sendMessage() } }} disabled={!canCompose} placeholder={pendingQuestions.length > 0 ? '请先完成上面的回答' : canCompose ? '补充你的创作项目想法…' : '正在准备会话…'} aria-label="发送消息" className="min-h-10 resize-none border-0 bg-transparent p-1 shadow-none focus-visible:ring-0" /><Button size="icon-md" onClick={() => void sendMessage()} disabled={!canCompose || !composer.trim()} aria-label="发送消息" title="发送消息"><Send className="size-4" /></Button></div></div>
          </section>
          <ProgressRail draft={draft} />
        </div>
      )}
    </div>
  )
}

function ProgressRail({ draft }: { draft: InteractiveDraft | null }) {
  return <aside className="order-first overflow-x-auto rounded-xl border border-border bg-card p-4 lg:order-last lg:overflow-visible"><div className="flex min-w-max gap-5 lg:min-w-0 lg:flex-col lg:gap-4"><div className="min-w-48 lg:min-w-0"><p className="text-sm font-semibold">创作项目画像</p><p className="mt-1 text-xs text-muted-foreground">六个维度逐步收集</p></div>{DIMENSIONS.map(([key, label]) => { const status = dimensionStatus(draft?.progress?.[key]); return <div key={key} className="min-w-28 border-l border-border pl-3 lg:min-w-0 lg:border-l-0 lg:border-t lg:pl-0 lg:pt-3"><div className="flex items-center justify-between gap-2"><span className="text-sm">{label}</span><span className={`text-xs ${status.className}`}>{status.label}</span></div><div className="mt-2 h-1 overflow-hidden rounded-full bg-muted"><div className={`h-full transition-all ${status.label === '待开始' ? 'w-0' : status.label === '已确认' ? 'w-full bg-emerald-500' : 'w-2/3 bg-primary'}`} /></div></div> })}</div></aside>
}

function ConfirmCard({ draft, isProjectUpdate, canCommit, committing, error, onCommit, onContinue }: { draft: InteractiveDraft; isProjectUpdate: boolean; canCommit: boolean; committing: boolean; error: string | null; onCommit: () => void; onContinue: () => void }) {
  const project = draft.project ?? {}
  const dimensions = isRecord(draft.profile?.dimensions) ? draft.profile.dimensions : {}
  return <Card className="border-emerald-500/30 bg-emerald-500/[0.035]"><CardHeader><CardTitle className="flex items-center gap-2"><Check className="size-4 text-emerald-600" />{isProjectUpdate ? '确认更新创作项目画像' : '确认创作项目画像'}</CardTitle><CardDescription>{isProjectUpdate ? '信息已整理完成，确认后会更新现有项目画像并保存变更。' : '信息已整理完成，确认后会创建创作项目并保存六维画像。'}</CardDescription></CardHeader><CardContent className="space-y-4"><div className="grid gap-2 rounded-lg border border-border bg-background/70 p-3 text-sm sm:grid-cols-2"><div><span className="text-xs text-muted-foreground">创作项目名称</span><p className="font-medium">{String(project.name ?? draft.name ?? '未命名创作项目')}</p></div><div><span className="text-xs text-muted-foreground">平台</span><p className="font-medium">{String(project.platform ?? '待设定')}</p></div><div className="sm:col-span-2"><span className="text-xs text-muted-foreground">定位</span><p className="mt-1 leading-5">{String(project.instructions ?? project.description ?? '已收集')}</p></div></div><div><p className="text-xs font-medium text-muted-foreground">六维画像摘要</p><div className="mt-2 grid gap-2 sm:grid-cols-2">{DIMENSIONS.map(([key, label]) => <div key={key} className="rounded-lg border border-border bg-background/60 p-3"><p className="text-xs text-muted-foreground">{label}</p><p className="mt-1 text-sm leading-5">{summarizeDimension(dimensions[key])}</p></div>)}</div></div>{(draft.missing_fields?.length || draft.inferred_fields?.length) ? <div className="grid gap-3 sm:grid-cols-2"><div><p className="text-xs font-medium text-muted-foreground">待补充字段</p><p className="mt-1 text-sm leading-5">{draft.missing_fields?.length ? draft.missing_fields.join('、') : '暂无'}</p></div><div><p className="text-xs font-medium text-muted-foreground">推断字段</p><p className="mt-1 text-sm leading-5">{draft.inferred_fields?.length ? draft.inferred_fields.join('、') : '暂无'}</p></div></div> : null}{draft.memory_append && <div className="rounded-lg border border-border bg-background/70 p-3"><p className="text-xs font-medium text-muted-foreground">将追加到项目记忆的内容</p><p className="mt-1 whitespace-pre-wrap text-sm leading-5">{draft.memory_append}</p></div>}{!canCommit && <p className="text-sm text-muted-foreground">确认凭据尚未就绪，请等待会话继续同步。</p>}{error && <p className="text-sm text-destructive">{error}</p>}<div className="flex flex-wrap gap-2"><Button onClick={onCommit} disabled={committing || !canCommit}>{committing && <Loader2 className="size-4 animate-spin" />}确认{isProjectUpdate ? '更新画像' : '创建创作项目'}</Button><Button variant="outline" onClick={onContinue} disabled={committing}><SquarePen className="size-4" />继续编辑</Button></div></CardContent></Card>
}

function summarizeDimension(value: unknown): string {
  if (!isRecord(value)) return '尚未整理'
  const content = isRecord(value.content) ? value.content : value
  const entries = Object.entries(content).filter(([, item]) => typeof item === 'string' || typeof item === 'number')
  if (entries.length === 0) return '尚未整理'
  return entries.slice(0, 3).map(([key, item]) => `${key}: ${String(item)}`).join('；')
}
