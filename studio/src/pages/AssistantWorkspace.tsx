import { useEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ChevronDown, Loader2, MessageSquare, Send, Sparkles, UserRound } from 'lucide-react'
import { Streamdown } from 'streamdown'
import { useLocation } from 'react-router-dom'
import { api } from '@/lib/api'
import type { AssistantModeID, AssistantWorkspace as AssistantWorkspaceState, AssistantProject } from '@/lib/api/assistant'
import { useAuth } from '@/contexts/AuthContext'
import { streamAssistantWorkspace, type InteractiveStreamEvent } from '@/lib/interactive-events'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { DshQuestionComposer, type DshQuestion, type DshQuestionAnswer } from '@/components/dsh-user-questions/DshQuestionComposer'

type Message = { id: string; role: 'user' | 'assistant'; text: string }
type QuestionBatch = { callId: string; questions: DshQuestion[]; answers?: DshQuestionAnswer['answers'] }
type Draft = Record<string, unknown>
type RouteState = { modeId?: AssistantModeID; projectId?: string; contentId?: string; checkpointId?: string; initialMessage?: string }
type PersistedWorkspace = { workspace: AssistantWorkspaceState; lastEventIds: Record<string, string | undefined> }

const WORKSPACE_STORAGE_KEY = 'anban:assistant-workspace'

const MODES: AssistantModeID[] = ['profile', 'creation', 'review']

function isMode(value: unknown): value is AssistantModeID {
  return value === 'profile' || value === 'creation' || value === 'review'
}
function stringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value : undefined
}

function parseEventData(event: InteractiveStreamEvent): Record<string, unknown> {
  if (typeof event.data !== 'string') return {}
  try {
    const value: unknown = JSON.parse(event.data)
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
  } catch {
    return {}
  }
}

function parseQuestions(value: unknown): DshQuestion[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return []
    const candidate = item as Record<string, unknown>
    const id = stringValue(candidate.id)
    const question = stringValue(candidate.question)
    if (!id || !question) return []
    const options = Array.isArray(candidate.options) ? candidate.options.flatMap((option) => {
      if (!option || typeof option !== 'object' || Array.isArray(option)) return []
      const item = option as Record<string, unknown>
      const label = stringValue(item.label)
      return label ? [{ label, description: stringValue(item.description), recommended: item.recommended === true }] : []
    }) : undefined
    return [{ id, question, header: stringValue(candidate.header), detail: stringValue(candidate.detail), options, multiSelect: candidate.multiSelect === true || candidate.multi_select === true }]
  })
}

function modeLabel(mode: AssistantModeID) {
  return mode === 'profile' ? '画像模式' : mode === 'creation' ? '创作模式' : '复盘模式'
}

function modePlaceholder(mode: AssistantModeID) {
  return mode === 'profile' ? '告诉我你想建立怎样的画像…' : mode === 'creation' ? '描述你想创作的内容，或告诉我你的目标…' : '告诉我你想从这件作品中复盘什么…'
}

function projectName(project?: AssistantProject) {
  return project?.name ? `${project.name}人设` : '新人设'
}

function readPersistedWorkspace(): PersistedWorkspace | null {
  try {
    const raw = sessionStorage.getItem(WORKSPACE_STORAGE_KEY)
    if (!raw) return null
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== 'object' || !('workspace' in value)) return null
    const candidate = value as Partial<PersistedWorkspace>
    if (!candidate.workspace || typeof candidate.workspace !== 'object' || !isMode(candidate.workspace.mode_id) || typeof candidate.workspace.id !== 'string') return null
    return { workspace: candidate.workspace as AssistantWorkspaceState, lastEventIds: candidate.lastEventIds ?? {} }
  } catch {
    return null
  }
}

