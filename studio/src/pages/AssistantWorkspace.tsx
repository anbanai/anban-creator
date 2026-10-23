import { useEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUp, Check, ChevronDown, Loader2, MessageSquare, Sparkles, UserRound } from 'lucide-react'
import { Streamdown } from 'streamdown'
import { useLocation } from 'react-router-dom'
import { api } from '@/lib/api'
import type { AssistantModeID, AssistantWorkspace as AssistantWorkspaceState, AssistantProject } from '@/lib/api/assistant'
import { useAuth } from '@/contexts/AuthContext'
import { streamAssistantWorkspace, type InteractiveStreamEvent } from '@/lib/interactive-events'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '@/components/ui/input-group'
import { DshQuestionComposer, type DshQuestion, type DshQuestionAnswer } from '@/components/dsh-user-questions/DshQuestionComposer'
import { AssistantVideoEditInputs } from '@/components/video-edit/AssistantVideoEditInputs'
import { VideoEditProjectDefaultsPanel } from '@/components/video-edit/VideoEditProjectDefaultsPanel'
import { TaskResults } from '@/components/tasks/TaskResults'
import type { InputAttachment } from '@/types/input-attachment'
import type { VideoEditDefaults, VideoEditPreferences } from '@/types/video-edit'
import type { Task, TaskFile } from '@/types'

type Message = { id: string; role: 'user' | 'assistant'; text: string }
type QuestionBatch = { callId: string; questions: DshQuestion[]; answers?: DshQuestionAnswer['answers'] }
type Draft = Record<string, unknown>
type RouteState = { modeId?: AssistantModeID; projectId?: string; contentId?: string; checkpointId?: string; initialMessage?: string }
type PersistedWorkspace = { workspace: AssistantWorkspaceState; lastEventIds: Record<string, string | undefined> }

const WORKSPACE_STORAGE_KEY = 'anban:assistant-workspace'

const MODES: AssistantModeID[] = ['profile', 'creation', 'review']
const CREATION_TYPES: Record<string, { value: string; label: string }[]> = {
  wechat: [{ value: 'wechat-article', label: '公众号文章' }, { value: 'wechat-picture', label: '公众号贴图' }],
  seednote: [{ value: 'seednote', label: '种草笔记' }],
  montage: [{ value: 'video', label: '视频生成' }],
  'video-edit': [{ value: 'video-edit', label: '全自动视频剪辑' }],
}

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

function assistantWorkspaceErrorCode(error: unknown): string | undefined {
  if (!error || typeof error !== 'object' || !('response' in error)) return undefined
  const data = (error as { response?: { data?: unknown } }).response?.data
  if (!data || typeof data !== 'object' || !('error_code' in data)) return undefined
  const code = (data as { error_code?: unknown }).error_code
  return typeof code === 'string' ? code : undefined
}

function assistantWorkspaceHttpStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object' || !('response' in error)) return undefined
  const status = (error as { response?: { status?: unknown } }).response?.status
  return typeof status === 'number' ? status : undefined
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
  const [creationType, setCreationType] = useState('')
  const [videoEditAssets, setVideoEditAssets] = useState<InputAttachment[]>([])
  const [videoEditBrief, setVideoEditBrief] = useState('')
  const [videoEditPreferences, setVideoEditPreferences] = useState<VideoEditPreferences>({})
  const [videoEditDefaults, setVideoEditDefaults] = useState<VideoEditDefaults>({})
  const [videoEditUploading, setVideoEditUploading] = useState(false)
  const [videoEditUploadFailed, setVideoEditUploadFailed] = useState(false)
  const [workspace, setWorkspace] = useState<AssistantWorkspaceState | null>(() => routeHasContext ? null : persisted?.workspace ?? null)
  const [messagesByMode, setMessagesByMode] = useState<Record<string, Message[]>>({})
  const [questionsByMode, setQuestionsByMode] = useState<Record<string, QuestionBatch[]>>({})
  const [draftByMode, setDraftByMode] = useState<Record<string, Draft | null>>({})
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const [responding, setResponding] = useState(false)
  const [switching, setSwitching] = useState(false)
  const [pendingMode, setPendingMode] = useState<AssistantModeID | null>(null)
  const [committing, setCommitting] = useState(false)
  const [lastEventIds, setLastEventIds] = useState<Record<string, string | undefined>>(persisted?.lastEventIds ?? {})
  const streamAbort = useRef<AbortController | null>(null)
  const userMessageCounter = useRef(0)
  const workspaceCreateKey = useRef<string | null>(null)
  const sendInFlight = useRef(false)

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
  const creationTypes = CREATION_TYPES[selectedProject?.platform ?? ''] ?? []
  const selectedCreationType = creationTypes.some((item) => item.value === creationType) ? creationType : creationTypes[0]?.value ?? ''
  const isVideoEditCreation = mode === 'creation' && selectedCreationType === 'video-edit'
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
  const reviewVideoQuery = useQuery({
    queryKey: ['assistant-review-video', selectedWork?.id],
    queryFn: async () => Promise.all([api.tasks.get(selectedWork!.id), api.tasks.files(selectedWork!.id)]),
    enabled: mode === 'review' && selectedProject?.platform === 'video-edit' && Boolean(selectedWork),
    staleTime: 30_000,
  })
  const activeMessages = messagesByMode[mode] ?? []
  const activeQuestions = (questionsByMode[mode] ?? []).filter((question) => !question.answers)
  const activeDraft = draftByMode[mode]
  const activeMode = bootstrap?.modes.find((candidate) => candidate.id === mode)
  const isRuntimeUnavailable = activeMode?.disabledReason === '助手运行环境暂不可用，请稍后重试'
  const modePrerequisiteBlocked = Boolean(activeMode && !activeMode.available && activeMode.disabledReason && !isRuntimeUnavailable)
  const composerPlaceholder = modePrerequisiteBlocked
    ? mode === 'creation' ? '先完成画像初始化后即可输入消息' : '先完成当前模式所需条件后即可输入消息'
    : modePlaceholder(mode)
  const modePrerequisiteAction = mode === 'review' && selectableProjects.length > 0
    ? { mode: 'creation' as const, label: '去创作模式' }
    : { mode: 'profile' as const, label: '去建立画像' }
  const draftProject = activeDraft?.project && typeof activeDraft.project === 'object' && !Array.isArray(activeDraft.project)
    ? activeDraft.project as Record<string, unknown>
    : undefined
  const profilePlatform = selectedProject?.platform ?? stringValue(draftProject?.platform)
  const isVideoEditProfile = mode === 'profile' && profilePlatform === 'video-edit'
  const canSend = Boolean(composer.trim()) && Boolean(activeMode?.available) && (mode === 'profile' || Boolean(selectedProject)) && (mode !== 'review' || Boolean(selectedWork)) && !sending && !responding && !switching

  useEffect(() => {
    if (mode === 'profile' && selectedProject) setVideoEditDefaults(selectedProject.video_edit_defaults ?? {})
  }, [mode, selectedProject?.id])

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
          if (event.event === 'turn.start') setResponding(true)
          if (event.event === 'turn.end' || event.event === 'question.opened' || event.event === 'session.error') setResponding(false)
          if (event.event === 'session.status' && ['idle', 'waiting_answer', 'runtime_failed'].includes(String(data.status))) setResponding(false)
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
        if (!controller.signal.aborted) {
          setResponding(false)
          setError(streamError instanceof Error ? streamError.message : '助手连接已中断')
        }
      }
    })()
    return () => controller.abort()
  }, [mode, token, workspace])

  const sendMessage = async (override?: string) => {
    const text = (override ?? composer).trim()
    if (!text || !canSend || sendInFlight.current) return
    sendInFlight.current = true
    const contextLines = mode === 'creation' && selectedCreationType
      ? [`创作类型：${selectedCreationType}`, ...(isVideoEditCreation ? [
          `剪辑要求：${videoEditBrief.trim() || '按本次创作需求自动决定'}`,
          `剪辑偏好：${JSON.stringify(videoEditPreferences)}`,
          `已上传素材：${videoEditAssets.map((asset) => asset.file_name).filter(Boolean).join('、') || '待上传'}`,
        ] : [])]
      : []
    const messageText = contextLines.length ? `${text}\n\n${contextLines.join('\n')}` : text
    const optimisticID = `optimistic-${++userMessageCounter.current}`
    setSending(true)
    setError(null)
    setComposer('')
    setMessagesByMode((current) => ({ ...current, [mode]: [...(current[mode] ?? []), { id: optimisticID, role: 'user', text: messageText }] }))
    try {
      let currentWorkspace = workspace
      if (!currentWorkspace) {
        if (!workspaceCreateKey.current) workspaceCreateKey.current = crypto.randomUUID()
        currentWorkspace = await api.assistant.createWorkspace({ mode_id: mode, ...(selectedProject ? { project_id: selectedProject.id } : {}), ...(selectedWork ? { content_id: selectedWork.id } : {}), ...(routeState.checkpointId ? { checkpoint_id: routeState.checkpointId } : {}) }, workspaceCreateKey.current)
        workspaceCreateKey.current = null
        setWorkspace(currentWorkspace)
      }
      await api.assistant.workspaceMessage(currentWorkspace.id, mode, messageText)
      setResponding(true)
    } catch (sendError) {
      setComposer(text)
      setMessagesByMode((current) => ({ ...current, [mode]: (current[mode] ?? []).filter((message) => message.id !== optimisticID) }))
      const errorCode = assistantWorkspaceErrorCode(sendError)
      if (errorCode === 'assistant_workspace_expired') {
        setWorkspace(null)
        setLastEventIds({})
        setError('对话已过期。消息仍保留在输入框中，重新发送后会建立新会话。')
      } else if (errorCode === 'assistant_workspace_busy') {
        setError('助手正在处理上一条消息，输入已保留，请稍后重试。')
      } else if (errorCode === 'interactive_session_busy' || errorCode === 'interactive_question_pending') {
        setError(errorCode === 'interactive_question_pending'
          ? '请先完成上方的问题，再发送新消息。输入已保留。'
          : '助手正在处理上一条消息，输入已保留，请稍后重试。')
      } else if (assistantWorkspaceHttpStatus(sendError) === 409) {
        setError('对话状态刚刚发生变化，输入已保留，请稍后重试。')
      } else {
        setError(sendError instanceof Error ? sendError.message : '消息发送失败')
      }
    } finally {
      sendInFlight.current = false
      setSending(false)
    }
  }

  const chooseMode = async (nextMode: AssistantModeID, context?: { projectId?: string; contentId?: string }) => {
    if (nextMode === mode && !pendingMode || switching) return
    const candidate = bootstrap?.modes.find((item) => item.id === nextMode)
    if (!candidate) {
      setError('当前模式暂时无法加载，请稍后重试')
      return
    }
    if (!candidate.available && candidate.disabledReason === '助手运行环境暂不可用，请稍后重试') {
      setError(candidate.disabledReason)
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
      setResponding(false)
    } catch (switchError) {
      const errorCode = assistantWorkspaceErrorCode(switchError)
      if (errorCode === 'assistant_workspace_expired') {
        setWorkspace(null)
        setLastEventIds({})
        setError('对话已过期，请重新发送消息以建立新会话。')
      } else if (errorCode === 'assistant_workspace_busy') {
        setError('助手仍在处理上一条消息，请稍后切换。')
      } else {
        setError(switchError instanceof Error ? switchError.message : '当前模式正在处理，请稍后切换')
      }
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
      const errorCode = assistantWorkspaceErrorCode(answerError)
      if (errorCode === 'assistant_workspace_expired') {
        setWorkspace(null)
        setLastEventIds({})
        setError('对话已过期，请重新发送消息以建立新会话。')
      } else if (errorCode === 'assistant_workspace_busy') {
        setError('助手仍在处理上一条消息，请稍后提交回答。')
      } else {
        setError(answerError instanceof Error ? answerError.message : '提交回答失败')
      }
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
        result = await api.assistant.workspaceCommit(workspace.id, mode, {
          confirmation_token: tokenValue,
          idempotency_key: idempotencyKey,
          ...(isVideoEditProfile ? { video_edit_defaults: videoEditDefaults } : {}),
        })
      } else {
        const payload = structuredClone(activeDraft)
        if (mode === 'creation') {
          if (!selectedCreationType) {
            setError('当前画像不支持可用的创作类型')
            return
          }
          const plan = payload.plan && typeof payload.plan === 'object' && !Array.isArray(payload.plan) ? payload.plan as Record<string, unknown> : payload
          plan.content_type = selectedCreationType
          if (selectedCreationType === 'video-edit') {
            const uploadedAssets = videoEditAssets.filter((asset) => asset.upload_id && asset.key)
            if (videoEditUploading || videoEditUploadFailed || uploadedAssets.length === 0) {
              setError(videoEditUploading ? '请等待素材上传完成' : '请上传有效的视频、音频或图片素材')
              return
            }
            plan.assets = uploadedAssets.map((asset) => ({
              type: asset.type,
              upload_id: asset.upload_id,
              file_name: asset.file_name,
              mime_type: asset.content_type,
              file_size: asset.size,
            }))
            plan.video_edit_brief = videoEditBrief.trim()
            plan.video_edit_preferences = Object.fromEntries(Object.entries(videoEditPreferences).filter(([, value]) => value !== undefined && value !== ''))
          }
          if (plan !== payload) payload.plan = plan
        }
        const checkpoint = await api.assistant.workspaceCheckpoint(workspace.id, mode, { schema_version: '1.0', payload, summary: `${modeLabel(mode)}草稿`, status: 'ready' })
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
          ...(isVideoEditProfile ? { video_edit_defaults: videoEditDefaults } : {}),
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

  const heading = mode === 'profile' ? `开始为「${projectName(selectedProject)}」建立画像` : mode === 'creation' ? `为「${projectName(selectedProject)}」制定新的创作计划` : `复盘「${projectName(selectedProject)}」的作品${selectedWork ? `：${selectedWork.title}` : ''}`
  const chooseCreationType = (value: string) => {
    setCreationType(value)
    if (value !== 'video-edit') {
      setVideoEditAssets([])
      setVideoEditBrief('')
      setVideoEditPreferences({})
      setVideoEditUploadFailed(false)
    }
  }
  const resetCreationProject = (value: string) => {
    setProjectId(value)
    setContentId('')
    setVideoEditDefaults(projects.find((project) => project.id === value)?.video_edit_defaults ?? {})
    setCreationType('')
    setVideoEditAssets([])
    setVideoEditBrief('')
    setVideoEditPreferences({})
    setVideoEditUploadFailed(false)
  }
  const creationTypeSelector = mode === 'creation' && selectedProject ? (
    <label className="mx-auto mt-3 block w-full max-w-xs space-y-1.5 text-left text-sm">
      <span>创作类型</span>
      <NativeSelect className="w-full" aria-label="创作类型" value={selectedCreationType} onChange={(event) => chooseCreationType(event.target.value)}>
        {creationTypes.map((item) => <NativeSelectOption key={item.value} value={item.value}>{item.label}</NativeSelectOption>)}
      </NativeSelect>
    </label>
  ) : null
  const videoEditInputs = isVideoEditCreation && selectedProject ? (
    <AssistantVideoEditInputs
      assets={videoEditAssets}
      brief={videoEditBrief}
      preferences={videoEditPreferences}
      defaults={selectedProject.video_edit_defaults}
      uploading={videoEditUploading}
      onAssetsChange={setVideoEditAssets}
      onBriefChange={setVideoEditBrief}
      onPreferencesChange={setVideoEditPreferences}
      onUploadingChange={setVideoEditUploading}
      onFailuresChange={setVideoEditUploadFailed}
    />
  ) : null
  const selectModePrerequisiteAction = () => {
    setError(null)
    setMode(modePrerequisiteAction.mode)
    setPendingMode(null)
    setContentId('')
  }

  return (
    <main data-testid="assistant-workspace" className="flex h-full min-h-0 w-full flex-col overflow-hidden bg-background">
      <header className="hidden h-14 shrink-0 items-center gap-3 border-b border-border/70 px-5 md:flex md:px-8">
        <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground"><Sparkles className="size-4" /></span><span className="text-sm font-semibold">AI 助手</span>
      </header>

      {!workspace ? (
        <section className="mx-auto flex w-full max-w-3xl flex-1 flex-col justify-center px-5 py-12 text-center">
          <p className="text-sm font-medium text-primary">Anban AI</p>
          <h1 className="mt-4 text-3xl font-semibold tracking-tight sm:text-4xl">{heading}</h1>
          <nav className="mx-auto mt-10 flex flex-wrap justify-center gap-1" aria-label="助手模式">{MODES.map((candidate) => { const item = bootstrap?.modes.find((entry) => entry.id === candidate); const blockedByRuntime = item?.disabledReason === '助手运行环境暂不可用，请稍后重试'; return <button key={candidate} type="button" aria-pressed={mode === candidate} disabled={bootstrapQuery.isLoading || blockedByRuntime} aria-disabled={blockedByRuntime} onClick={() => void chooseMode(candidate)} className={`rounded-full px-4 py-2 text-sm ${mode === candidate ? 'bg-foreground text-background' : 'text-muted-foreground hover:bg-muted'} ${blockedByRuntime ? 'opacity-50' : ''}`}>{modeLabel(candidate)}</button> })}</nav>
          <p className="mx-auto mt-2 min-h-6 max-w-xl text-sm leading-6 text-muted-foreground" aria-live="polite">{activeMode?.description ?? (bootstrapQuery.isLoading ? '正在准备创作空间…' : '')}</p>
          {!activeMode?.available && activeMode?.disabledReason && !isRuntimeUnavailable && <div className="mx-auto mt-3 flex w-full max-w-xl flex-col items-center gap-3 rounded-lg border border-border bg-muted/40 px-4 py-3 sm:flex-row sm:justify-between"><p role="status" className="text-left text-sm leading-6 text-muted-foreground">{activeMode.disabledReason}</p><Button type="button" variant="outline" size="sm" className="shrink-0" onClick={selectModePrerequisiteAction}>{modePrerequisiteAction.label}</Button></div>}
          {bootstrapQuery.isError && <div className="mt-3 flex flex-wrap items-center justify-center gap-3"><p role="alert" className="text-sm text-destructive">AI 助手暂时无法加载，请稍后重试。</p><Button type="button" variant="outline" size="sm" disabled={bootstrapQuery.isFetching} onClick={() => void bootstrapQuery.refetch()}>{bootstrapQuery.isFetching ? <Loader2 className="size-4 animate-spin" /> : '重试加载'}</Button></div>}
          {bootstrapQuery.isLoading && <p role="status" className="sr-only">正在加载创作模式</p>}
          {(mode === 'profile' || selectedProject || activeMode?.available) && <WorkspaceSelectors mode={selectionMode} projects={selectableProjects} projectId={selectedProject?.id ?? projectId} contentId={selectedWork?.id ?? contentId} works={works} onProject={resetCreationProject} onContent={setContentId} onMore={() => void worksQuery.fetchNextPage()} hasMore={Boolean(worksQuery.hasNextPage)} />}
          {isVideoEditProfile && <VideoEditProjectDefaultsPanel value={videoEditDefaults} onChange={setVideoEditDefaults} />}
          {error && <p role="alert" className="mt-4 text-sm text-destructive">{error}</p>}
          {mode === 'review' && selectedProject?.platform === 'video-edit' && selectedWork && (
            <VideoEditReviewResults query={reviewVideoQuery} />
          )}
          {creationTypeSelector}
          {videoEditInputs}
          <Composer value={composer} placeholder={composerPlaceholder} inputDisabled={sending || switching || modePrerequisiteBlocked} sendDisabled={!canSend} onChange={setComposer} onSend={() => void sendMessage()} />
        </section>
      ) : (
        <section className="mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col px-4 pb-3 md:px-8">
          <div data-testid="assistant-conversation" className="min-h-0 flex-1 overflow-y-auto py-4" aria-live="polite">
            {mode === 'review' && selectedProject?.platform === 'video-edit' && selectedWork && (
              <VideoEditReviewResults query={reviewVideoQuery} />
            )}
            <Conversation messages={activeMessages} questions={activeQuestions} onAnswer={submitAnswers} />
            {activeDraft && <div className="mt-6 rounded-xl border border-primary/30 bg-primary/[0.04] p-4"><div className="flex items-center gap-2 text-sm font-medium"><Check className="size-4 text-emerald-600" />{modeLabel(mode)}已整理完成</div><p className="mt-2 text-xs text-muted-foreground">确认后保存当前结构化结果。</p><Button className="mt-4" size="sm" disabled={committing || (mode === 'creation' && selectedCreationType === 'video-edit' && (videoEditUploading || videoEditUploadFailed || videoEditAssets.length === 0))} onClick={() => void commitDraft()}>{committing ? <Loader2 className="size-4 animate-spin" /> : '确认并保存'}</Button></div>}
            {error && <p role="alert" className="mt-4 text-sm text-destructive">{error}</p>}
          </div>
          <Composer value={composer} placeholder={activeQuestions.length ? '请先完成上方问题' : '输入消息…'} inputDisabled={sending || switching || activeQuestions.length > 0} sendDisabled={!canSend || activeQuestions.length > 0} status={responding ? '助手正在回复…' : undefined} onChange={setComposer} onSend={() => void sendMessage()} />
        </section>
      )}
    </main>
  )
}

function VideoEditReviewResults({ query }: { query: { data?: [Task, TaskFile[]]; isLoading: boolean; isError: boolean } }) {
  if (query.isLoading) return <p role="status" className="py-4 text-sm text-muted-foreground">正在加载作品产物</p>
  if (query.isError) return <p role="alert" className="py-4 text-sm text-destructive">作品产物暂时无法加载。</p>
  if (!query.data) return null
  return <div className="mb-6"><TaskResults task={query.data[0]} files={query.data[1]} /></div>
}

function WorkspaceSelectors({ mode, projects, projectId, contentId, works, disabled = false, onProject, onContent, onMore, hasMore }: { mode: AssistantModeID; projects: AssistantProject[]; projectId: string; contentId: string; works: AssistantProject['works']; disabled?: boolean; onProject: (value: string) => void; onContent: (value: string) => void; onMore: () => void; hasMore: boolean }) {
  return <div className="mx-auto mt-6 flex w-full max-w-xs flex-col items-stretch gap-3"><NativeSelect className="w-full" aria-label="选择画像" value={projectId} disabled={disabled} onChange={(event) => onProject(event.target.value)}><NativeSelectOption value="">{mode === 'profile' ? '新建画像' : '选择画像'}</NativeSelectOption>{projects.map((project) => <NativeSelectOption key={project.id} value={project.id}>{project.name}</NativeSelectOption>)}</NativeSelect>{mode === 'review' && <NativeSelect className="w-full" aria-label="选择作品" value={contentId} disabled={disabled} onChange={(event) => onContent(event.target.value)}><NativeSelectOption value="">选择作品</NativeSelectOption>{works.map((work) => <NativeSelectOption key={work.id} value={work.id}>{work.title}</NativeSelectOption>)}</NativeSelect>}{mode === 'review' && hasMore && <Button type="button" variant="ghost" size="sm" className="self-end" aria-label="加载更多作品" disabled={disabled} onClick={onMore}><ChevronDown className="size-4" />加载更多作品</Button>}</div>
}

function Composer({ value, placeholder, inputDisabled, sendDisabled, status, onChange, onSend }: { value: string; placeholder: string; inputDisabled: boolean; sendDisabled: boolean; status?: string; onChange: (value: string) => void; onSend: () => void }) {
  return <div className="shrink-0 pt-2">
    {status && <p role="status" className="pb-2 text-xs text-muted-foreground">{status}</p>}
    <InputGroup className="min-h-20 items-end rounded-xl bg-background px-2 py-1 shadow-sm focus-within:ring-2 focus-within:ring-ring/20">
      <InputGroupTextarea value={value} rows={2} onChange={(event) => onChange(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && Reflect.get(event.nativeEvent, 'keyCode') !== 229) { event.preventDefault(); onSend() } }} placeholder={placeholder} aria-label="发送消息" disabled={inputDisabled} className="max-h-48 min-h-16 px-3 py-3 leading-6" />
      <InputGroupAddon align="inline-end" className="self-end pb-1.5 pr-1.5">
        <InputGroupButton aria-label="发送消息" title="发送消息" size="icon-sm" variant="default" disabled={sendDisabled || !value.trim()} onClick={onSend}><ArrowUp className="size-4" /></InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  </div>
}

function Conversation({ messages, questions, onAnswer }: { messages: Message[]; questions: QuestionBatch[]; onAnswer: (batch: QuestionBatch, answer: DshQuestionAnswer) => Promise<void> }) {
  if (messages.length === 0 && questions.length === 0) return <div className="flex min-h-[18rem] items-center justify-center text-sm text-muted-foreground">输入你的需求后开始</div>
  return <div className="space-y-5">{messages.map((message) => <div key={message.id} className={`flex gap-3 ${message.role === 'user' ? 'justify-end' : ''}`}><div className={`flex max-w-[min(720px,90%)] gap-3 ${message.role === 'user' ? 'flex-row-reverse' : ''}`}><div className={`mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}>{message.role === 'user' ? <UserRound className="size-3.5" /> : <MessageSquare className="size-3.5" />}</div><div className={`rounded-2xl px-4 py-3 text-sm leading-6 ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted/70'}`}><Streamdown>{message.text}</Streamdown></div></div></div>)}{questions.map((batch) => <DshQuestionComposer key={batch.callId} questions={batch.questions} answers={batch.answers} onSubmit={(answer) => onAnswer(batch, answer)} />)}</div>
}
