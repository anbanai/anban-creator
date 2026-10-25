import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bot, Check, ChevronDown, Loader2, Plus, UserRound } from 'lucide-react'
import { Streamdown } from 'streamdown'
import { useLocation, useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import type { AssistantModeID, AssistantWorkspace as AssistantWorkspaceState, AssistantProject } from '@/lib/api/assistant'
import type { InteractiveDraft } from '@/lib/api/interactive'
import { useAuth } from '@/contexts/AuthContext'
import { InteractiveStreamError, streamAssistantWorkspace, type InteractiveStreamEvent } from '@/lib/interactive-events'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { AgentPromptDropProvider } from '@/components/agent-prompt/AgentPromptDropProvider'
import { AgentIconStack } from '@/components/agent-prompt/AgentIconStack'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import { TaskComposerParameters } from '@/components/tasks/TaskComposerParameters'
import { AssistantContextBar } from '@/components/assistant/AssistantContextBar'
import { useAgentExecutionProfiles } from '@/hooks/useAgentExecutionProfiles'
import { useImageCapabilities } from '@/hooks/useImageCapabilities'
import { DshQuestionComposer, type DshQuestion, type DshQuestionAnswer } from '@/components/dsh-user-questions/DshQuestionComposer'
import { AssistantVideoEditInputs } from '@/components/video-edit/AssistantVideoEditInputs'
import { VideoEditProjectDefaultsPanel } from '@/components/video-edit/VideoEditProjectDefaultsPanel'
import { TaskResults } from '@/components/tasks/TaskResults'
import { AssistantDraftSummary } from '@/components/assistant/AssistantDraftSummary'
import { SignedImage } from '@/components/ui/SignedImage'
import type { InputAttachment, PromptAttachment } from '@/types/input-attachment'
import type { AgentExecutionProfileID } from '@/types'
import type { VideoEditDefaults, VideoEditPreferences } from '@/types/video-edit'
import type { Task, TaskFile } from '@/types'
import type { ImageRatio } from '@/lib/schemas'
import { platformDisplayName } from '@/lib/labels'
import { profileLabAgents } from '@/lib/project-capabilities'

type Message = { id: string; role: 'user' | 'assistant'; text: string; order: number }
type QuestionBatch = { callId: string; questions: DshQuestion[]; answers?: DshQuestionAnswer['answers']; order: number }
type Draft = Record<string, unknown>
type RouteState = { modeId?: AssistantModeID; projectId?: string; contentId?: string; checkpointId?: string; initialMessage?: string }
type PersistedWorkspace = {
  workspace: AssistantWorkspaceState | null
  lastEventIds: Record<string, string | undefined>
  creationType?: string
  mode?: AssistantModeID
  projectId?: string
  contentId?: string
  composer?: string
  messagesByMode?: Record<string, Message[]>
  questionsByMode?: Record<string, QuestionBatch[]>
  draftByMode?: Record<string, Draft | null>
}

const WORKSPACE_STORAGE_KEY = 'anban:assistant-workspace'

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

function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined
}

function parsePersistedMessages(value: unknown): Record<string, Message[]> | undefined {
  const source = recordValue(value)
  if (!source) return undefined
  const result: Record<string, Message[]> = {}
  for (const [mode, entries] of Object.entries(source)) {
    if (!Array.isArray(entries)) continue
    result[mode] = entries.flatMap((entry, index) => {
      const item = recordValue(entry)
      const id = stringValue(item?.id)
      const text = typeof item?.text === 'string' ? item.text : undefined
      const role = item?.role === 'user' || item?.role === 'assistant' ? item.role : undefined
      return id && text !== undefined && role ? [{ id, role, text, order: typeof item?.order === 'number' ? item.order : index + 1 }] : []
    })
  }
  return result
}

function parsePersistedQuestions(value: unknown): Record<string, QuestionBatch[]> | undefined {
  const source = recordValue(value)
  if (!source) return undefined
  const result: Record<string, QuestionBatch[]> = {}
  for (const [mode, entries] of Object.entries(source)) {
    if (!Array.isArray(entries)) continue
    result[mode] = entries.flatMap((entry, index) => {
      const item = recordValue(entry)
      const callId = stringValue(item?.callId)
      const questions = parseQuestions(item?.questions)
      if (!callId || !questions.length) return []
      return [{ callId, questions, answers: Array.isArray(item?.answers) ? item.answers as DshQuestionAnswer['answers'] : undefined, order: typeof item?.order === 'number' ? item.order : entries.length + index + 1 }]
    })
  }
  return result
}

function parsePersistedDrafts(value: unknown): Record<string, Draft | null> | undefined {
  const source = recordValue(value)
  if (!source) return undefined
  const result: Record<string, Draft | null> = {}
  for (const [mode, draft] of Object.entries(source)) {
    const parsed = recordValue(draft)
    if (draft === null || parsed) result[mode] = parsed ?? null
  }
  return result
}

function parsePersistedEventIds(value: unknown): Record<string, string | undefined> {
  const source = recordValue(value)
  if (!source) return {}
  return Object.fromEntries(Object.entries(source).flatMap(([mode, eventId]) => typeof eventId === 'string' ? [[mode, eventId]] : []))
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
  return mode === 'profile' ? '人设' : mode === 'creation' ? '创作' : '复盘'
}

function modePlaceholder(mode: AssistantModeID) {
  return mode === 'profile' ? '告诉我你想建立怎样的人设…' : mode === 'creation' ? '描述你想创作的内容，或告诉我你的目标…' : '告诉我你想从这件作品中复盘什么…'
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

function isStaleAssistantWorkspaceError(error: unknown) {
  const errorCode = assistantWorkspaceErrorCode(error)
  return errorCode === 'assistant_workspace_expired'
    || errorCode === 'assistant_workspace_not_found'
    || errorCode === 'interactive_session_closed'
    || assistantWorkspaceHttpStatus(error) === 404
    || assistantWorkspaceHttpStatus(error) === 410
}

function readPersistedWorkspace(): PersistedWorkspace | null {
  try {
    const raw = sessionStorage.getItem(WORKSPACE_STORAGE_KEY)
    if (!raw) return null
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== 'object' || !('workspace' in value)) return null
    const candidate = value as Partial<PersistedWorkspace>
    if (candidate.workspace !== null && (typeof candidate.workspace !== 'object' || !isMode(candidate.workspace.mode_id) || typeof candidate.workspace.id !== 'string')) return null
    return {
      workspace: candidate.workspace as AssistantWorkspaceState | null,
      lastEventIds: parsePersistedEventIds(candidate.lastEventIds),
      creationType: typeof candidate.creationType === 'string' ? candidate.creationType : undefined,
      mode: isMode(candidate.mode) ? candidate.mode : undefined,
      projectId: typeof candidate.projectId === 'string' ? candidate.projectId : undefined,
      contentId: typeof candidate.contentId === 'string' ? candidate.contentId : undefined,
      composer: typeof candidate.composer === 'string' ? candidate.composer : undefined,
      messagesByMode: parsePersistedMessages(candidate.messagesByMode),
      questionsByMode: parsePersistedQuestions(candidate.questionsByMode),
      draftByMode: parsePersistedDrafts(candidate.draftByMode),
    }
  } catch {
    return null
  }
}