export default function AssistantWorkspace() {
  const { token } = useAuth()
  const location = useLocation()
  const routeState = (location.state ?? {}) as RouteState
  const routeHasContext = Boolean(routeState.modeId || routeState.projectId || routeState.contentId || routeState.checkpointId || routeState.initialMessage)
  const persisted = readPersistedWorkspace()
  const bootstrapQuery = useQuery({ queryKey: ['assistant-bootstrap'], queryFn: api.assistant.bootstrap, staleTime: 30_000 })
  const queryClient = useQueryClient()
  const bootstrap = bootstrapQuery.data
  const [mode, setMode] = useState<AssistantModeID>(() => isMode(routeState.modeId) ? routeState.modeId : persisted?.workspace.mode_id ?? 'profile')
  const [projectId, setProjectId] = useState(routeState.projectId ?? persisted?.workspace.project_id ?? '')
  const [contentId, setContentId] = useState(routeState.contentId ?? persisted?.workspace.content_id ?? '')
  const [composer, setComposer] = useState(routeState.initialMessage ?? '')
  const [workspace, setWorkspace] = useState<AssistantWorkspaceState | null>(() => routeHasContext ? null : persisted?.workspace ?? null)
  const [messagesByMode, setMessagesByMode] = useState<Record<string, Message[]>>({})
  const [questionsByMode, setQuestionsByMode] = useState<Record<string, QuestionBatch[]>>({})
  const [draftByMode, setDraftByMode] = useState<Record<string, Draft | null>>({})
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const [switching, setSwitching] = useState(false)
  const [pendingMode, setPendingMode] = useState<AssistantModeID | null>(null)
  const [committing, setCommitting] = useState(false)
  const [lastEventIds, setLastEventIds] = useState<Record<string, string | undefined>>(persisted?.lastEventIds ?? {})
  const streamAbort = useRef<AbortController | null>(null)
  const userMessageCounter = useRef(0)
  const workspaceCreateKey = useRef<string | null>(null)

  useEffect(() => {
    if (!workspace) {
      if (routeHasContext) sessionStorage.removeItem(WORKSPACE_STORAGE_KEY)
      return
    }
    sessionStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify({ workspace, lastEventIds }))
  }, [workspace, lastEventIds, routeHasContext])

  const projects = bootstrap?.projects ?? []
  const selectionMode = pendingMode ?? mode
  const selectableProjects = selectionMode === 'profile' ? projects : projects.filter((project) => project.profileReady)
  const selectedProject = selectableProjects.find((project) => project.id === projectId) ?? (selectionMode !== 'profile' && selectableProjects.length === 1 ? selectableProjects[0] : undefined)
  const worksQuery = useInfiniteQuery({
    queryKey: ['assistant-workspace-works', selectedProject?.id],
    initialPageParam: undefined as { created_at: string; id: string } | undefined,
    queryFn: ({ pageParam }) => api.assistant.works(selectedProject!.id, pageParam, 25),
    getNextPageParam: (page) => page.has_more ? page.next_cursor : undefined,
    enabled: selectionMode === 'review' && Boolean(selectedProject),
    staleTime: 30_000,
  })
  const works = worksQuery.data?.pages.flatMap((page) => page.items) ?? []
  const selectedWork = works.find((work) => work.id === contentId) ?? (selectionMode === 'review' && works.length === 1 ? works[0] : undefined)
  const activeMessages = messagesByMode[mode] ?? []
  const activeQuestions = (questionsByMode[mode] ?? []).filter((question) => !question.answers)
  const activeDraft = draftByMode[mode]
  const activeMode = bootstrap?.modes.find((candidate) => candidate.id === mode)
  const canSend = Boolean(composer.trim()) && Boolean(activeMode?.available) && (mode === 'profile' || Boolean(selectedProject)) && (mode !== 'review' || Boolean(selectedWork)) && !sending && !switching

  useEffect(() => {
    if (!workspace || !token) return
    streamAbort.current?.abort()
    const controller = new AbortController()
    streamAbort.current = controller
    const stream = streamAssistantWorkspace(workspace.id, token, controller.signal, {
      lastEventId: lastEventIds[mode],
      expectedSessionId: workspace.sessions[mode],
    })
    void (async () => {
      try {
        for await (const event of stream) {
          if (event.id) setLastEventIds((current) => ({ ...current, [mode]: event.id }))
          const data = parseEventData(event)
          const eventMode = isMode(data.mode_id) ? data.mode_id : mode
          const eventSession = stringValue(data.session_id)
          if (eventSession && workspace.sessions[eventMode] && eventSession !== workspace.sessions[eventMode]) {
            // A Server restart may replace the durable logical session while
            // the browser still has the old workspace snapshot. The scoped
            // SSE event is authoritative for the current mode; reconcile the
            // session map before processing its payload.
            setWorkspace((current) => current ? {
              ...current,
              session_id: eventMode === mode ? eventSession : current.session_id,
              sessions: { ...current.sessions, [eventMode]: eventSession },
            } : current)
          }
          if (event.event === 'assistant.message' || event.event === 'user.message') {
            const text = stringValue(data.text)
            if (!text) continue
            const role = event.event === 'assistant.message' ? 'assistant' : 'user'
            setMessagesByMode((current) => {
              const list = current[eventMode] ?? []
              if (event.id && list.some((item) => item.id === event.id)) return current
              if (role === 'user' && event.id) {
                const optimisticIndex = list.findIndex((item) => item.id.startsWith('optimistic-') && item.text === text)
                if (optimisticIndex >= 0) {
                  const next = [...list]
                  next[optimisticIndex] = { id: event.id, role, text }
                  return { ...current, [eventMode]: next }
                }
              }
              return { ...current, [eventMode]: [...list, { id: event.id ?? `${role}-${Date.now()}-${list.length}`, role, text }] }
            })
          } else if (event.event === 'question.opened') {
            const callId = stringValue(data.call_id)
            const questions = parseQuestions(data.questions)
            if (callId && questions.length) setQuestionsByMode((current) => ({ ...current, [eventMode]: [...(current[eventMode] ?? []).filter((item) => item.callId !== callId), { callId, questions }] }))
          } else if (event.event === 'draft.ready' && data.draft && typeof data.draft === 'object') {
            setDraftByMode((current) => ({ ...current, [eventMode]: data.draft as Draft }))
          }
        }
      } catch (streamError) {
        if (!controller.signal.aborted) setError(streamError instanceof Error ? streamError.message : '助手连接已中断')
      }
    })()
    return () => controller.abort()
  }, [mode, token, workspace])

  const sendMessage = async (override?: string) => {
    const text = (override ?? composer).trim()
    if (!text || !canSend) return
    setSending(true)
    setError(null)
    setComposer('')
    setMessagesByMode((current) => ({ ...current, [mode]: [...(current[mode] ?? []), { id: `optimistic-${++userMessageCounter.current}`, role: 'user', text }] }))
    try {
      let currentWorkspace = workspace
      if (!currentWorkspace) {
        if (!workspaceCreateKey.current) workspaceCreateKey.current = crypto.randomUUID()
        currentWorkspace = await api.assistant.createWorkspace({ mode_id: mode, ...(selectedProject ? { project_id: selectedProject.id } : {}), ...(selectedWork ? { content_id: selectedWork.id } : {}), ...(routeState.checkpointId ? { checkpoint_id: routeState.checkpointId } : {}) }, workspaceCreateKey.current)
        workspaceCreateKey.current = null
        setWorkspace(currentWorkspace)
      }
      await api.assistant.workspaceMessage(currentWorkspace.id, mode, text)
    } catch (sendError) {
      setComposer(text)
      setError(sendError instanceof Error ? sendError.message : '消息发送失败')
    } finally {
      setSending(false)
    }
  }

  const chooseMode = async (nextMode: AssistantModeID, context?: { projectId?: string; contentId?: string }) => {
    if (nextMode === mode && !pendingMode || switching) return
    const candidate = bootstrap?.modes.find((item) => item.id === nextMode)
    if (!candidate?.available) {
      setError(candidate?.disabledReason ?? '当前模式不可用')
      return
    }
    setError(null)
    if (!workspace) {
      setPendingMode(null)
      setMode(nextMode)
      setContentId('')
      return
    }
    const requestedProjectId = context?.projectId ?? selectedProject?.id ?? projectId
    const requestedContentId = context?.contentId ?? selectedWork?.id ?? contentId
    if (nextMode === 'review' && (!requestedProjectId || !requestedContentId)) {
      setPendingMode(nextMode)
      setProjectId(requestedProjectId)
      setContentId('')
      return
    }
    setSwitching(true)
    try {
      const nextWorkspace = await api.assistant.switchWorkspaceMode(workspace.id, nextMode, {
        ...(requestedProjectId ? { project_id: requestedProjectId } : {}),
        ...(requestedContentId ? { content_id: requestedContentId } : {}),
      })
      setMode(nextMode)
      setPendingMode(null)
      setProjectId(nextWorkspace.project_id ?? projectId)
      setContentId(nextWorkspace.content_id ?? contentId)
      setWorkspace(nextWorkspace)
    } catch (switchError) {
      setError(switchError instanceof Error ? switchError.message : '当前模式正在处理，请稍后切换')
    } finally {
      setSwitching(false)
    }
  }

  const submitAnswers = async (batch: QuestionBatch, answer: DshQuestionAnswer) => {
    if (!workspace) return
    try {
      await api.assistant.workspaceAnswer(workspace.id, mode, batch.callId, answer.answers)
      setQuestionsByMode((current) => ({ ...current, [mode]: (current[mode] ?? []).map((item) => item.callId === batch.callId ? { ...item, answers: answer.answers } : item) }))
    } catch (answerError) {
      setError(answerError instanceof Error ? answerError.message : '提交回答失败')
    }
  }

  const commitDraft = async () => {
    if (!workspace || !activeDraft || committing) return
    setCommitting(true)
    try {
      const idempotencyKey = crypto.randomUUID()
      const tokenValue = stringValue(activeDraft.confirmation_token)
      let result: Awaited<ReturnType<typeof api.assistant.workspaceCommit>>
      if (mode === 'profile' && tokenValue) {
        result = await api.assistant.workspaceCommit(workspace.id, mode, { confirmation_token: tokenValue, idempotency_key: idempotencyKey })
      } else {
        const checkpoint = await api.assistant.workspaceCheckpoint(workspace.id, mode, { schema_version: '1.0', payload: activeDraft, summary: `${modeLabel(mode)}草稿`, status: 'ready' })
        result = await api.assistant.workspaceCommit(workspace.id, mode, { checkpoint_id: checkpoint.id, checkpoint_digest: checkpoint.payload_digest, idempotency_key: idempotencyKey })
      }
      const committedProjectID = stringValue(result.project?.id)
      if (mode === 'profile' && committedProjectID) {
        const committedProject: AssistantProject = {
          id: committedProjectID,
          name: stringValue(result.project?.name) ?? '新画像',
          platform: stringValue(result.project?.platform) ?? 'wechat',
          profileRevision: typeof result.project?.profile_revision === 'number' ? result.project.profile_revision : 1,
          profileReady: true,
          works: [],
        }
        setProjectId(committedProjectID)
        setContentId('')
        setWorkspace((current) => current ? { ...current, project_id: committedProjectID, content_id: '' } : current)
        queryClient.setQueryData<{ projects: AssistantProject[] } | undefined>(['assistant-bootstrap'], (current) => current ? { ...current, projects: [...current.projects.filter((project) => project.id !== committedProjectID), committedProject] } : current)
        void queryClient.invalidateQueries({ queryKey: ['assistant-bootstrap'] })
      }
      setDraftByMode((current) => ({ ...current, [mode]: null }))
    } catch (commitError) {
      setError(commitError instanceof Error ? commitError.message : '确认失败')
    } finally {
      setCommitting(false)
    }
  }

  if (bootstrapQuery.isError) return <main className="flex min-h-dvh items-center justify-center px-6"><p role="alert" className="text-sm text-destructive">AI 助手暂时无法加载，请稍后重试。</p></main>
  if (bootstrapQuery.isLoading) return <main className="flex min-h-dvh items-center justify-center text-sm text-muted-foreground"><Loader2 className="mr-2 size-4 animate-spin" />正在加载</main>

  const heading = mode === 'profile' ? `开始为「${projectName(selectedProject)}」建立画像` : mode === 'creation' ? `为「${projectName(selectedProject)}」制定新的创作计划` : `复盘「${projectName(selectedProject)}」的作品${selectedWork ? `：${selectedWork.title}` : ''}`

  return (
    <main data-testid="assistant-workspace" className="flex min-h-dvh w-full flex-col bg-background">
      <header className="flex h-16 shrink-0 items-center justify-between border-b border-border/70 px-5 md:px-10">
        <div className="flex items-center gap-3"><span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground"><Sparkles className="size-4" /></span><span className="text-sm font-semibold">AI 助手</span>{workspace && <span className="text-xs text-muted-foreground">{modeLabel(mode)}</span>}</div>
      </header>

      {!workspace ? <section className="mx-auto flex w-full max-w-3xl flex-1 flex-col justify-center px-5 py-12 text-center"><p className="text-sm font-medium text-primary">Anban AI</p><h1 className="mt-4 text-3xl font-semibold tracking-tight sm:text-4xl">{heading}</h1><p className="mx-auto mt-3 max-w-xl text-sm leading-6 text-muted-foreground">选择模式，告诉我你接下来想完成什么。</p><nav className="mx-auto mt-10 flex flex-wrap justify-center gap-1" aria-label="助手模式">{MODES.map((candidate) => { const item = bootstrap?.modes.find((entry) => entry.id === candidate); return <button key={candidate} type="button" aria-pressed={mode === candidate} aria-disabled={!item?.available} onClick={() => void chooseMode(candidate)} className={`rounded-full px-4 py-2 text-sm ${mode === candidate ? 'bg-foreground text-background' : 'text-muted-foreground hover:bg-muted'} ${!item?.available ? 'opacity-50' : ''}`}>{modeLabel(candidate)}</button> })}</nav>
        <WorkspaceSelectors mode={selectionMode} projects={selectableProjects} projectId={selectedProject?.id ?? projectId} contentId={selectedWork?.id ?? contentId} works={works} onProject={(value) => { setProjectId(value); setContentId('') }} onContent={setContentId} onMore={() => void worksQuery.fetchNextPage()} hasMore={Boolean(worksQuery.hasNextPage)} />
        <Composer value={composer} placeholder={modePlaceholder(mode)} disabled={!canSend} onChange={setComposer} onSend={() => void sendMessage()} />
      </section> : <section className="mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col px-4 pb-4 md:px-8"><div className="flex shrink-0 flex-wrap items-center justify-center gap-1 py-4">{MODES.map((candidate) => { const item = bootstrap?.modes.find((entry) => entry.id === candidate); return <button key={candidate} type="button" aria-pressed={mode === candidate} disabled={!item?.available || switching} onClick={() => void chooseMode(candidate)} className={`rounded-full px-4 py-2 text-sm ${mode === candidate ? 'bg-foreground text-background' : 'text-muted-foreground hover:bg-muted'} ${!item?.available ? 'opacity-50' : ''}`}>{modeLabel(candidate)}</button> })}</div><WorkspaceSelectors mode={selectionMode} projects={selectableProjects} projectId={selectedProject?.id ?? projectId} contentId={selectedWork?.id ?? contentId} works={works} disabled={Boolean(workspace && !pendingMode)} onProject={(value) => { setProjectId(value); setContentId('') }} onContent={(value) => { setContentId(value); if (pendingMode && selectedProject) void chooseMode(pendingMode, { projectId: selectedProject.id, contentId: value }) }} onMore={() => void worksQuery.fetchNextPage()} hasMore={Boolean(worksQuery.hasNextPage)} /><div className="min-h-0 flex-1 overflow-y-auto py-4" aria-live="polite"><Conversation messages={activeMessages} questions={activeQuestions} onAnswer={submitAnswers} />{activeDraft && <div className="mt-6 rounded-xl border border-primary/30 bg-primary/[0.04] p-4"><div className="flex items-center gap-2 text-sm font-medium"><Check className="size-4 text-emerald-600" />{modeLabel(mode)}已整理完成</div><p className="mt-2 text-xs text-muted-foreground">确认后保存当前结构化结果。</p><Button className="mt-4" size="sm" disabled={committing} onClick={() => void commitDraft()}>{committing ? <Loader2 className="size-4 animate-spin" /> : '确认并保存'}</Button></div>}{error && <p role="alert" className="mt-4 text-sm text-destructive">{error}</p>}</div><Composer value={composer} placeholder={activeQuestions.length ? '请先完成上面的回答' : modePlaceholder(mode)} disabled={!canSend || activeQuestions.length > 0} onChange={setComposer} onSend={() => void sendMessage()} /></section>}
    </main>
  )
}

function WorkspaceSelectors({ mode, projects, projectId, contentId, works, disabled = false, onProject, onContent, onMore, hasMore }: { mode: AssistantModeID; projects: AssistantProject[]; projectId: string; contentId: string; works: AssistantProject['works']; disabled?: boolean; onProject: (value: string) => void; onContent: (value: string) => void; onMore: () => void; hasMore: boolean }) {
  return <div className="mx-auto mt-8 flex w-full max-w-xl flex-wrap items-center justify-center gap-2"><NativeSelect aria-label="选择画像" value={projectId} disabled={disabled} onChange={(event) => onProject(event.target.value)}><NativeSelectOption value="">{mode === 'profile' ? '新建画像' : '选择画像'}</NativeSelectOption>{projects.map((project) => <NativeSelectOption key={project.id} value={project.id}>{project.name}</NativeSelectOption>)}</NativeSelect>{mode === 'review' && <><span className="text-muted-foreground">/</span><NativeSelect aria-label="选择作品" value={contentId} disabled={disabled} onChange={(event) => onContent(event.target.value)}><NativeSelectOption value="">选择作品</NativeSelectOption>{works.map((work) => <NativeSelectOption key={work.id} value={work.id}>{work.title}</NativeSelectOption>)}</NativeSelect>{hasMore && <Button type="button" variant="ghost" size="icon-sm" aria-label="加载更多作品" disabled={disabled} onClick={onMore}><ChevronDown className="size-4" /></Button>}</>}</div>
}