export default function AssistantWorkspace() {
  const { token } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const routeState = (location.state ?? {}) as RouteState
  const routeHasContext = Boolean(routeState.modeId || routeState.projectId || routeState.contentId || routeState.checkpointId || routeState.initialMessage)
  const persisted = readPersistedWorkspace()
  const bootstrapQuery = useQuery({ queryKey: ['assistant-bootstrap'], queryFn: api.assistant.bootstrap, staleTime: 30_000 })
  const queryClient = useQueryClient()
  const bootstrap = bootstrapQuery.data
  const [mode, setMode] = useState<AssistantModeID>(() => isMode(routeState.modeId) ? routeState.modeId : persisted?.mode ?? persisted?.workspace?.mode_id ?? 'profile')
  const [projectId, setProjectId] = useState(routeState.projectId ?? persisted?.projectId ?? persisted?.workspace?.project_id ?? '')
  const [contentId, setContentId] = useState(routeState.contentId ?? persisted?.contentId ?? persisted?.workspace?.content_id ?? '')
  const [composer, setComposer] = useState(routeState.initialMessage ?? persisted?.composer ?? '')
  const [attachments, setAttachments] = useState<PromptAttachment[]>([])
  const [executionProfile, setExecutionProfile] = useState<AgentExecutionProfileID | ''>('')
  const [creationType, setCreationType] = useState(routeHasContext ? '' : persisted?.creationType ?? '')
  const [imageRatio, setImageRatio] = useState<ImageRatio>('auto')
  const [imageCapabilityKey, setImageCapabilityKey] = useState('')
  const [videoEditAssets, setVideoEditAssets] = useState<InputAttachment[]>([])
  const [videoEditBrief, setVideoEditBrief] = useState('')
  const [videoEditPreferences, setVideoEditPreferences] = useState<VideoEditPreferences>({})
  const [videoEditDefaults, setVideoEditDefaults] = useState<VideoEditDefaults>({})
  const [videoEditUploading, setVideoEditUploading] = useState(false)
  const [videoEditUploadFailed, setVideoEditUploadFailed] = useState(false)
  const [workspace, setWorkspace] = useState<AssistantWorkspaceState | null>(() => routeHasContext ? null : persisted?.workspace ?? null)
  const [newProfile, setNewProfile] = useState(false)
  const [conversationPhase, setConversationPhase] = useState<'setup' | 'active'>(() => routeHasContext ? 'setup' : persisted?.workspace ? 'active' : 'setup')
  const [messagesByMode, setMessagesByMode] = useState<Record<string, Message[]>>(persisted?.messagesByMode ?? {})
  const [questionsByMode, setQuestionsByMode] = useState<Record<string, QuestionBatch[]>>(persisted?.questionsByMode ?? {})
  const [draftByMode, setDraftByMode] = useState<Record<string, Draft | null>>(persisted?.draftByMode ?? {})
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const [responding, setResponding] = useState(false)
  const [switching, setSwitching] = useState(false)
  const [pendingMode, setPendingMode] = useState<AssistantModeID | null>(null)
  const [committing, setCommitting] = useState(false)
  const [lastEventIds, setLastEventIds] = useState<Record<string, string | undefined>>(persisted?.lastEventIds ?? {})
  const streamAbort = useRef<AbortController | null>(null)
  const userMessageCounter = useRef(0)
  const lastSubmittedMessage = useRef('')
  const workspaceResetVersion = useRef(0)
  const workspaceCreateKey = useRef<string | null>(null)
  const sendInFlight = useRef(false)
  const imageRatioContextKey = useRef<string | null>(null)
  const conversationOrder = useRef<Record<string, number>>({})

  const nextConversationOrder = (conversationMode: AssistantModeID) => {
    const next = (conversationOrder.current[conversationMode] ?? 0) + 1
    conversationOrder.current[conversationMode] = next
    return next
  }

  useEffect(() => {
    for (const conversationMode of new Set([...Object.keys(messagesByMode), ...Object.keys(questionsByMode)])) {
      const maxMessageOrder = Math.max(0, ...(messagesByMode[conversationMode] ?? []).map((item) => item.order))
      const maxQuestionOrder = Math.max(0, ...(questionsByMode[conversationMode] ?? []).map((item) => item.order))
      conversationOrder.current[conversationMode] = Math.max(conversationOrder.current[conversationMode] ?? 0, maxMessageOrder, maxQuestionOrder)
    }
  }, [messagesByMode, questionsByMode])

  useEffect(() => {
    try {
      sessionStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify({
        workspace,
        lastEventIds,
        creationType,
        mode,
        projectId,
        contentId,
        composer,
        messagesByMode,
        questionsByMode,
        draftByMode,
      } satisfies PersistedWorkspace))
    } catch {
      // Conversation recovery is best effort when browser storage is unavailable or full.
    }
  }, [workspace, lastEventIds, creationType, mode, projectId, contentId, composer, messagesByMode, questionsByMode, draftByMode])

  const projects = bootstrap?.projects ?? []
  const projectsQuery = useQuery({ queryKey: ['projects', 'assistant-home'], queryFn: () => api.projects.list({ status: 'active' }), staleTime: 30_000 })
  const executionProfilesQuery = useAgentExecutionProfiles()
  const platformConfigsQuery = useQuery({ queryKey: ['platform-configs'], queryFn: api.projects.platformConfigs, staleTime: Infinity })
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'ai_entry_attachment' },
    policy: GENERAL_AGENT_ATTACHMENT_POLICY,
    attachments,
    onAttachmentsChange: setAttachments,
  })
  const completeProjects = projects.map((project) => {
    const full = projectsQuery.data?.find((candidate) => candidate.id === project.id)
    return full ? {
      ...project,
      avatar_url: full.avatar_url,
      description: full.description,
      image_ratio: full.image_ratio,
      portrait_reference_image: full.portrait_reference_image,
    } : project
  })
  const readyProjects = completeProjects.filter((project) => project.profileReady)
  const selectionMode = pendingMode ?? mode
  const selectableProjects = selectionMode === 'profile' ? completeProjects : readyProjects
  const selectedProject = selectableProjects.find((project) => project.id === projectId) ?? (selectionMode !== 'profile' && selectableProjects.length === 1 ? selectableProjects[0] : undefined)
  const creationTypes = CREATION_TYPES[selectedProject?.platform ?? ''] ?? []
  const selectedCreationType = creationTypes.some((item) => item.value === creationType) ? creationType : creationTypes[0]?.value ?? ''
  const isVideoEditCreation = mode === 'creation' && selectedCreationType === 'video-edit'
  const usesImageSettings = mode === 'creation' && Boolean(selectedProject) && !isVideoEditCreation
  const imageCapabilitiesQuery = useImageCapabilities(usesImageSettings)
  const selectedPlatformConfig = platformConfigsQuery.data?.find((config) => config.id === selectedProject?.platform)
  const supportedImageRatios = selectedPlatformConfig?.supported_image_ratios ?? []
  const effectiveImageCapabilityKey = imageCapabilityKey || imageCapabilitiesQuery.defaultCapability || ''
  const worksQuery = useInfiniteQuery({
    queryKey: ['assistant-workspace-works', selectedProject?.id],
    initialPageParam: undefined as { created_at: string; id: string } | undefined,
    queryFn: ({ pageParam }) => api.assistant.works(selectedProject!.id, pageParam, 25),
    getNextPageParam: (page) => page.has_more ? page.next_cursor : undefined,
    enabled: selectionMode === 'review' && Boolean(selectedProject),
    staleTime: 30_000,
  })
  const works = worksQuery.data?.pages.flatMap((page) => page.items) ?? []
  const resolvedContentId = pendingMode === 'review'
    ? contentId
    : conversationPhase === 'active' ? workspace?.content_id ?? contentId : contentId
  const selectedWork = works.find((work) => work.id === resolvedContentId) ?? (selectionMode === 'review' && works.length === 1 ? works[0] : undefined)
  const reviewVideoQuery = useQuery({
    queryKey: ['assistant-review-video', selectedWork?.id],
    queryFn: async () => Promise.all([api.tasks.get(selectedWork!.id), api.tasks.files(selectedWork!.id)]),
    enabled: mode === 'review' && selectedProject?.platform === 'video-edit' && Boolean(selectedWork),
    staleTime: 30_000,
  })
  const activeMessages = messagesByMode[mode] ?? []
  const activeQuestions = questionsByMode[mode] ?? []
  const pendingQuestions = activeQuestions.filter((question) => !question.answers)
  const activeDraft = draftByMode[mode]
  const draftPending = Boolean(activeDraft)
  const activeMode = bootstrap?.modes.find((candidate) => candidate.id === mode)
  const isRuntimeUnavailable = activeMode?.disabledReason === '助手运行环境暂不可用，请稍后重试'
  const modePrerequisiteBlocked = Boolean(activeMode && !activeMode.available && activeMode.disabledReason && !isRuntimeUnavailable)
  const missingContextInstruction = activeMode?.available
    ? mode === 'creation' && !selectedProject
      ? '先选择人设后即可输入消息'
      : mode === 'creation' && selectedProject && creationTypes.length === 0
        ? '当前人设暂不支持首页创作，请选择其他人设'
      : mode === 'review' && !selectedProject
        ? '先选择人设后即可输入消息'
        : mode === 'review' && !selectedWork
          ? '先选择作品后即可输入消息'
          : undefined
    : undefined
  const composerBlocked = modePrerequisiteBlocked || Boolean(missingContextInstruction) || Boolean(activeMode && !activeMode.available)
  const composerPlaceholder = modePrerequisiteBlocked
    ? mode === 'creation' ? '先完成新人设创建后即可输入消息' : '先完成当前模式所需条件后即可输入消息'
    : missingContextInstruction ?? modePlaceholder(mode)
  const modePrerequisiteAction = mode === 'review' && selectableProjects.length > 0
    ? { mode: 'creation' as const, label: '开始创作' }
    : { mode: 'profile' as const, label: '去建立人设' }
  const draftProject = activeDraft?.project && typeof activeDraft.project === 'object' && !Array.isArray(activeDraft.project)
    ? activeDraft.project as Record<string, unknown>
    : undefined
  const profilePlatform = selectedProject?.platform ?? stringValue(draftProject?.platform)
  const isVideoEditProfile = mode === 'profile' && profilePlatform === 'video-edit'
  const canSend = Boolean(composer.trim()) && Boolean(activeMode?.available) && (mode === 'profile' || Boolean(selectedProject)) && (mode !== 'review' || Boolean(selectedWork)) && !sending && !responding && !committing && !composerBlocked && !draftPending

  const resetExpiredWorkspace = (preservedComposer?: string) => {
    workspaceResetVersion.current += 1
    setWorkspace(null)
    setConversationPhase('setup')
    setLastEventIds({})
    setResponding(false)
    setError(null)
    if (preservedComposer !== undefined) setComposer(preservedComposer)
    setMessagesByMode((current) => ({ ...current, [mode]: [] }))
    setQuestionsByMode((current) => ({ ...current, [mode]: [] }))
    setDraftByMode((current) => ({ ...current, [mode]: null }))
  }

  useEffect(() => {
    if (!bootstrap || routeState.modeId || persisted?.workspace?.mode_id || workspace || newProfile || projectId || readyProjects.length === 0) return
    setProjectId(readyProjects[0].id)
  }, [bootstrap, persisted?.workspace?.mode_id, projectId, readyProjects, routeState.modeId, newProfile, workspace])

  useEffect(() => {
    if (!bootstrap || routeState.modeId || persisted?.workspace?.mode_id || workspace || !isMode(bootstrap.defaultModeId)) return
    setMode(bootstrap.defaultModeId)
  }, [bootstrap, persisted?.workspace?.mode_id, routeState.modeId, workspace])

  useEffect(() => {
    if (executionProfile || executionProfilesQuery.isLoading) return
    const available = (executionProfilesQuery.data ?? []).filter((profile) => profile.available)
    const preferred = available.find((profile) => profile.id === 'effective') ?? available[0]
    if (preferred) setExecutionProfile(preferred.id)
  }, [executionProfile, executionProfilesQuery.data, executionProfilesQuery.isLoading])

  useEffect(() => {
    if (mode === 'profile' && selectedProject) setVideoEditDefaults(selectedProject.video_edit_defaults ?? {})
  }, [mode, selectedProject?.id])

  useEffect(() => {
    if (conversationPhase !== 'setup' || mode !== 'creation') return
    if (creationTypes.length && !creationTypes.some((item) => item.value === creationType)) setCreationType(creationTypes[0].value)
    if (!creationTypes.length && creationType) setCreationType('')
  }, [conversationPhase, mode, creationType, creationTypes])

  useEffect(() => {
    if (!usesImageSettings) {
      if (imageRatio !== 'auto') setImageRatio('auto')
      if (imageCapabilityKey) setImageCapabilityKey('')
      imageRatioContextKey.current = null
      return
    }
    const contextKey = `${selectedProject?.id ?? ''}:${selectedProject?.platform ?? ''}:${selectedProject?.image_ratio ?? ''}:${selectedPlatformConfig?.default_image_ratio ?? ''}`
    const configuredRatio = selectedProject?.image_ratio || selectedPlatformConfig?.default_image_ratio || 'auto'
    const nextRatio = configuredRatio === 'auto' || supportedImageRatios.length === 0 || supportedImageRatios.includes(configuredRatio)
      ? configuredRatio as ImageRatio
      : 'auto'
    if (imageRatioContextKey.current !== contextKey) {
      imageRatioContextKey.current = contextKey
      if (imageRatio !== nextRatio) setImageRatio(nextRatio)
      return
    }
    if (imageRatio !== 'auto' && supportedImageRatios.length > 0 && !supportedImageRatios.includes(imageRatio)) setImageRatio(nextRatio)
  }, [imageCapabilityKey, imageRatio, selectedPlatformConfig?.default_image_ratio, selectedProject?.id, selectedProject?.image_ratio, supportedImageRatios, usesImageSettings])

  useEffect(() => {
    if (mode !== 'creation' && attachments.length) setAttachments([])
  }, [mode, attachments.length])

  useEffect(() => {
    if (conversationPhase === 'setup' && mode !== 'profile' && !projectId && selectableProjects.length === 1) setProjectId(selectableProjects[0].id)
  }, [conversationPhase, mode, projectId, selectableProjects])

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
          if (event.event === 'session.expired' || event.event === 'expired') {
            const preservedComposer = lastSubmittedMessage.current || undefined
            lastSubmittedMessage.current = ''
            resetExpiredWorkspace(preservedComposer)
            continue
          }
          if (event.event === 'turn.start') setResponding(true)
          if (event.event === 'turn.end' || event.event === 'question.opened' || event.event === 'session.error') {
            setResponding(false)
            lastSubmittedMessage.current = ''
          }
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
            const order = nextConversationOrder(eventMode)
            setMessagesByMode((current) => {
              const list = current[eventMode] ?? []
              if (event.id && list.some((item) => item.id === event.id)) return current
              if (role === 'user' && event.id) {
                const optimisticIndex = list.findIndex((item) => item.id.startsWith('optimistic-') && item.text === text)
                if (optimisticIndex >= 0) {
                  const next = [...list]
                  next[optimisticIndex] = { ...next[optimisticIndex], id: event.id, role, text }
                  return { ...current, [eventMode]: next }
                }
              }
              return { ...current, [eventMode]: [...list, { id: event.id ?? `${role}-${Date.now()}-${list.length}`, role, text, order }] }
            })
          } else if (event.event === 'question.opened') {
            const callId = stringValue(data.call_id)
            const questions = parseQuestions(data.questions)
            if (callId && questions.length) {
              const order = nextConversationOrder(eventMode)
              setQuestionsByMode((current) => ({ ...current, [eventMode]: [...(current[eventMode] ?? []).filter((item) => item.callId !== callId), { callId, questions, order }] }))
            }
          } else if (event.event === 'draft.ready' && data.draft && typeof data.draft === 'object') {
            setDraftByMode((current) => ({ ...current, [eventMode]: data.draft as Draft }))
          }
        }
      } catch (streamError) {
        if (!controller.signal.aborted) {
          setResponding(false)
          if (streamError instanceof InteractiveStreamError && (streamError.status === 404 || streamError.status === 410)) {
            resetExpiredWorkspace()
            setError(null)
          } else {
            setError(streamError instanceof Error ? streamError.message : '助手连接已中断')
          }
        }
      }
    })()
    return () => controller.abort()
  }, [mode, token, workspace])

  const sendMessage = async (override?: string) => {
    const text = (override ?? composer).trim()
    if (!text || !canSend || sendInFlight.current) return
    sendInFlight.current = true
    const resetVersion = workspaceResetVersion.current
    const submittedAttachments = attachmentController.toInputAttachments()
    const contextLines = mode === 'creation' && selectedCreationType
      ? [
          ...(selectedCreationType === 'seednote' ? [] : [`创作类型：${selectedCreationType}`]),
          ...(isVideoEditCreation ? [
            `剪辑要求：${videoEditBrief.trim() || '按本次创作需求自动决定'}`,
            `剪辑偏好：${JSON.stringify(videoEditPreferences)}`,
            `已上传素材：${videoEditAssets.map((asset) => asset.file_name).filter(Boolean).join('、') || '待上传'}`,
          ] : []),
        ]
      : []
    if (submittedAttachments.length) {
      contextLines.push(`已附加素材：${submittedAttachments.map((asset) => asset.file_name || asset.type).join('、')}`)
    }
    const messageText = contextLines.length ? `${text}\n\n${contextLines.join('\n')}` : text
    const optimisticID = `optimistic-${++userMessageCounter.current}`
    const order = nextConversationOrder(mode)
    setSending(true)
    setError(null)
    lastSubmittedMessage.current = text
    setComposer('')
    setMessagesByMode((current) => ({ ...current, [mode]: [...(current[mode] ?? []), { id: optimisticID, role: 'user', text: messageText, order }] }))
    let createdWorkspace: AssistantWorkspaceState | null = null
    try {
      let currentWorkspace = workspace
      if (!currentWorkspace) {
        if (!workspaceCreateKey.current) workspaceCreateKey.current = crypto.randomUUID()
        currentWorkspace = await api.assistant.createWorkspace({ mode_id: mode, ...(selectedProject ? { project_id: selectedProject.id } : {}), ...(selectedWork ? { content_id: selectedWork.id } : {}), ...(routeState.checkpointId ? { checkpoint_id: routeState.checkpointId } : {}) }, workspaceCreateKey.current)
        workspaceCreateKey.current = null
        createdWorkspace = currentWorkspace
      }
      await api.assistant.workspaceMessage(currentWorkspace.id, mode, messageText)
      if (!workspace && workspaceResetVersion.current === resetVersion) {
        const acceptedWorkspace: AssistantWorkspaceState = {
          ...currentWorkspace,
          mode_id: mode,
          ...(selectedProject ? { project_id: selectedProject.id } : {}),
          ...(selectedWork ? { content_id: selectedWork.id } : {}),
        }
        setWorkspace(acceptedWorkspace)
        setConversationPhase('active')
      }
      setResponding(true)
    } catch (sendError) {
      if (createdWorkspace) void Promise.resolve(api.assistant.closeWorkspace(createdWorkspace.id)).catch(() => undefined)
      setComposer(text)
      setMessagesByMode((current) => ({ ...current, [mode]: (current[mode] ?? []).filter((message) => message.id !== optimisticID) }))
      const errorCode = assistantWorkspaceErrorCode(sendError)
      if (isStaleAssistantWorkspaceError(sendError)) {
        resetExpiredWorkspace()
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
    const contextProjectChanged = context && Object.prototype.hasOwnProperty.call(context, 'projectId') && (context.projectId ?? '') !== projectId
    const contextContentChanged = context && Object.prototype.hasOwnProperty.call(context, 'contentId') && (context.contentId ?? '') !== contentId
    if (nextMode === mode && !pendingMode && !contextProjectChanged && !contextContentChanged || switching || committing || sending || responding) return
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
    if (conversationPhase === 'setup') {
      setMode(nextMode)
      if (context && Object.prototype.hasOwnProperty.call(context, 'projectId')) setProjectId(context.projectId ?? '')
      setContentId('')
      return
    }
    if (!workspace) return
    const requestedProjectId = context && Object.prototype.hasOwnProperty.call(context, 'projectId') ? context.projectId ?? '' : selectedProject?.id ?? projectId
    const requestedContentId = context && Object.prototype.hasOwnProperty.call(context, 'contentId') ? context.contentId ?? '' : selectedWork?.id ?? contentId
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
      const contextChanged = (workspace.project_id ?? '') !== (nextWorkspace.project_id ?? '') || (workspace.content_id ?? '') !== (nextWorkspace.content_id ?? '')
      const sessionChanged = workspace.sessions[nextMode] !== nextWorkspace.sessions[nextMode]
      if (contextChanged) {
        setMessagesByMode((current) => ({ ...current, [nextMode]: [] }))
        setQuestionsByMode((current) => ({ ...current, [nextMode]: [] }))
        setDraftByMode((current) => ({ ...current, [nextMode]: null }))
      }
      if (sessionChanged) setLastEventIds((current) => ({ ...current, [nextMode]: undefined }))
      setMode(nextMode)
      setPendingMode(null)
      setProjectId(nextWorkspace.project_id ?? projectId)
      setContentId(nextWorkspace.content_id ?? contentId)
      setWorkspace(nextWorkspace)
      setResponding(false)
    } catch (switchError) {
      const errorCode = assistantWorkspaceErrorCode(switchError)
      if (isStaleAssistantWorkspaceError(switchError)) {
        resetExpiredWorkspace()
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
      if (isStaleAssistantWorkspaceError(answerError)) {
        resetExpiredWorkspace()
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
        const draftPayload = activeDraft.payload && typeof activeDraft.payload === 'object' && !Array.isArray(activeDraft.payload)
          ? activeDraft.payload
          : activeDraft
        const payload = structuredClone(draftPayload) as Draft
        if (mode === 'creation') {
          if (!selectedCreationType) {
            setError('当前人设不支持可用的创作类型')
            return
          }
          const plan = payload.plan && typeof payload.plan === 'object' && !Array.isArray(payload.plan) ? payload.plan as Record<string, unknown> : payload
          plan.content_type = selectedCreationType
          if (executionProfile) plan.execution_profile = executionProfile
          if (usesImageSettings) {
            if (imageRatio) plan.image_ratio = imageRatio
            if (effectiveImageCapabilityKey) plan.image_capability_key = effectiveImageCapabilityKey
          }
          const composerAssets = selectedCreationType === 'video-edit' ? [] : attachmentController.toInputAttachments().map((asset) => ({
            type: asset.type,
            ...(asset.asset_id ? { asset_id: asset.asset_id } : {}),
            ...(asset.upload_id ? { upload_id: asset.upload_id } : {}),
            ...(asset.file_name ? { file_name: asset.file_name } : {}),
            ...(asset.content_type ? { mime_type: asset.content_type } : {}),
            ...(asset.size !== undefined ? { file_size: asset.size } : {}),
            ...(asset.instruction ? { instruction: asset.instruction } : {}),
          }))
          if (composerAssets.length) {
            const existingAssets = Array.isArray(plan.assets) ? plan.assets : []
            const existingKeys = new Set(existingAssets.flatMap((asset) => {
              if (!asset || typeof asset !== 'object' || Array.isArray(asset)) return []
              const item = asset as Record<string, unknown>
              return [stringValue(item.asset_id), stringValue(item.upload_id), stringValue(item.file_name)].filter(Boolean)
            }))
            plan.assets = [...existingAssets, ...composerAssets.filter((asset) => {
              const identity = asset.asset_id || asset.upload_id || asset.file_name
              if (!identity || existingKeys.has(identity)) return false
              existingKeys.add(identity)
              return true
            })]
          }
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
          name: stringValue(result.project?.name) ?? '新人设',
          platform: stringValue(result.project?.platform) ?? 'wechat',
          profileRevision: typeof result.project?.profile_revision === 'number' ? result.project.profile_revision : 1,
          profileReady: true,
          ...(isVideoEditProfile ? { video_edit_defaults: videoEditDefaults } : {}),
          works: [],
        }
        setProjectId(committedProjectID)
        setNewProfile(false)
        setContentId('')
        setWorkspace((current) => current ? { ...current, project_id: committedProjectID, content_id: '' } : current)
        queryClient.setQueryData<{ projects: AssistantProject[] } | undefined>(['assistant-bootstrap'], (current) => current ? { ...current, projects: [...current.projects.filter((project) => project.id !== committedProjectID), committedProject] } : current)
        void queryClient.invalidateQueries({ queryKey: ['assistant-bootstrap'] })
      }
      if (mode !== 'creation') setAttachments([])
      const committedTaskID = stringValue(result.task?.id)
      setDraftByMode((current) => ({ ...current, [mode]: null }))
      if (mode === 'creation' && committedTaskID) navigate(`/tasks/${encodeURIComponent(committedTaskID)}`)
    } catch (commitError) {
      setError(commitError instanceof Error ? commitError.message : '确认失败')
    } finally {
      setCommitting(false)
    }
  }

  const chooseCreationType = (value: string) => {
    setCreationType(value)
    if (value !== 'video-edit') {
      setVideoEditAssets([])
      setVideoEditBrief('')
      setVideoEditPreferences({})
      setVideoEditUploadFailed(false)
    }
  }

  const creationTypeSelector = mode === 'creation' && selectedProject?.platform === 'video-edit' ? (
    <label className="flex w-full max-w-xs min-w-0 items-center gap-1.5 text-xs text-muted-foreground sm:w-auto">
      <span className="sr-only">创作类型</span>
      <NativeSelect className="h-8 w-full min-w-28 max-w-44 text-xs sm:w-auto" aria-label="创作类型" value={selectedCreationType} disabled={switching || committing || sending || responding || draftPending} onChange={(event) => chooseCreationType(event.target.value)}>
        {creationTypes.map((item) => <NativeSelectOption key={item.value} value={item.value}>{item.label}</NativeSelectOption>)}
      </NativeSelect>
    </label>
  ) : null

  const resetCreationProject = (value: string | null) => {
    const nextValue = value ?? ''
    setProjectId(nextValue)
    setContentId('')
    setVideoEditDefaults(projects.find((project) => project.id === nextValue)?.video_edit_defaults ?? {})
    setCreationType('')
    setVideoEditAssets([])
    setVideoEditBrief('')
    setVideoEditPreferences({})
    setVideoEditUploadFailed(false)
  }
  const selectReviewContent = (value: string) => {
    setContentId(value)
    if (pendingMode === 'review' && value && projectId) {
      void chooseMode('review', { projectId, contentId: value })
    }
  }
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

  const selectPersona = (nextProjectId: string) => {
    if (switching || committing || sending || responding) return
    setNewProfile(false)
    setError(null)
    setContentId('')
    if (workspace && (mode === 'profile' || mode === 'creation')) {
      void chooseMode(mode, { projectId: nextProjectId })
      return
    }
    if (workspace && mode === 'review') {
      void chooseMode('review', { projectId: nextProjectId, contentId: '' })
      return
    }
    setProjectId(nextProjectId)
  }

  const startNewProfile = async () => {
    if (switching || committing || sending || responding) return
    setNewProfile(true)
    setProjectId('')
    setContentId('')
    setAttachments([])
    setError(null)
    if (!workspace) {
      setMode('profile')
      return
    }
    const previousProjectId = workspace.project_id ?? projectId
    setSwitching(true)
    try {
      await api.assistant.closeWorkspace(workspace.id)
      streamAbort.current?.abort()
      setWorkspace(null)
      setConversationPhase('setup')
      setLastEventIds({})
      setMessagesByMode({})
      setQuestionsByMode({})
      setDraftByMode({})
      setPendingMode(null)
      setResponding(false)
      setMode('profile')
    } catch (closeError) {
      setNewProfile(false)
      setProjectId(previousProjectId)
      setError(closeError instanceof Error ? closeError.message : '当前对话无法结束，请稍后重试')
    } finally {
      setSwitching(false)
    }
  }

  const editPersona = (nextProjectId: string) => {
    setNewProfile(false)
    setError(null)
    setContentId('')
    if (workspace) {
      void chooseMode('profile', { projectId: nextProjectId })
      return
    }
    setProjectId(nextProjectId)
    setMode('profile')
  }

  const viewPersonaWorks = (nextProjectId: string) => {
    navigate(`/tasks?project_id=${encodeURIComponent(nextProjectId)}`)
  }

  const composerParameters = (
    <TaskComposerParameters
      execution={{
        profiles: executionProfilesQuery.data ?? [],
        value: executionProfile,
        onChange: setExecutionProfile,
        loading: executionProfilesQuery.isLoading,
        disabled: executionProfilesQuery.isError,
        taskType: selectedProject?.platform,
      }}
      image={usesImageSettings ? {
        ratios: supportedImageRatios,
        ratio: imageRatio,
        onRatioChange: (value) => setImageRatio(value),
        capabilities: imageCapabilitiesQuery.items,
        capabilityKey: effectiveImageCapabilityKey,
        onCapabilityChange: setImageCapabilityKey,
        loading: imageCapabilitiesQuery.isLoading,
        disabled: imageCapabilitiesQuery.isError,
      } : undefined}
      disabled={switching || committing || sending || responding || draftPending}
      label="选择参数"
    />
  )
  const creationComposerParameters = mode === 'creation' ? composerParameters : null
  const composerContextBar = (
    <AssistantContextBar
      phase="locked"
      mode={mode}
      modes={bootstrap?.modes ?? []}
      projects={completeProjects}
      projectId={selectedProject?.id ?? (newProfile ? '' : projectId)}
      works={works}
      contentId={selectedWork?.id ?? contentId}
      creationType={selectedCreationType}
      creationTypeOptions={creationTypes}
      loadingProjects={!selectedProject && (bootstrapQuery.isLoading || projectsQuery.isLoading)}
      loadingWorks={worksQuery.isLoading}
    />
  )
  const activeReviewSelectors = workspace && pendingMode === 'review' ? <WorkspaceSelectors mode="review" projects={selectableProjects} projectId={selectedProject?.id ?? projectId} contentId={contentId} works={works} disabled={switching} onProject={selectPersona} onContent={selectReviewContent} onMore={() => void worksQuery.fetchNextPage()} hasMore={Boolean(worksQuery.hasNextPage)} /> : null

  const personaRail = bootstrap ? (
    <PersonaRail
      projects={readyProjects}
      selectedProjectId={selectedProject?.id ?? (newProfile ? '' : projectId)}
      onSelect={selectPersona}
      onCreate={startNewProfile}
      onViewWorks={viewPersonaWorks}
      onEditProfile={editPersona}
    />
  ) : null
  const confirmationDraft = activeDraft && mode === 'creation'
    ? withCreationConfirmationSettings(activeDraft, {
      execution_profile: executionProfile || undefined,
      ...(usesImageSettings ? {
        image_ratio: imageRatio,
        image_capability_key: effectiveImageCapabilityKey || undefined,
      } : {}),
    })
    : activeDraft

  return (
    <AgentPromptDropProvider>
      <main data-testid="assistant-workspace" className="flex h-full min-h-0 w-full flex-col overflow-hidden bg-background">
      {!workspace ? (
        <section className="mx-auto flex min-h-0 w-full max-w-5xl flex-1 flex-col px-4 pb-3 md:px-8">
          <div data-testid="assistant-conversation" className="assistant-scroll min-h-0 flex flex-1 flex-col overflow-y-auto pb-1 pt-5 md:pb-2 md:pt-8">
            <HomeStage project={selectedProject} hasReadyPersona={readyProjects.length > 0} />
            {personaRail}
            {routeHasContext && mode === 'review' && <WorkspaceSelectors mode={selectionMode} projects={selectableProjects} projectId={selectedProject?.id ?? projectId} contentId={selectedWork?.id ?? contentId} works={works} onProject={resetCreationProject} onContent={setContentId} onMore={() => void worksQuery.fetchNextPage()} hasMore={Boolean(worksQuery.hasNextPage)} />}
            {!activeMode?.available && activeMode?.disabledReason && !isRuntimeUnavailable && <div className="mx-auto mt-4 flex w-full max-w-2xl flex-col items-center gap-3 rounded-lg border border-border bg-muted/40 px-4 py-3 sm:flex-row sm:justify-between"><p role="status" className="text-left text-sm leading-6 text-muted-foreground">{activeMode.disabledReason}</p><Button type="button" variant="outline" size="sm" className="shrink-0" onClick={selectModePrerequisiteAction}>{modePrerequisiteAction.label}</Button></div>}
            {bootstrapQuery.isError && <div className="mt-3 flex flex-wrap items-center justify-center gap-3"><p role="alert" className="text-sm text-destructive">首页暂时无法加载，请稍后重试。</p><Button type="button" variant="outline" size="sm" disabled={bootstrapQuery.isFetching} onClick={() => void bootstrapQuery.refetch()}>{bootstrapQuery.isFetching ? <Loader2 className="size-4 animate-spin" /> : '重试加载'}</Button></div>}
            {bootstrapQuery.isLoading && <p role="status" className="sr-only">正在加载首页</p>}
            {isVideoEditProfile && <VideoEditProjectDefaultsPanel value={videoEditDefaults} onChange={setVideoEditDefaults} />}
            {error && <p role="alert" className="mt-4 text-center text-sm text-destructive">{error}</p>}
            {mode === 'review' && selectedProject?.platform === 'video-edit' && selectedWork && <VideoEditReviewResults query={reviewVideoQuery} />}
            {videoEditInputs}
          </div>
          <Composer fadeAbove={false} value={composer} attachments={attachments} attachmentController={attachmentController} attachmentsEnabled={mode === 'creation'} contextBar={composerContextBar} placeholder={composerPlaceholder} inputDisabled={sending || switching || committing || composerBlocked} sendDisabled={!canSend} status={missingContextInstruction} trailingTools={<>{creationTypeSelector}{creationComposerParameters}</>} onChange={setComposer} onAttachmentsChange={setAttachments} onSend={() => void sendMessage()} />
        </section>
      ) : (
        <section className="mx-auto flex min-h-0 w-full max-w-6xl min-w-0 flex-1 flex-col px-4 pb-3 md:px-8">
          <div data-testid="assistant-conversation" className="assistant-scroll min-h-0 flex-1 overflow-y-auto py-4" aria-live="polite">
            {mode === 'review' && selectedProject?.platform === 'video-edit' && selectedWork && (
              <VideoEditReviewResults query={reviewVideoQuery} />
            )}
            <Conversation messages={activeMessages} questions={activeQuestions} responding={responding} disabled={draftPending} userAvatarURL={personaImageURL(selectedProject)} onAnswer={submitAnswers} />
            {activeReviewSelectors}
            {activeDraft && confirmationDraft && <DraftConfirmationCard mode={mode} draft={confirmationDraft} committing={committing} disabled={mode === 'creation' && selectedCreationType === 'video-edit' && (videoEditUploading || videoEditUploadFailed || videoEditAssets.length === 0)} onCommit={() => void commitDraft()} onContinue={() => { setDraftByMode((current) => ({ ...current, [mode]: null })); setError(null) }} />}
            {error && <p role="alert" className="mt-4 text-sm text-destructive">{error}</p>}
          </div>
          <Composer value={composer} attachments={attachments} attachmentController={attachmentController} attachmentsEnabled={mode === 'creation'} contextBar={composerContextBar} placeholder={draftPending ? '请先选择确认卡片中的下一步' : pendingQuestions.length ? '请先完成上方问题' : '输入消息…'} inputDisabled={sending || switching || committing || draftPending || pendingQuestions.length > 0} sendDisabled={!canSend || pendingQuestions.length > 0} trailingTools={<>{creationTypeSelector}{creationComposerParameters}</>} onChange={setComposer} onAttachmentsChange={setAttachments} onSend={() => void sendMessage()} />
        </section>
      )}
      </main>
    </AgentPromptDropProvider>
  )
}

function VideoEditReviewResults({ query }: { query: { data?: [Task, TaskFile[]]; isLoading: boolean; isError: boolean } }) {
  if (query.isLoading) return <p role="status" className="py-4 text-sm text-muted-foreground">正在加载作品产物</p>
  if (query.isError) return <p role="alert" className="py-4 text-sm text-destructive">作品产物暂时无法加载。</p>
  if (!query.data) return null
  return <div className="mb-6"><TaskResults task={query.data[0]} files={query.data[1]} /></div>
}

function personaImageURL(project?: AssistantProject) {
  return project?.avatar_url || project?.portrait_reference_image?.download_url || project?.portrait_reference_image?.preview_url || ''
}

function platformAgentID(platform: string) {
  if (platform === 'wechat') return 'wechat-article'
  if (platform === 'video-edit') return 'montage'
  return platform
}

function platformMeta(platform: string) {
  return { label: platformDisplayName(platform, '创作'), agentID: platformAgentID(platform) }
}

function platformIntroduction(platform: string) {
  const agentID = platformAgentID(platform)
  return profileLabAgents.find((agent) => agent.id === agentID)?.detail ?? `${platformDisplayName(platform, '创作')}内容创作`
}

function HomeStage({ project, hasReadyPersona }: { project?: AssistantProject; hasReadyPersona: boolean }) {
  const imageURL = personaImageURL(project)
  const meta = platformMeta(project?.platform ?? '')
  return <section aria-label="当前人设" className="mx-auto flex w-full max-w-xl flex-1 flex-col justify-end px-2 pb-8 pt-6 text-left md:pb-10">
    <div className="flex shrink-0 items-center justify-center gap-4 sm:gap-6">
    <div className="relative flex size-24 shrink-0 items-center justify-center overflow-hidden rounded-2xl border border-border bg-muted/60 shadow-sm sm:size-28">
      {imageURL ? <SignedImage src={imageURL} alt={`${project?.name ?? '当前人设'}形象`} className="size-full object-cover" showLoading={false} fallbackIcon={<UserRound className="size-10 text-muted-foreground" />} /> : <UserRound aria-hidden="true" className="size-12 text-muted-foreground" />}
    </div>
    <div className="min-w-0">
      <p className="text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">{project ? '当前人设' : '准备开始'}</p>
      <h1 className="mt-2 text-xl font-semibold tracking-tight sm:text-2xl">{project?.name ?? '神秘人'}</h1>
      <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground">{project?.description || (hasReadyPersona ? '选择一个人设，开始你的下一次创作。' : '为了创建更好的作品，需要先创建您的人设')}</p>
      {project && <span className="mt-3 inline-flex items-center gap-1.5 text-xs text-muted-foreground"><AgentIconStack agentIds={[meta.agentID]} maxVisible={1} compact />{meta.label}</span>}
    </div>
    </div>
  </section>
}

function PersonaRail({ projects, selectedProjectId, onSelect, onCreate, onViewWorks, onEditProfile }: {
  projects: AssistantProject[]
  selectedProjectId: string
  onSelect: (projectId: string) => void
  onCreate: () => void
  onViewWorks: (projectId: string) => void
  onEditProfile: (projectId: string) => void
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [hasContentAbove, setHasContentAbove] = useState(false)
  const [hasContentBelow, setHasContentBelow] = useState(false)

  useEffect(() => {
    const element = scrollRef.current
    if (!element) return

    const updateOverflow = () => {
      setHasContentAbove(element.scrollTop > 1)
      setHasContentBelow(element.scrollHeight - element.scrollTop > element.clientHeight + 1)
    }
    updateOverflow()
    element.addEventListener('scroll', updateOverflow, { passive: true })
    window.addEventListener('resize', updateOverflow)
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(updateOverflow)
    observer?.observe(element)
    return () => {
      observer?.disconnect()
      element.removeEventListener('scroll', updateOverflow)
      window.removeEventListener('resize', updateOverflow)
    }
  }, [projects.length])

  return <section aria-label="首页人设选择" className="mx-auto mt-auto w-full max-w-5xl shrink-0 pb-1">
    <div className="mb-3 flex items-center justify-between px-1">
      <p className="text-sm font-medium">选择人设</p>
      <span className="text-xs text-muted-foreground">{projects.length ? `${projects.length} 个可用人设` : '还没有可用人设'}</span>
    </div>
    <div className="relative">
      <div ref={scrollRef} className="assistant-scroll persona-card-scroll grid min-h-[5.25rem] max-h-[14.125rem] grid-cols-2 min-[700px]:grid-cols-3 min-[1600px]:grid-cols-4 min-[1900px]:grid-cols-5 min-[2200px]:grid-cols-6 auto-rows-[5.25rem] gap-2 overflow-y-auto pr-1" data-testid="persona-card-scroll" tabIndex={0}>
        <button type="button" aria-label="创建新人设" onClick={onCreate} className={`group flex h-full min-w-0 items-center gap-2 rounded-xl border border-dashed px-2.5 py-2 text-left transition-colors hover:border-primary/60 hover:bg-primary/[0.04] ${selectedProjectId === '' ? 'border-primary bg-primary/[0.05]' : 'border-border'}`}>
          <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"><Plus className="size-4" /></span>
          <span className="min-w-0"><span className="block truncate text-sm font-semibold">创建新人设</span><span className="mt-0.5 block truncate text-xs leading-4 text-muted-foreground">聊一聊你的定位与风格</span></span>
        </button>
        {projects.map((project) => {
          const meta = platformMeta(project.platform)
          const imageURL = personaImageURL(project)
          const selected = selectedProjectId === project.id
          return <article key={project.id} className={`group relative flex h-full min-w-0 flex-col rounded-xl border bg-card p-2 transition-shadow ${selected ? 'border-primary shadow-sm ring-1 ring-primary/25' : 'border-border hover:shadow-sm'}`}>
            <button type="button" aria-label={`选择人设：${project.name}`} aria-pressed={selected} onClick={() => onSelect(project.id)} className="flex w-full shrink-0 items-start gap-2 text-left">
              <div className="flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-muted">
                {imageURL ? <SignedImage src={imageURL} alt="" className="size-full object-cover" showLoading={false} fallbackIcon={<UserRound className="size-5 text-muted-foreground" />} /> : <UserRound aria-hidden="true" className="size-5 text-muted-foreground" />}
              </div>
              <span className="min-w-0 flex-1"><span className="flex items-center gap-1 text-sm font-semibold"><span className="truncate">{project.name}</span><AgentIconStack agentIds={[meta.agentID]} maxVisible={1} compact /></span><span className="block truncate text-xs leading-4 text-muted-foreground">{project.description || platformIntroduction(project.platform)}</span></span>
            </button>
            <div className="mt-auto flex shrink-0 items-center justify-end gap-1 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100">
              <Button type="button" variant="ghost" size="sm" aria-label={`查看作品：${project.name}`} onClick={() => onViewWorks(project.id)} className="h-7 px-1.5 text-[11px]">查看作品</Button>
              <Button type="button" variant="ghost" size="sm" aria-label={`调整人设：${project.name}`} onClick={() => onEditProfile(project.id)} className="h-7 px-1.5 text-[11px]">调整人设</Button>
            </div>
          </article>
        })}
      </div>
      {hasContentAbove && <div data-testid="persona-card-fade-top" aria-hidden="true" className="pointer-events-none absolute inset-x-0 top-0 h-12 bg-gradient-to-b from-background via-background/80 to-transparent" />}
      {hasContentBelow && <div data-testid="persona-card-fade-bottom" aria-hidden="true" className="pointer-events-none absolute inset-x-0 bottom-0 h-12 bg-gradient-to-t from-background via-background/80 to-transparent" />}
    </div>
  </section>
}

function DraftConfirmationCard({ mode, draft, committing, disabled, onCommit, onContinue }: { mode: AssistantModeID; draft: Draft; committing: boolean; disabled: boolean; onCommit: () => void; onContinue: () => void }) {
  const isCreation = mode === 'creation'
  const isProfile = mode === 'profile'
  const title = isCreation ? '确认创作' : isProfile ? '保存人设' : '确认复盘结论'
  const buttonLabel = isCreation ? '创作作品' : isProfile ? '保存人设' : '保存复盘结论'
  const continueLabel = isCreation ? '继续讨论' : '继续深入访谈'
  return <section role="region" aria-label={title} className="mt-6 rounded-xl border border-primary/30 bg-primary/[0.04] p-4">
    <div className="flex items-center gap-2 text-sm font-medium"><Check className="size-4 text-emerald-600" />{title}</div>
    <p className="mt-2 text-xs leading-5 text-muted-foreground">{isCreation ? '确认后会按这份计划创建作品，也可以继续讨论细节。' : isProfile ? '确认后保存当前人设，也可以继续补充访谈内容。' : '确认后保存当前结构化结果。'}</p>
    {isCreation || mode === 'review' ? <div className="mt-4 rounded-lg border border-border/70 bg-background/70 p-3"><AssistantDraftSummary draft={draft as unknown as InteractiveDraft} mode={mode} compact /></div> : <ProfileDraftSummary draft={draft} />}
    <div className="mt-4 flex flex-wrap items-center gap-2">
      <Button size="sm" disabled={committing || disabled} onClick={onCommit}>{committing ? <Loader2 className="size-4 animate-spin" /> : buttonLabel}</Button>
      {isCreation || isProfile ? <Button type="button" variant="outline" size="sm" disabled={committing} onClick={onContinue}>{continueLabel}</Button> : null}
    </div>
  </section>
}

function withCreationConfirmationSettings(draft: Draft, settings: Record<string, unknown>): Draft {
  const sourcePayload = draft.payload && typeof draft.payload === 'object' && !Array.isArray(draft.payload)
    ? draft.payload as Record<string, unknown>
    : draft
  const sourcePlan = sourcePayload.plan && typeof sourcePayload.plan === 'object' && !Array.isArray(sourcePayload.plan)
    ? sourcePayload.plan as Record<string, unknown>
    : sourcePayload
  const nextPlan = { ...sourcePlan }
  for (const [key, value] of Object.entries(settings)) {
    if (value !== undefined && value !== '') nextPlan[key] = value
  }
  return {
    ...draft,
    payload: { ...sourcePayload, plan: nextPlan },
  }
}

function ProfileDraftSummary({ draft }: { draft: Draft }) {
  const project = draft.project && typeof draft.project === 'object' && !Array.isArray(draft.project) ? draft.project as Record<string, unknown> : undefined
  const profile = draft.profile && typeof draft.profile === 'object' && !Array.isArray(draft.profile) ? draft.profile as Record<string, unknown> : undefined
  const values = [stringValue(project?.name), stringValue(project?.platform), stringValue(profile?.identity), stringValue(profile?.positioning)].filter(Boolean)
  return values.length ? <p className="mt-4 text-sm leading-6 text-muted-foreground">{values.join(' · ')}</p> : null
}

function WorkspaceSelectors({ mode, projects, projectId, contentId, works, disabled = false, onProject, onContent, onMore, hasMore }: { mode: AssistantModeID; projects: AssistantProject[]; projectId: string; contentId: string; works: AssistantProject['works']; disabled?: boolean; onProject: (value: string) => void; onContent: (value: string) => void; onMore: () => void; hasMore: boolean }) {
  return <div className="mx-auto mt-6 flex w-full max-w-xs flex-col items-stretch gap-3"><NativeSelect className="w-full" aria-label="选择人设" value={projectId} disabled={disabled} onChange={(event) => onProject(event.target.value)}><NativeSelectOption value="">{mode === 'profile' ? '新建人设' : '选择人设'}</NativeSelectOption>{projects.map((project) => <NativeSelectOption key={project.id} value={project.id}>{project.name}</NativeSelectOption>)}</NativeSelect>{mode === 'review' && <NativeSelect className="w-full" aria-label="选择作品" value={contentId} disabled={disabled} onChange={(event) => onContent(event.target.value)}><NativeSelectOption value="">选择作品</NativeSelectOption>{works.map((work) => <NativeSelectOption key={work.id} value={work.id}>{work.title}</NativeSelectOption>)}</NativeSelect>}{mode === 'review' && hasMore && <Button type="button" variant="ghost" size="sm" className="self-end" aria-label="加载更多作品" disabled={disabled} onClick={onMore}><ChevronDown className="size-4" />加载更多作品</Button>}</div>
}

function Composer({ value, attachments, attachmentController, attachmentsEnabled = true, fadeAbove = true, contextBar, placeholder, inputDisabled, sendDisabled, status, leadingTools, trailingTools, onChange, onAttachmentsChange, onSend }: {
  value: string
  attachments: PromptAttachment[]
  attachmentController: ReturnType<typeof usePromptAttachments>
  attachmentsEnabled?: boolean
  fadeAbove?: boolean
  contextBar?: ReactNode
  placeholder: string
  inputDisabled: boolean
  sendDisabled: boolean
  status?: ReactNode
  leadingTools?: ReactNode
  trailingTools?: ReactNode
  onChange: (value: string) => void
  onAttachmentsChange: (attachments: PromptAttachment[]) => void
  onSend: () => void
}) {
  return <div data-slot="input-group" className={`relative shrink-0 pt-2 ${fadeAbove ? 'before:pointer-events-none before:absolute before:inset-x-0 before:-top-8 before:h-8 before:bg-gradient-to-b before:from-transparent before:via-background/75 before:to-background' : ''}`}>
    {status && <p role="status" className="pb-2 text-xs text-muted-foreground">{status}</p>}
    <AgentPromptInput
      value={{ prompt: value, attachments }}
      onChange={(next) => {
        onChange(next.prompt)
        onAttachmentsChange(next.attachments)
      }}
      onSubmit={() => onSend()}
      attachmentController={attachmentController}
      attachmentPolicy={GENERAL_AGENT_ATTACHMENT_POLICY}
      attachmentsEnabled={attachmentsEnabled}
      contextBar={contextBar}
      placeholder={placeholder}
      submitLabel="发送消息"
      ariaLabel="发送消息"
      submitting={attachmentController.uploading}
      disabled={inputDisabled}
      submitDisabled={sendDisabled || !value.trim()}
      leadingTools={leadingTools}
      trailingTools={trailingTools}
    />
  </div>
}

function Conversation({ messages, questions, responding, disabled, userAvatarURL, onAnswer }: { messages: Message[]; questions: QuestionBatch[]; responding: boolean; disabled: boolean; userAvatarURL?: string; onAnswer: (batch: QuestionBatch, answer: DshQuestionAnswer) => Promise<void> }) {
  const endRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [messages.length, questions.length, responding])

  if (messages.length === 0 && questions.length === 0) return <div className="flex min-h-[18rem] items-center justify-center text-sm text-muted-foreground">输入你的需求后开始</div>
  const entries = [
    ...messages.map((message) => ({ kind: 'message' as const, order: message.order, message })),
    ...questions.map((batch) => ({ kind: 'question' as const, order: batch.order, batch })),
  ].sort((left, right) => left.order - right.order)
  return <div className="min-w-0 space-y-5">{entries.map((entry) => entry.kind === 'message'
    ? <div key={entry.message.id} className={`flex min-w-0 gap-3 ${entry.message.role === 'user' ? 'justify-end' : ''}`}><div className={`flex min-w-0 max-w-[min(900px,100%)] gap-3 ${entry.message.role === 'user' ? 'flex-row-reverse' : ''}`}><div className={`mt-0.5 flex size-7 shrink-0 items-center justify-center overflow-hidden rounded-full ${entry.message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}>{entry.message.role === 'user' ? userAvatarURL ? <SignedImage src={userAvatarURL} alt="当前人设头像" className="size-7 rounded-full object-cover" fallbackIcon={<UserRound className="size-3.5" />} fallbackClassName="bg-primary text-primary-foreground" /> : <UserRound className="size-3.5" /> : <Bot className="size-3.5" />}</div><div className={`min-w-0 max-w-full rounded-2xl px-4 py-3 text-sm leading-6 ${entry.message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted/70'} [&_pre]:max-w-full [&_pre]:overflow-x-auto [&_table]:block [&_table]:max-w-full [&_table]:overflow-x-auto`}><Streamdown>{entry.message.text}</Streamdown></div></div></div>
    : <DshQuestionComposer key={entry.batch.callId} questions={entry.batch.questions} submitted={Boolean(entry.batch.answers)} answers={entry.batch.answers} disabled={disabled} onSubmit={(answer) => onAnswer(entry.batch, answer)} />)}{responding && <div role="status" className="flex items-center gap-2 px-10 text-sm text-muted-foreground"><Bot className="size-4" /><Loader2 className="size-4 animate-spin" />AI 正在处理…</div>}<div ref={endRef} aria-hidden="true" className="h-px" /></div>
}