function Composer({ value, placeholder, disabled, onChange, onSend }: { value: string; placeholder: string; disabled: boolean; onChange: (value: string) => void; onSend: () => void }) {
  return <section className="mt-8 overflow-hidden rounded-2xl border border-border bg-background shadow-sm focus-within:border-ring focus-within:ring-4 focus-within:ring-ring/10"><Textarea value={value} onChange={(event) => onChange(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); onSend() } }} placeholder={placeholder} aria-label="发送消息" disabled={disabled} className="min-h-28 resize-none border-0 px-5 py-4 shadow-none focus-visible:ring-0" /><div className="flex items-center justify-end border-t border-border/70 px-3 py-2.5"><Button size="icon-md" aria-label="发送消息" title="发送消息" disabled={disabled || !value.trim()} onClick={onSend}><Send className="size-4" /></Button></div></section>
}

function Conversation({ messages, questions, onAnswer }: { messages: Message[]; questions: QuestionBatch[]; onAnswer: (batch: QuestionBatch, answer: DshQuestionAnswer) => Promise<void> }) {
  if (messages.length === 0 && questions.length === 0) return <div className="flex min-h-[18rem] items-center justify-center text-sm text-muted-foreground">输入你的需求后开始</div>
  return <div className="space-y-5">{messages.map((message) => <div key={message.id} className={`flex gap-3 ${message.role === 'user' ? 'justify-end' : ''}`}><div className={`flex max-w-[min(720px,90%)] gap-3 ${message.role === 'user' ? 'flex-row-reverse' : ''}`}><div className={`mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}>{message.role === 'user' ? <UserRound className="size-3.5" /> : <MessageSquare className="size-3.5" />}</div><div className={`rounded-2xl px-4 py-3 text-sm leading-6 ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted/70'}`}><Streamdown>{message.text}</Streamdown></div></div></div>)}{questions.map((batch) => <DshQuestionComposer key={batch.callId} questions={batch.questions} answers={batch.answers} onSubmit={(answer) => onAnswer(batch, answer)} />)}</div>
}
