import { useCallback, useContext, useEffect, useRef, useState, type MutableRefObject } from 'react'
import { UNSAFE_NavigationContext, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Streamdown } from 'streamdown'
import {
  AlertTriangle,
  ArrowRight,
  Ban,
  Check,
  ChevronDown,
  Loader2,
  MessageSquare,
  RotateCcw,
  Send,
  Sparkles,
  SquarePen,
  UserRound,
  Volume2,
  VolumeX,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { api } from '@/lib/api'
import type {
  InteractiveAgent,
  InteractiveDraft,
} from '@/lib/api/interactive'
import type { AssistantModeID } from '@/lib/api/assistant'
import { streamInteractiveSession } from '@/lib/interactive-events'
import { clearPortraitCreationHandoff, readPortraitCreationHandoff } from '@/lib/portrait-project-handoff'
import { assistantMessageHandoffKey, clearAssistantMessageHandoff, readAssistantMessageHandoff } from '@/lib/assistant-message-handoff'
import { useAuth } from '@/contexts/AuthContext'
import { DshQuestionComposer, type DshQuestion, type DshQuestionAnswer, type DshQuestionOption } from '@/components/dsh-user-questions/DshQuestionComposer'
import AssistantHome from './AssistantHome'
import { AssistantDraftSummary, assistantDraftFields } from '@/components/assistant/AssistantDraftSummary'

type Message = { id: string; role: 'assistant' | 'user'; text: string }
type QuestionBatch = { callId: string; questions: DshQuestion[]; answers?: DshQuestionAnswer['answers']; eventId?: number }
type TimelineItem =
  | { kind: 'message'; message: Message; eventId?: number }
  | { kind: 'question'; callId: string; eventId?: number }
  | { kind: 'draft'; id: 'draft'; eventId?: number }
  | { kind: 'activity'; id: string; eventId?: number; callId?: string; status: 'started' | 'running' | 'waiting' | 'completed' | 'stopped' | 'error'; text: string }
type PendingSubmission =
  | { kind: 'message'; sessionId: string; message: Message; status: 'pending' | 'accepted' | 'failed' }
  | { kind: 'answer'; sessionId: string; callId: string; answers: DshQuestionAnswer['answers']; status: 'pending' | 'accepted' | 'failed' }
type RunState = 'idle' | 'starting' | 'running' | 'waiting_answer' | 'error' | 'expired' | 'closed'
type TerminalRunState = Extract<RunState, 'error' | 'expired' | 'closed'>
type JsonRecord = Record<string, unknown>

const NEW_MESSAGE_SOUND_STORAGE_KEY = 'anban:chat:new-message-sound'
const CHAT_SCROLL_THRESHOLD = 80

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

function eventIdValue(value: unknown): number | undefined {
  const id = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : NaN
  return Number.isSafeInteger(id) && id > 0 ? id : undefined
}

function messageFingerprint(role: Message['role'], text: string): string {
  return `${role}\u0000${text}`
}

function legacyMessageCandidates(messages: Message[] = []): Map<string, string[]> {
  const candidates = new Map<string, string[]>()
  for (const message of messages) {
    if (eventIdValue(message.id) !== undefined) continue
    const key = messageFingerprint(message.role, message.text)
    candidates.set(key, [...(candidates.get(key) ?? []), message.id])
  }
  return candidates
}

function parseData(eventData: string): JsonRecord {
  try {
    const value: unknown = JSON.parse(eventData)
    return isRecord(value) ? value : { text: String(value) }
  } catch {
    return { text: eventData }
  }
}

type PersistedInteractiveSession = {
  sessionId?: string | null
  lastEventId?: string
  draftReadyEventId?: string
  createIdempotencyKey?: string
  turnActive?: boolean
  terminalState?: TerminalRunState
  terminalMessage?: string
  draftNeedsReconfirmation?: boolean
  messages?: Message[]
  questionBatches?: QuestionBatch[]
  timeline?: TimelineItem[]
  draft?: InteractiveDraft | null
}

function assistantSessionStorageKey(userId: string, mode: string, projectId?: string, contentId?: string): string {
  return `anban:assistant-session:${[userId, mode, projectId ?? 'new', contentId ?? 'none'].map(encodeURIComponent).join(':')}`
}

function readPersistedInteractiveSession(key: string): PersistedInteractiveSession | null {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return null
    const value: unknown = JSON.parse(raw)
    if (!isRecord(value)) return null
    const sessionId = typeof value.sessionId === 'string' && value.sessionId ? value.sessionId : null
    const createIdempotencyKey = typeof value.createIdempotencyKey === 'string' && value.createIdempotencyKey ? value.createIdempotencyKey : undefined
    if (!sessionId && !createIdempotencyKey) return null
    const messages = Array.isArray(value.messages)
      ? value.messages.flatMap((item): Message[] => {
        if (!isRecord(item)) return []
        const text = stringValue(item.text)
        const role = item.role === 'assistant' || item.role === 'user' ? item.role : undefined
        if (!text || !role) return []
        return [{ id: stringValue(item.id) ?? crypto.randomUUID(), role, text }]
      })
      : undefined
    const questionBatches = Array.isArray(value.questionBatches)
      ? value.questionBatches.flatMap((item): QuestionBatch[] => {
        if (!isRecord(item)) return []
        const callId = stringValue(item.callId)
        const questions = Array.isArray(item.questions) ? item.questions.flatMap((question) => {
          const parsed = parseQuestion(question)
          return parsed ? [parsed] : []
        }) : []
        if (!callId || questions.length === 0) return []
        return [{ callId, questions, answers: item.answers === undefined ? undefined : parseAnswers(item.answers), eventId: eventIdValue(item.eventId) }]
      })
      : undefined
    const timeline = Array.isArray(value.timeline)
      ? value.timeline.flatMap((item): TimelineItem[] => {
        if (!isRecord(item) || typeof item.kind !== 'string') return []
        if (item.kind === 'message' && isRecord(item.message)) {
          const text = stringValue(item.message.text)
          const role = item.message.role === 'assistant' || item.message.role === 'user' ? item.message.role : undefined
          const id = stringValue(item.message.id) ?? crypto.randomUUID()
          return text && role ? [{ kind: 'message', message: { id, role, text }, eventId: eventIdValue(item.eventId) ?? eventIdValue(id) }] : []
        }
        if (item.kind === 'question') {
          const callId = stringValue(item.callId)
          return callId ? [{ kind: 'question', callId, eventId: eventIdValue(item.eventId) }] : []
        }
        if (item.kind === 'draft') return [{ kind: 'draft', id: 'draft', eventId: eventIdValue(item.eventId) ?? eventIdValue(value.draftReadyEventId) }]
        if (item.kind === 'activity') {
          const id = stringValue(item.id)
          const text = stringValue(item.text)
          const status = item.status === 'started' || item.status === 'running' || item.status === 'waiting' || item.status === 'completed' || item.status === 'stopped' || item.status === 'error' ? item.status : undefined
          return id && text && status ? [{ kind: 'activity', id, eventId: eventIdValue(item.eventId), status, text }] : []
        }
        return []
      })
      : undefined
    const draft = isRecord(value.draft) ? normalizeDraft(value.draft) : undefined
    return {
      sessionId,
      lastEventId: typeof value.lastEventId === 'string' ? value.lastEventId : undefined,
      draftReadyEventId: typeof value.draftReadyEventId === 'string' ? value.draftReadyEventId : undefined,
      createIdempotencyKey,
      turnActive: value.turnActive === true,
      terminalState: isTerminalRunState(value.terminalState) ? value.terminalState : undefined,
      terminalMessage: stringValue(value.terminalMessage),
      draftNeedsReconfirmation: value.draftNeedsReconfirmation === true,
      messages,
      questionBatches,
      timeline,
      draft,
    }
  } catch {
    return null
  }
}

function persistInteractiveSession(key: string, session: PersistedInteractiveSession): void {
  try {
    const previous = readPersistedInteractiveSession(key)
    sessionStorage.setItem(key, JSON.stringify({ ...previous, ...session }))
  } catch {
    // Session persistence is an enhancement; the live SSE stream remains authoritative.
  }
}

function replacePersistedInteractiveSession(key: string, session: PersistedInteractiveSession): void {
  try {
    const next = { ...session }
    if (next.sessionId == null) delete next.sessionId
    sessionStorage.setItem(key, JSON.stringify(next))
  } catch {
    // Session persistence is an enhancement; the live SSE stream remains authoritative.
  }
}

function hasSavedMaterial(session: PersistedInteractiveSession | null): boolean {
  return Boolean(session?.messages?.length || session?.questionBatches?.length || session?.draft?.ready)
}

function recoveryPrompt(session: PersistedInteractiveSession | null): string {
  const messages = (session?.messages ?? []).slice(-24).map((message) => `${message.role === 'user' ? '用户' : '助手'}：${message.text}`).join('\n')
  const answers = (session?.questionBatches ?? []).filter((batch) => batch.answers?.length).map((batch) => `${batch.callId}：${batch.answers?.map((answer) => `${answer.id}=${answer.selected.join('、')}${answer.custom ? `（${answer.custom}）` : ''}`).join('；')}`).join('\n')
  const pendingQuestions = (session?.questionBatches ?? []).filter((batch) => !batch.answers?.length).flatMap((batch) => batch.questions.map((question) => {
    const options = question.options?.map((option) => option.description ? `${option.label}（${option.description}）` : option.label).join('、')
    return `${question.question}${options ? `（选项：${options}）` : ''}`
  })).join('\n')
  const draft = session?.draft ? JSON.stringify({
    revision: session.draft.revision,
    project: session.draft.project,
    profile: session.draft.profile,
    progress: session.draft.progress,
    missing_fields: session.draft.missing_fields,
    inferred_fields: session.draft.inferred_fields,
    memory_append: session.draft.memory_append,
  }) : ''
  const prompt = [
    '这是一次从浏览器本地保存材料恢复的新创作项目画像会话。原 runtime 上下文已经失效，下面内容只能作为用户提供的草稿，不能视为服务端已确认的结果。',
    '请先告诉用户已恢复哪些内容，再让用户补充或确认；完成后重新生成一个需要重新确认的新草稿。不要假装恢复了原会话，也不要直接沿用旧确认凭据。',
    messages ? `保存的历史消息：\n${messages}` : '',
    answers ? `保存的问答：\n${answers}` : '',
    pendingQuestions ? `保存的待回答问题：\n${pendingQuestions}` : '',
    draft ? `保存的草稿：\n${draft}` : '',
    '请继续画像流程，优先从保存材料中缺失或需要确认的部分开始。',
  ].filter(Boolean).join('\n\n')
  return prompt.length > 12000 ? `${prompt.slice(0, 11960)}\n\n（历史材料过长，已截取；请继续向用户核对未显示的部分。）` : prompt
}

function displayInteractiveMessage(text: string): string {
  return text.startsWith('这是一次从浏览器本地保存材料恢复的新创作项目画像会话。')
    ? '我已带入上次保存的画像材料，请根据这些内容继续并重新确认。'
    : text
}

function resumeEventId(session: PersistedInteractiveSession | null): string | undefined {
  const timeline = session?.timeline ?? []
  const timelineMessageIds = new Set(timeline.flatMap((item) => item.kind === 'message' ? [item.message.id] : []))
  const timelineQuestionIds = new Set(timeline.flatMap((item) => item.kind === 'question' ? [item.callId] : []))
  const missingPersistedItems = (session?.messages ?? []).some((message) => !timelineMessageIds.has(message.id))
    || (session?.questionBatches ?? []).some((batch) => !timelineQuestionIds.has(batch.callId))
  const timelineNeedsReplay = timeline.some((item) => item.kind !== 'draft' && item.eventId === undefined)
    || missingPersistedItems
    || (timeline.length === 0 && Boolean(session?.messages?.length || session?.questionBatches?.length))
  if (timelineNeedsReplay) return undefined
  if (!session?.draft?.ready || !session.draftReadyEventId) return session?.lastEventId
  const readyEventId = Number(session.draftReadyEventId)
  if (!Number.isSafeInteger(readyEventId) || readyEventId < 1) return session.lastEventId
  return readyEventId > 1 ? String(readyEventId - 1) : undefined
}

function clearPersistedInteractiveSession(key: string): void {
  try {
    sessionStorage.removeItem(key)
  } catch {
    // Ignore storage policies such as private browsing.
  }
}

function restoreTimeline(session: PersistedInteractiveSession | null): TimelineItem[] {
  const existing = session?.timeline ?? []
  const messages = session?.messages ?? []
  const questions = session?.questionBatches ?? []
  const messageById = new Map(messages.map((message) => [message.id, message]))
  const questionById = new Map(questions.map((batch) => [batch.callId, batch]))
  const normalizedExisting = existing.map((item): TimelineItem => {
    if (item.kind === 'message') {
      const message = messageById.get(item.message.id) ?? item.message
      return { ...item, message, eventId: item.eventId ?? eventIdValue(message.id) }
    }
    if (item.kind === 'question') {
      return { ...item, eventId: item.eventId ?? questionById.get(item.callId)?.eventId }
    }
    if (item.kind === 'draft') {
      return { ...item, eventId: item.eventId ?? eventIdValue(session?.draftReadyEventId) }
    }
    return item
  })
  const messageIds = new Set(normalizedExisting.flatMap((item) => item.kind === 'message' ? [item.message.id] : []))
  const questionIds = new Set(normalizedExisting.flatMap((item) => item.kind === 'question' ? [item.callId] : []))
  const hasDraft = normalizedExisting.some((item) => item.kind === 'draft')
  return sortTimeline([
    ...normalizedExisting,
    ...messages.filter((message) => !messageIds.has(message.id)).map((message): TimelineItem => ({ kind: 'message', message, eventId: eventIdValue(message.id) })),
    ...questions.filter((batch) => !questionIds.has(batch.callId)).map((batch): TimelineItem => ({ kind: 'question', callId: batch.callId, eventId: batch.eventId })),
    ...(session?.draft?.ready && !hasDraft ? [{ kind: 'draft' as const, id: 'draft' as const, eventId: eventIdValue(session.draftReadyEventId) }] : []),
  ])
}

function timelineEventId(item: TimelineItem): number | undefined {
  return item.eventId
}

function sortTimeline(items: TimelineItem[]): TimelineItem[] {
  const ordered = items.filter((item) => timelineEventId(item) !== undefined)
    .map((item, index) => ({ item, index }))
    .sort((a, b) => (timelineEventId(a.item) ?? 0) - (timelineEventId(b.item) ?? 0) || a.index - b.index)
    .map(({ item }) => item)
  let orderedIndex = 0
  return items.map((item) => timelineEventId(item) === undefined ? item : ordered[orderedIndex++]!)
}

function upsertTimeline(items: TimelineItem[], next: TimelineItem, matches: (item: TimelineItem) => boolean): TimelineItem[] {
  const index = items.findIndex(matches)
  if (index < 0) return sortTimeline([...items, next])
  const merged = { ...items[index], ...next } as TimelineItem
  return sortTimeline(items.map((item, itemIndex) => itemIndex === index ? merged : item))
}

function formatAgentActivity(name?: string): string {
  const label = name?.replace(/^mcp__anban__/, '').replace(/[_-]+/g, ' ').trim()
  return label ? `Agent 正在执行：${label}` : 'Agent 正在工作…'
}

function isLostInteractiveSession(data: JsonRecord): boolean {
  const code = stringValue(data.code)?.toLowerCase() ?? ''
  const message = stringValue(data.message)?.toLowerCase() ?? ''
  return code === 'session_not_found' || code === 'session_closed' || /session (not found|closed)|会话(不存在|已关闭|已失效)/.test(message)
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
    payload: isRecord(draft.payload) ? draft.payload : undefined,
    memory_append: stringValue(draft.memory_append),
  }
}

function hasObjectContent(value: unknown): boolean {
  if (typeof value === 'string') return value.trim().length > 0
  if (typeof value === 'number') return value !== 0
  if (typeof value === 'boolean') return value
  if (Array.isArray(value)) return value.length > 0
  if (!isRecord(value)) return value != null
  return Object.values(value).some((item) => hasObjectContent(item))
}

function isEmptyDraft(draft: InteractiveDraft): boolean {
  return draft.revision <= 0
    && !hasObjectContent(draft.project)
    && !hasObjectContent(draft.profile)
    && !hasObjectContent(draft.progress)
    && !draft.name
    && !draft.fields
    && !draft.memory_append
    && (draft.missing_fields?.length ?? 0) === 0
    && (draft.inferred_fields?.length ?? 0) === 0
    && !draft.ready
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
      return label ? [{
        label,
        description: stringValue(option.description),
        recommended: option.recommended === true ? true : undefined,
      }] : []
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

function isTerminalRunState(value: unknown): value is TerminalRunState {
  return value === 'error' || value === 'expired' || value === 'closed'
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

function assistantCheckpointPayload(draft: InteractiveDraft, mode: AssistantModeID): Record<string, unknown> {
  if (mode !== 'profile') {
    if (draft.payload && isRecord(draft.payload)) return draft.payload
    const fields = draft.fields ?? {}
    return mode === 'creation' ? { plan: fields } : { review: fields }
  }
  return Object.fromEntries(Object.entries({
    revision: draft.revision,
    project: draft.project,
    profile: draft.profile,
    progress: draft.progress,
    missing_fields: draft.missing_fields,
    inferred_fields: draft.inferred_fields,
    ready: draft.ready,
    name: draft.name,
    fields: draft.fields,
    memory_append: draft.memory_append,
  }).filter(([, value]) => value !== undefined))
}

function assistantCheckpointSummary(mode: AssistantModeID, draft: InteractiveDraft): string {
  const label = mode === 'profile' ? '画像' : mode === 'creation' ? '创作计划' : '复盘'
  const name = draft.name ?? (isRecord(draft.project) ? stringValue(draft.project.name) : undefined)
  const fieldCount = Object.keys(mode === 'profile' ? draft.fields ?? draft.progress ?? {} : assistantDraftFields(draft, mode)).length
  return name ? `${label}草稿：${name}` : `${label}草稿，已整理 ${fieldCount} 项`
}

function ChatConversation() {
  const { token, user } = useAuth()
  const queryClient = useQueryClient()
  const location = useLocation()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const requestedAgent = params.get('agent') || ''
  const assistantSession = params.get('assistant_session') === '1'
  const rawAssistantMode = params.get('mode')
  const assistantModeId = rawAssistantMode === 'profile' || rawAssistantMode === 'creation' || rawAssistantMode === 'review'
    ? rawAssistantMode as AssistantModeID
    : undefined
  const intent = assistantSession
    ? assistantModeId === 'creation' ? 'plan-creation' : assistantModeId === 'review' ? 'review-content' : params.get('project_id') ? 'update-project-profile' : 'create-project'
    : params.get('intent') || 'create-project'
  const projectId = params.get('project_id') || undefined
  const contentId = params.get('content_id') || undefined
  const checkpointId = params.get('checkpoint_id') || undefined
  const isProjectUpdate = intent === 'update-project-profile'
  const [selectedAgent, setSelectedAgent] = useState(assistantModeId ?? (requestedAgent || (location.pathname === '/projects/create' ? 'project-creator' : '')))
  const sessionStorageKey = assistantSessionStorageKey(user?.id ?? 'signed-out', assistantModeId ?? selectedAgent, projectId, contentId)
  const [persistedSessionKey] = useState(sessionStorageKey)
  const [persistedSession] = useState(() => {
    if (checkpointId) {
      clearPersistedInteractiveSession(sessionStorageKey)
      return null
    }
    return readPersistedInteractiveSession(sessionStorageKey)
  })
  const [sessionId, setSessionId] = useState<string | null>(persistedSession?.terminalState ? null : persistedSession?.sessionId ?? null)
  const [runState, setRunState] = useState<RunState>(persistedSession?.terminalState ?? 'idle')
  const [lastEventId, setLastEventId] = useState(persistedSession?.lastEventId)
  const [draftReadyEventId, setDraftReadyEventId] = useState(persistedSession?.draftReadyEventId)
  const [messages, setMessages] = useState<Message[]>(persistedSession?.messages ?? [])
  const [questionBatches, setQuestionBatches] = useState<QuestionBatch[]>(persistedSession?.questionBatches ?? [])
  const [timeline, setTimeline] = useState<TimelineItem[]>(() => restoreTimeline(persistedSession))
  const [draft, setDraft] = useState<InteractiveDraft | null>(persistedSession?.draft ?? null)
  const [composer, setComposer] = useState('')
  const [streamError, setStreamError] = useState<string | null>(persistedSession?.terminalMessage ?? null)
  const [draftNeedsReconfirmation, setDraftNeedsReconfirmation] = useState(persistedSession?.draftNeedsReconfirmation === true)
  const [commitError, setCommitError] = useState<string | null>(null)
  const [committing, setCommitting] = useState(false)
  const [cancelDialogOpen, setCancelDialogOpen] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [cancelError, setCancelError] = useState<string | null>(null)
  const [committed, setCommitted] = useState(false)
  const [createdTaskId, setCreatedTaskId] = useState<string | null>(null)
  const [sessionProject, setSessionProject] = useState<{ id?: string; name?: string } | null>(null)
  const [modeMenuOpen, setModeMenuOpen] = useState(false)
  const [soundEnabled, setSoundEnabled] = useState(() => {
    try {
      return localStorage.getItem(NEW_MESSAGE_SOUND_STORAGE_KEY) !== 'off'
    } catch {
      return true
    }
  })
  const [newMessageCount, setNewMessageCount] = useState(0)
  const [modeSwitchError, setModeSwitchError] = useState<string | null>(null)
  const [restartNonce, setRestartNonce] = useState(0)
  const [checkpointRecoveryPending, setCheckpointRecoveryPending] = useState(false)
  const [recoveryAvailable, setRecoveryAvailable] = useState(() => Boolean(persistedSession?.draftNeedsReconfirmation || (!persistedSession?.sessionId && hasSavedMaterial(persistedSession))))
  const idempotencyKeyRef = useRef(crypto.randomUUID())
  const createIdempotencyKeyRef = useRef(persistedSession?.createIdempotencyKey ?? crypto.randomUUID())
  const sessionRef = useRef<string | null>(persistedSession?.terminalState ? null : persistedSession?.sessionId ?? null)
  const startPromiseRef = useRef<{ key: string; promise: Promise<string | null> } | null>(null)
  const abandonedStartKeysRef = useRef(new Set<string>())
  const runGenerationRef = useRef(0)
  const streamAbortRef = useRef<AbortController | null>(null)
  const disposedRef = useRef(false)
  const cleanupGenerationRef = useRef(0)
  const committedRef = useRef(false)
  const hasInputRef = useRef(false)
  const confirmationTokenRef = useRef<string | null>(null)
  const recoveryPromptRef = useRef<string | null>(null)
  const recoveryMessagePromiseRef = useRef<Promise<void> | null>(null)
  const portraitHandoffIdeaRef = useRef<string | null>(null)
  const initialAssistantMessageSentRef = useRef(false)
  const recoveryDraftRef = useRef<InteractiveDraft | null>(null)
  const sendingMessageRef = useRef(false)
  const pendingSubmissionRef = useRef<PendingSubmission | null>(null)
  const currentTurnRef = useRef<number | undefined>(undefined)
  const failedToolTurnsRef = useRef(new Set<number>())
  const unscopedToolFailureRef = useRef(false)
  const legacyMessageCandidatesRef = useRef(legacyMessageCandidates(persistedSession?.messages))
  const messageAliasesRef = useRef(new Map<string, string>())
  const messagesRef = useRef(messages)
  const questionBatchesRef = useRef(questionBatches)
  const timelineRef = useRef(timeline)
  const scrollRegionRef = useRef<HTMLDivElement>(null)
  const followTailRef = useRef(true)
  const audioContextRef = useRef<AudioContext | null>(null)
  const pendingSoundRef = useRef(false)
  const draftRef = useRef(draft)
  const draftReadyEventIdRef = useRef(draftReadyEventId)
  const lastEventIdRef = useRef(lastEventId)
  messagesRef.current = messages
  questionBatchesRef.current = questionBatches
  timelineRef.current = timeline
  draftRef.current = draft
  draftReadyEventIdRef.current = draftReadyEventId
  lastEventIdRef.current = lastEventId
  const updateMessagesNow = useCallback((update: (items: Message[]) => Message[]) => {
    const next = update(messagesRef.current)
    messagesRef.current = next
    setMessages(next)
  }, [])
  const updateQuestionsNow = useCallback((update: (items: QuestionBatch[]) => QuestionBatch[]) => {
    const next = update(questionBatchesRef.current)
    questionBatchesRef.current = next
    setQuestionBatches(next)
  }, [])
  const updateTimelineNow = useCallback((update: (items: TimelineItem[]) => TimelineItem[]) => {
    const next = update(timelineRef.current)
    timelineRef.current = next
    setTimeline(next)
  }, [])
  const [sendingMessage, setSendingMessage] = useState(false)
  const [turnActive, setTurnActive] = useState(false)

  const getAudioContext = useCallback(() => {
    if (audioContextRef.current) return audioContextRef.current
    const AudioContextConstructor = window.AudioContext
      ?? (window as Window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!AudioContextConstructor) return null
    const context = new AudioContextConstructor()
    audioContextRef.current = context
    return context
  }, [])

  const playNotificationSound = useCallback(() => {
    if (!soundEnabled) return
    const context = getAudioContext()
    if (!context || context.state !== 'running') {
      pendingSoundRef.current = true
      return
    }
    const now = context.currentTime
    const gain = context.createGain()
    gain.gain.setValueAtTime(0.0001, now)
    gain.gain.exponentialRampToValueAtTime(0.045, now + 0.015)
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.28)
    gain.connect(context.destination)
    for (const [frequency, offset] of [[660, 0], [880, 0.08]] as const) {
      const oscillator = context.createOscillator()
      oscillator.type = 'sine'
      oscillator.frequency.setValueAtTime(frequency, now + offset)
      oscillator.connect(gain)
      oscillator.start(now + offset)
      oscillator.stop(now + 0.3)
    }
  }, [getAudioContext, soundEnabled])

  useEffect(() => {
    const unlockAudio = () => {
      if (!soundEnabled) return
      const context = getAudioContext()
      if (!context) return
      void context.resume().then(() => {
        if (pendingSoundRef.current) {
          pendingSoundRef.current = false
          playNotificationSound()
        }
      }).catch(() => undefined)
    }
    window.addEventListener('pointerdown', unlockAudio)
    window.addEventListener('keydown', unlockAudio)
    return () => {
      window.removeEventListener('pointerdown', unlockAudio)
      window.removeEventListener('keydown', unlockAudio)
    }
  }, [getAudioContext, playNotificationSound, soundEnabled])

  useEffect(() => () => {
    void audioContextRef.current?.close().catch(() => undefined)
  }, [])

  const isNearBottom = useCallback(() => {
    const element = scrollRegionRef.current
    if (!element) return true
    return element.scrollHeight - element.scrollTop - element.clientHeight <= CHAT_SCROLL_THRESHOLD
  }, [])

  const scrollToLatest = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const element = scrollRegionRef.current
    if (!element) return
    followTailRef.current = true
    setNewMessageCount(0)
    if (typeof element.scrollTo === 'function') element.scrollTo({ top: element.scrollHeight, behavior })
  }, [])

  const handleScroll = useCallback(() => {
    const nearBottom = isNearBottom()
    followTailRef.current = nearBottom
    if (nearBottom) setNewMessageCount(0)
  }, [isNearBottom])

  const notifyIncomingMessage = useCallback(() => {
    if (!followTailRef.current && !isNearBottom()) {
      setNewMessageCount((count) => count + 1)
    } else {
      followTailRef.current = true
    }
    playNotificationSound()
  }, [isNearBottom, playNotificationSound])

  useEffect(() => {
    const element = scrollRegionRef.current
    if (!element || !followTailRef.current) return
    const frame = window.requestAnimationFrame(() => {
      if (followTailRef.current && typeof element.scrollTo === 'function') element.scrollTo({ top: element.scrollHeight, behavior: 'smooth' })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [timeline.length])

  const bootstrapQuery = useQuery({
    queryKey: ['assistant-bootstrap'],
    queryFn: api.assistant.bootstrap,
    enabled: assistantSession,
    staleTime: 30_000,
  })
  const agentsQuery = useQuery({
    queryKey: ['interactive-agents'],
    queryFn: api.interactive.agents,
    enabled: !assistantSession,
    staleTime: 30_000,
  })
  const agents = agentsQuery.data ?? []
  const selectableAgents: InteractiveAgent[] = assistantSession
    ? (bootstrapQuery.data?.modes ?? []).map((mode) => ({ id: mode.id, display_name: mode.displayName, description: mode.description, available: mode.available }))
    : agents.filter((agent) => agent.available !== false)
  const activeAgent = selectableAgents.find((agent) => agent.id === selectedAgent)
  const pendingQuestions = questionBatches.filter((batch) => !batch.answers)
  const assistantMode = bootstrapQuery.data?.modes.find((mode) => mode.id === assistantModeId)
  const assistantCheckpointQuery = useQuery({
    queryKey: ['assistant-checkpoint', assistantModeId ?? '', projectId ?? '', contentId ?? ''],
    queryFn: () => api.assistant.latestCheckpoint({
      mode_id: assistantModeId!,
      ...(projectId ? { project_id: projectId } : {}),
      ...(contentId ? { content_id: contentId } : {}),
    }),
    enabled: assistantSession && Boolean(assistantMode?.available)
      && (assistantModeId === 'profile' || Boolean(projectId))
      && (assistantModeId !== 'review' || Boolean(contentId)),
    staleTime: 30_000,
  })
  const assistantRecoveryCheckpoint = assistantSession ? assistantCheckpointQuery.data : undefined
  const dirty = Boolean(sessionId && !committed)

  useNavigationWarning(dirty, committedRef, isProjectUpdate ? '当前项目画像更新尚未完成，离开后将丢失，确定离开吗？' : '当前创作项目创建尚未完成，离开后将丢失，确定离开吗？')

  const closeAssistantSession = useCallback((id: string) => assistantSession ? api.assistant.close(id) : api.interactive.close(id), [assistantSession])
  const sendAssistantMessage = useCallback((id: string, text: string) => assistantSession ? api.assistant.message(id, text) : api.interactive.message(id, text), [assistantSession])
  const answerAssistantQuestion = useCallback((id: string, callId: string, answers: DshQuestionAnswer['answers']) => assistantSession
    ? api.assistant.answer(id, callId, answers)
    : api.interactive.answer(id, callId, answers), [assistantSession])

  useEffect(() => {
    if (!assistantSession || assistantModeId !== 'creation' || !projectId || composer.trim()) return
    if (portraitHandoffIdeaRef.current) return
    const idea = readPortraitCreationHandoff(projectId)
    if (!idea) return
    portraitHandoffIdeaRef.current = idea
    setComposer(`这是我刚完成画像初始化后的一次性创作意向。请基于项目画像先与我核对并制定创作计划，不要直接创建作品任务。\n\n本次创作意向：${idea}`)
  }, [assistantModeId, assistantSession, composer, projectId])

  const persistAssistantCheckpoint = useCallback((id: string, status: 'draft' | 'ready' = 'draft', currentDraft = draftRef.current) => {
    if (!assistantSession || !assistantModeId) return Promise.resolve(null)
    const payload = currentDraft ? assistantCheckpointPayload(currentDraft, assistantModeId) : {}
    return api.assistant.checkpoint(id, {
      schema_version: '1.0',
      payload,
      summary: currentDraft ? assistantCheckpointSummary(assistantModeId, currentDraft) : `${assistantModeId === 'profile' ? '画像' : assistantModeId === 'creation' ? '创作' : '复盘'}模式草稿`,
      status,
    }).then((checkpoint) => {
      queryClient.setQueryData(['assistant-checkpoint', assistantModeId, projectId ?? '', contentId ?? ''], {
        id: checkpoint.id, summary: checkpoint.summary, status: checkpoint.status, updatedAt: new Date().toISOString(),
      })
      return checkpoint
    })
  }, [assistantModeId, assistantSession, contentId, projectId, queryClient])

  const closeSessionWithCheckpoint = useCallback(async (id: string) => {
    if (committedRef.current) return
    if (assistantSession) await persistAssistantCheckpoint(id, draftRef.current?.ready ? 'ready' : 'draft')
    await closeAssistantSession(id)
  }, [assistantSession, closeAssistantSession, persistAssistantCheckpoint])

  const resetConversation = useCallback((closePrevious = true) => {
    runGenerationRef.current += 1
    streamAbortRef.current?.abort()
    const previousId = sessionRef.current
    if (startPromiseRef.current) abandonedStartKeysRef.current.add(createIdempotencyKeyRef.current)
    if (previousId && !committedRef.current && closePrevious) void closeSessionWithCheckpoint(previousId).catch(() => undefined)
    clearPersistedInteractiveSession(sessionStorageKey)
    sessionRef.current = null
    setSessionId(null)
    setRunState('idle')
    setMessages([])
    setQuestionBatches([])
    setTimeline([])
    setDraft(null)
    setDraftNeedsReconfirmation(false)
    setSessionProject(null)
    confirmationTokenRef.current = null
    setComposer('')
    setStreamError(null)
    setCommitError(null)
    setCommitted(false)
    setCreatedTaskId(null)
    committedRef.current = false
    hasInputRef.current = false
    setCommitting(false)
    sendingMessageRef.current = false
    setSendingMessage(false)
    setTurnActive(false)
    currentTurnRef.current = undefined
    failedToolTurnsRef.current.clear()
    unscopedToolFailureRef.current = false
    setRecoveryAvailable(false)
    recoveryPromptRef.current = null
    recoveryMessagePromiseRef.current = null
    recoveryDraftRef.current = null
    legacyMessageCandidatesRef.current.clear()
    messageAliasesRef.current.clear()
    setLastEventId(undefined)
    setDraftReadyEventId(undefined)
    idempotencyKeyRef.current = crypto.randomUUID()
    createIdempotencyKeyRef.current = crypto.randomUUID()
  }, [closeSessionWithCheckpoint, sessionStorageKey, assistantSession])

  const preserveLocalMaterialAfterLostSession = useCallback((message: string, activityStatus: 'stopped' | 'error' = 'stopped', terminalState: TerminalRunState = 'error') => {
    runGenerationRef.current += 1
    streamAbortRef.current?.abort()
    startPromiseRef.current = null
    sessionRef.current = null
    setSessionId(null)
    setRunState(terminalState)
    setTurnActive(false)
    setRecoveryAvailable(true)
    const savedAtFailure = readPersistedInteractiveSession(sessionStorageKey)
    const persisted = savedAtFailure
    setDraftNeedsReconfirmation(Boolean(savedAtFailure?.draft?.ready || savedAtFailure?.draftNeedsReconfirmation || draftRef.current?.ready))
    const savedTimeline = (persisted?.timeline ?? timelineRef.current).map((item) => item.kind === 'activity' && (item.status === 'running' || item.status === 'started')
      ? { ...item, status: activityStatus }
      : item)
    timelineRef.current = savedTimeline
    setTimeline(savedTimeline)
    setStreamError(message)
    confirmationTokenRef.current = null
    const savedMessages = messagesRef.current
    const savedQuestionBatches = questionBatchesRef.current
    const savedDraft = draftRef.current
    const savedDraftReadyEventId = draftReadyEventIdRef.current
    const savedLastEventId = lastEventIdRef.current
    replacePersistedInteractiveSession(sessionStorageKey, {
      ...persisted,
      sessionId: null,
      createIdempotencyKey: persisted?.createIdempotencyKey ?? createIdempotencyKeyRef.current,
      messages: savedMessages,
      questionBatches: savedQuestionBatches,
      timeline: savedTimeline,
      draft: savedDraft,
      draftReadyEventId: savedDraftReadyEventId,
      lastEventId: savedLastEventId,
      turnActive: false,
      terminalState,
      terminalMessage: message,
      draftNeedsReconfirmation: Boolean(savedDraft?.ready || persisted?.draftNeedsReconfirmation),
    })
  }, [sessionStorageKey])

  const terminateConversation = useCallback((state: TerminalRunState, message: string) => {
    preserveLocalMaterialAfterLostSession(message, state === 'error' ? 'error' : 'stopped', state)
    if (assistantSession) void queryClient.invalidateQueries({ queryKey: ['assistant-checkpoint'] })
  }, [assistantSession, preserveLocalMaterialAfterLostSession, queryClient])

  const processEvent = useCallback((event: { event: string; data: string; id?: string }) => {
    const data = parseData(event.data)
    const eventId = eventIdValue(event.id)
    if (event.id) {
      lastEventIdRef.current = event.id
      setLastEventId(event.id)
    }
    const streamedMessage = (role: Message['role']): Message => {
      const rawText = stringValue(data.text) ?? ''
      const text = role === 'user' ? displayInteractiveMessage(rawText) : rawText
      const aliasKey = event.id ? `${role}:${event.id}` : undefined
      const existingId = aliasKey ? messageAliasesRef.current.get(aliasKey) : undefined
      if (existingId) return { id: existingId, role, text }
      const legacyIds = legacyMessageCandidatesRef.current.get(messageFingerprint(role, text))
      const legacyId = legacyIds?.shift()
      const id = legacyId ?? stringValue(data.id) ?? event.id ?? crypto.randomUUID()
      if (aliasKey) messageAliasesRef.current.set(aliasKey, id)
      return { id, role, text }
    }
    switch (event.event) {
      case 'assistant.message':
        {
          const message = streamedMessage('assistant')
          const isNewMessage = !messagesRef.current.some((item) => item.id === message.id)
          if (isNewMessage) notifyIncomingMessage()
          updateMessagesNow((items) => items.some((item) => item.id === message.id) ? items : [...items, message])
          updateTimelineNow((items) => upsertTimeline(items, { kind: 'message', message, eventId }, (item) => item.kind === 'message' && item.message.id === message.id))
        }
        break
      case 'user.message':
        {
          const streamed = streamedMessage('user')
          const pending = pendingSubmissionRef.current
          const message = pending?.kind === 'message' && pending.message.text === streamed.text
            ? pending.message
            : streamed
          if (pending?.kind === 'message' && pending.message.id === message.id) pendingSubmissionRef.current = null
          updateMessagesNow((items) => items.some((item) => item.id === message.id) ? items : [...items, message])
          updateTimelineNow((items) => upsertTimeline(items, { kind: 'message', message, eventId }, (item) => item.kind === 'message' && item.message.id === message.id))
        }
        break
      case 'session.status':
        setRunState(isRunState(data.status) ? data.status : 'running')
        if (data.status === 'idle') {
          setTurnActive(false)
          updateTimelineNow((items) => items.map((item) => item.kind === 'activity' && (item.status === 'running' || item.status === 'started')
            ? { ...item, status: 'completed' }
            : item))
        }
        break
      case 'session.snapshot':
        setRunState(isRunState(data.status) ? data.status : 'running')
        setTurnActive(data.busy === true)
        {
          if (isRecord(data.draft)) {
            const snapshotDraft = data.draft
            const normalizedDraft = normalizeDraft({ draft: snapshotDraft })
            if (recoveryDraftRef.current && isEmptyDraft(normalizedDraft)) {
              // A newly created session emits an empty snapshot before the
              // recovery prompt has produced its first draft. Keep the saved
              // local material until a meaningful replacement arrives.
            } else {
              recoveryDraftRef.current = null
              confirmationTokenRef.current = stringValue(snapshotDraft.confirmation_token) ?? null
              if (normalizedDraft.ready && confirmationTokenRef.current) {
                setRecoveryAvailable(false)
                setDraftNeedsReconfirmation(false)
              }
              draftRef.current = normalizedDraft
              setDraft(normalizedDraft)
              if (normalizedDraft.ready) {
                const readyEventId = eventIdValue(data.draft_event_id)
                const readyEventIdValue = readyEventId === undefined ? undefined : String(readyEventId)
                draftReadyEventIdRef.current = readyEventIdValue
                setDraftReadyEventId(readyEventIdValue)
                updateTimelineNow((items) => upsertTimeline(items, {
                  kind: 'draft',
                  id: 'draft',
                  eventId: readyEventId ?? eventId,
                }, (item) => item.kind === 'draft'))
              } else {
                draftReadyEventIdRef.current = undefined
                setDraftReadyEventId(undefined)
                updateTimelineNow((items) => items.filter((item) => item.kind !== 'draft'))
              }
            }
          }
          const callId = stringValue(data.pending_call_id) ?? ''
          const questions = Array.isArray(data.questions) ? data.questions.flatMap((value) => {
            const question = parseQuestion(value)
            return question ? [question] : []
          }) : []
          updateQuestionsNow((batches) => {
            const answered = batches.filter((batch) => Boolean(batch.answers))
            return callId && questions.length > 0
              ? [...answered, { callId, questions, eventId: batches.find((batch) => batch.callId === callId)?.eventId ?? eventId }]
              : answered
          })
          if (callId && questions.length > 0) {
            updateTimelineNow((items) => {
              const existing = items.find((item) => item.kind === 'question' && item.callId === callId)
              if (existing?.eventId !== undefined) return items
              return upsertTimeline(items, { kind: 'question', callId, eventId }, (item) => item.kind === 'question' && item.callId === callId)
            })
          }
          const waiting = callId !== '' && questions.length > 0
          if (data.busy === true || waiting) {
            const status = waiting ? 'waiting' : 'running'
            const text = waiting ? 'Agent 正在等待你的回答' : 'Agent 正在工作…'
            updateTimelineNow((items) => {
              let currentIndex = -1
              items.forEach((item, index) => {
                if (item.kind === 'activity' && (item.status === 'started' || item.status === 'running' || item.status === 'waiting')) currentIndex = index
              })
              if (currentIndex >= 0) {
                return items.map((item, index) => {
                  if (index !== currentIndex || item.kind !== 'activity') return item
                  return {
                    ...item,
                    status,
                    text: status === 'waiting' || item.status === 'started' || item.status === 'waiting' ? text : item.text,
                  }
                })
              }
              return upsertTimeline(items, {
                kind: 'activity',
                id: `snapshot-${status}-${eventId ?? crypto.randomUUID()}`,
                status,
                eventId,
                text,
              }, (item) => item.kind === 'activity' && item.status === status)
            })
          } else {
            updateTimelineNow((items) => items.map((item) => item.kind === 'activity' && (item.status === 'running' || item.status === 'started')
              ? { ...item, status: 'completed' }
              : item))
          }
        }
        break
      case 'turn.start':
        setTurnActive(true)
        currentTurnRef.current = typeof data.turn === 'number' && Number.isSafeInteger(data.turn) ? data.turn : undefined
        updateTimelineNow((items) => upsertTimeline(items, {
          kind: 'activity',
          id: `turn-start-${eventId ?? crypto.randomUUID()}`,
          eventId,
          status: 'started',
          text: 'Agent 本轮开始处理',
        }, (item) => item.kind === 'activity' && item.eventId === eventId && eventId !== undefined))
        break
      case 'turn.end':
        setTurnActive(false)
        {
          const reason = isRecord(data.reason) ? data.reason : {}
          const turn = typeof data.turn === 'number' && Number.isSafeInteger(data.turn) ? data.turn : currentTurnRef.current
          const toolFailed = unscopedToolFailureRef.current || (turn !== undefined && failedToolTurnsRef.current.has(turn))
          if (turn !== undefined) failedToolTurnsRef.current.delete(turn)
          unscopedToolFailureRef.current = false
          if (currentTurnRef.current === turn) currentTurnRef.current = undefined
          const status: Extract<TimelineItem, { kind: 'activity' }>['status'] = toolFailed || reason.kind === 'error' ? 'error' : reason.kind === 'aborted' || reason.kind === 'interrupted' || reason.kind === 'cancelled' ? 'stopped' : 'completed'
          const text = status === 'error' ? 'Agent 本轮发生错误' : status === 'stopped' ? 'Agent 本轮已停止' : 'Agent 本轮已完成'
          const completion: TimelineItem = { kind: 'activity', id: `turn-end-${eventId ?? crypto.randomUUID()}`, eventId, status, text }
          updateTimelineNow((items) => upsertTimeline(
            items.map((item) => item.kind === 'activity' && (item.status === 'running' || item.status === 'started')
              ? { ...item, status: status === 'stopped' && item.id.startsWith('turn-start-') ? 'completed' : status }
              : item),
            completion,
            (item) => item.kind === 'activity' && item.id === completion.id,
          ))
        }
        break
      case 'ready':
      case 'session.ready':
        setRunState('running')
        break
      case 'question.opened':
        setTurnActive(false)
        updateTimelineNow((items) => items.map((item) => item.kind === 'activity' && (item.status === 'running' || item.status === 'started')
          ? { ...item, status: 'waiting', text: 'Agent 正在等待你的回答' }
          : item))
        setRunState('waiting_answer')
        {
          const callId = stringValue(data.call_id) ?? stringValue(data.callId) ?? ''
          const questions = Array.isArray(data.questions) ? data.questions.flatMap((value) => {
            const question = parseQuestion(value)
            return question ? [question] : []
          }) : []
          updateQuestionsNow((batches) => batches.some((batch) => batch.callId === callId)
            ? batches.map((batch) => batch.callId === callId ? { ...batch, eventId: batch.eventId ?? eventId } : batch)
            : [...batches, { callId, questions, eventId }])
          updateTimelineNow((items) => upsertTimeline(items, { kind: 'question', callId, eventId }, (item) => item.kind === 'question' && item.callId === callId))
        }
        break
      case 'agent.activity':
        setTurnActive(true)
        {
          const text = formatAgentActivity(stringValue(data.name))
          updateTimelineNow((items) => {
            const completedPrevious = items.map((item) => {
              if (item.kind !== 'activity') return item
              if (item.status === 'started') return { ...item, status: 'completed' as const }
              return item.status === 'running' ? { ...item, status: 'completed' as const } : item
            })
            return upsertTimeline(completedPrevious, {
              kind: 'activity',
              id: `activity-${eventId ?? crypto.randomUUID()}`,
              callId: stringValue(data.call_id),
              eventId,
              status: 'running',
              text,
            }, (item) => item.kind === 'activity' && item.eventId === eventId && eventId !== undefined)
          })
        }
        break
      case 'tool.result':
        {
          const callId = stringValue(data.call_id)
          const ok = data.ok === true
          const lostSession = !ok && isLostInteractiveSession(data)
          if (!ok) {
            const turn = typeof data.turn === 'number' && Number.isSafeInteger(data.turn) ? data.turn : currentTurnRef.current
            if (turn === undefined) unscopedToolFailureRef.current = true
            else failedToolTurnsRef.current.add(turn)
          }
          updateTimelineNow((items) => items.map((item) => {
            if (!callId || item.kind !== 'activity' || item.callId !== callId) return item
            return {
              ...item,
              status: ok ? 'completed' : 'error',
              text: ok && item.text.includes('stage project creator draft')
                ? '画像草稿已暂存，等待确认'
                : !ok && lostSession
                  ? '画像草稿暂存失败'
                  : !ok
                    ? 'Agent 工具执行失败'
                    : item.text,
            }
          }))
          if (lostSession) {
            preserveLocalMaterialAfterLostSession('创作会话已失效，项目尚未创建', 'error')
          }
        }
        break
      case 'question.answered':
        updateQuestionsNow((batches) => batches.map((batch) => batch.callId === (stringValue(data.call_id) ?? stringValue(data.callId) ?? '')
          ? { ...batch, answers: parseAnswers(data.answers ?? data.answer) }
          : batch))
        updateTimelineNow((items) => items.map((item) => item.kind === 'activity' && item.status === 'waiting'
          ? { ...item, status: 'completed' }
          : item))
        setRunState('running')
        break
      case 'draft.updated':
      case 'draft.ready':
        recoveryDraftRef.current = null
        {
          const payload = isRecord(data.draft) ? data.draft : data
          const token = stringValue(payload.confirmation_token)
          const notReady = payload.ready === false || data.ready === false
          if (token) {
            confirmationTokenRef.current = token
            if (event.event === 'draft.ready' && !notReady) {
              setRecoveryAvailable(false)
              setDraftNeedsReconfirmation(false)
            }
          } else if (notReady) {
            confirmationTokenRef.current = null
          }
        }
        {
          const nextDraft = normalizeDraft(data)
          const next = event.event === 'draft.ready'
            ? { ...nextDraft, ready: data.ready !== false }
            : { ...(draftRef.current ?? {}), ...nextDraft }
          draftRef.current = next
          setDraft(next)
        }
        if (event.event === 'draft.ready') {
          setTurnActive(false)
          setDraftReadyEventId(event.id)
          draftReadyEventIdRef.current = event.id
          updateTimelineNow((items) => upsertTimeline(items, { kind: 'draft', id: 'draft', eventId }, (item) => item.kind === 'draft'))
        }
        break
      case 'session.error':
        {
          const sessionId = sessionRef.current
          preserveLocalMaterialAfterLostSession(stringValue(data.message) ?? '创作会话发生错误', 'error')
          if (sessionId && !committedRef.current) void closeSessionWithCheckpoint(sessionId).catch(() => undefined)
        }
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
  }, [closeSessionWithCheckpoint, isProjectUpdate, notifyIncomingMessage, preserveLocalMaterialAfterLostSession, terminateConversation, updateMessagesNow, updateQuestionsNow, updateTimelineNow])

  const sendRecoveryPrompt = useCallback(async (id: string, prompt: string) => {
    if (recoveryMessagePromiseRef.current) return recoveryMessagePromiseRef.current
    const promise = (async () => {
      for (let attempt = 0; attempt < 5; attempt += 1) {
        try {
          await sendAssistantMessage(id, prompt)
          recoveryPromptRef.current = null
          return
        } catch (error) {
          const message = error instanceof Error ? error.message : ''
          const retryable = /409|410|closed|runtime|starting|busy|繁忙|处理中/.test(message)
          if (!retryable || attempt === 4) {
            setStreamError('已恢复本地材料，但继续画像失败，请重新连接后重试。')
            setRunState('error')
            return
          }
          await new Promise((resolve) => window.setTimeout(resolve, 300 * (attempt + 1)))
        }
      }
    })()
    recoveryMessagePromiseRef.current = promise
    try {
      await promise
    } finally {
      recoveryMessagePromiseRef.current = null
    }
    return promise
  }, [])

  const connectStream = useCallback(async (id: string, generation: number) => {
    streamAbortRef.current?.abort()
    const controller = new AbortController()
    streamAbortRef.current = controller
    try {
      if (!token) throw new Error('登录状态已失效，请重新登录。')
      const persisted = readPersistedInteractiveSession(sessionStorageKey)
      for await (const event of streamInteractiveSession(id, token, controller.signal, {
        lastEventId: persisted?.sessionId === id ? resumeEventId(persisted) : undefined,
        ...(assistantSession ? { endpoint: '/assistant' as const } : {}),
        onConnected: () => {
          if (disposedRef.current || generation !== runGenerationRef.current) return
          setStreamError(null)
          const prompt = recoveryPromptRef.current
      if (prompt && sessionRef.current === id) void sendRecoveryPrompt(id, prompt)
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
      const message = error instanceof Error ? error.message : '与创作会话连接失败'
      if (/^SSE connection failed after \d+ attempts$/.test(message)) {
        setStreamError('会话连接多次失败。当前会话已保留，请重新连接。')
        setRunState('error')
        return
      }
      const terminal = /^SSE connection failed: (401|404|410)$/.test(message) || message === '会话历史已失效，请重新开始创建'
      if (terminal) {
        if (/^SSE connection failed: (404|410)$/.test(message) || message === '会话历史已失效，请重新开始创建') {
          preserveLocalMaterialAfterLostSession(assistantSession ? '原运行时已失效，正在检查服务端保存的结构化草稿' : '原会话已失效，本地材料仍已保留')
        } else {
          setStreamError(message)
          setRunState('error')
        }
      } else {
        // A transient browser, proxy, or upstream failure must not close the
        // server-owned session. Keeping its ID persisted lets the next mount
        // resume the same Job instead of creating a second runtime.
        setStreamError(message)
        setRunState('error')
      }
    }
  }, [assistantSession, isProjectUpdate, preserveLocalMaterialAfterLostSession, processEvent, sendRecoveryPrompt, sessionStorageKey, terminateConversation, token])

  const reconnectSession = useCallback(() => {
    const id = sessionRef.current
    if (!id) return
    const generation = ++runGenerationRef.current
    disposedRef.current = false
    setStreamError(null)
    setRunState('starting')
    void connectStream(id, generation)
  }, [connectStream])

  const recoverSavedDraft = useCallback(() => {
    if (assistantSession) {
      if (!assistantRecoveryCheckpoint) return
      setCheckpointRecoveryPending(true)
      resetConversation(false)
      setRecoveryAvailable(true)
      startPromiseRef.current = null
      const nextParams = new URLSearchParams(location.search)
      nextParams.set('checkpoint_id', assistantRecoveryCheckpoint.id)
      navigate({ pathname: location.pathname, search: `?${nextParams.toString()}` }, { replace: true })
      return
    }
    const saved = readPersistedInteractiveSession(sessionStorageKey)
    if (!saved || !hasSavedMaterial(saved)) return
    recoveryPromptRef.current = recoveryPrompt(saved)
    recoveryMessagePromiseRef.current = null
    recoveryDraftRef.current = saved.draft?.ready ? saved.draft : null
    createIdempotencyKeyRef.current = crypto.randomUUID()
    idempotencyKeyRef.current = crypto.randomUUID()
    replacePersistedInteractiveSession(sessionStorageKey, {
      ...saved,
      sessionId: null,
      createIdempotencyKey: createIdempotencyKeyRef.current,
      terminalState: undefined,
      terminalMessage: undefined,
      draftNeedsReconfirmation: Boolean(saved.draft?.ready),
      turnActive: false,
    })
    setRecoveryAvailable(false)
    setDraftNeedsReconfirmation(Boolean(saved.draft?.ready))
    setStreamError(null)
    setRunState('idle')
    setRestartNonce((value) => value + 1)
  }, [assistantRecoveryCheckpoint, assistantSession, location.pathname, location.search, navigate, resetConversation, sessionStorageKey])

  const startSession = useCallback((agent: InteractiveAgent): Promise<string | null> => {
    const key = `${agent.id}:${intent}:${projectId ?? ''}:${restartNonce}`
    if (startPromiseRef.current?.key === key) return startPromiseRef.current.promise
    const createRequestKey = createIdempotencyKeyRef.current
    const generation = ++runGenerationRef.current
    disposedRef.current = false
    setRunState('starting')
    setStreamError(null)
    persistInteractiveSession(sessionStorageKey, { createIdempotencyKey: createRequestKey })
    const promise = (projectId
      ? assistantSession
        ? api.assistant.createSession({ mode_id: agent.id as AssistantModeID, project_id: projectId, ...(contentId ? { content_id: contentId } : {}), ...(checkpointId ? { checkpoint_id: checkpointId } : {}) }, createRequestKey)
        : api.interactive.createSession(agent.id, intent, projectId, createRequestKey)
      : assistantSession
        ? api.assistant.createSession({ mode_id: agent.id as AssistantModeID, ...(checkpointId ? { checkpoint_id: checkpointId } : {}) }, createRequestKey)
        : api.interactive.createSession(agent.id, intent, undefined, createRequestKey)
    ).then(async (session) => {
      if (disposedRef.current || generation !== runGenerationRef.current) {
        if (abandonedStartKeysRef.current.has(createRequestKey) || createIdempotencyKeyRef.current !== createRequestKey) {
          abandonedStartKeysRef.current.delete(createRequestKey)
          await closeSessionWithCheckpoint(session.id).catch(() => undefined)
        } else {
          persistInteractiveSession(sessionStorageKey, { sessionId: session.id })
        }
        return null
      }
      sessionRef.current = session.id
      persistInteractiveSession(sessionStorageKey, { sessionId: session.id })
      setSessionProject({ id: session.project_id ?? projectId, name: session.project_name })
      setSessionId(session.id)
      setRunState((session.status as RunState) || 'running')
      return session.id
    }).catch((error) => {
      if (disposedRef.current || generation !== runGenerationRef.current) return null
      setStreamError(error instanceof Error ? error.message : `无法启动创作${isProjectUpdate ? '更新' : '创建'}会话`)
      setRunState('error')
      return null
    })
    startPromiseRef.current = { key, promise }
    return promise
  }, [assistantSession, checkpointId, closeSessionWithCheckpoint, connectStream, contentId, intent, isProjectUpdate, projectId, restartNonce, sessionStorageKey])

  useEffect(() => {
    if (sessionStorageKey !== persistedSessionKey || !persistedSession?.sessionId || persistedSession.terminalState || !token) return
    const currentPersisted = readPersistedInteractiveSession(sessionStorageKey)
    if (currentPersisted?.sessionId !== persistedSession.sessionId) return
    const generation = ++runGenerationRef.current
    disposedRef.current = false
    setRunState('starting')
    setTurnActive(persistedSession.turnActive === true)
    sessionRef.current = persistedSession.sessionId
    void connectStream(persistedSession.sessionId, generation)
  }, [connectStream, persistedSession, persistedSessionKey, sessionStorageKey, token])

  useEffect(() => {
    if (!assistantSession || !checkpointId || !activeAgent || sessionId || recoveryAvailable || ['starting', 'error', 'expired', 'closed'].includes(runState)) return
    void startSession(activeAgent).then((id) => {
      if (id) void connectStream(id, runGenerationRef.current)
    })
  }, [activeAgent, assistantSession, checkpointId, connectStream, recoveryAvailable, runState, sessionId, startSession])

  useEffect(() => {
    if (!checkpointRecoveryPending || !checkpointId) return
    setCheckpointRecoveryPending(false)
    setRecoveryAvailable(false)
  }, [checkpointId, checkpointRecoveryPending])

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

  useEffect(() => {
    if (!sessionId && !isTerminalRunState(runState)) return
    const persisted = readPersistedInteractiveSession(sessionStorageKey)
    const persistedSessionId = sessionId ?? persisted?.sessionId
    if (!persistedSessionId) return
    if (sessionId && persisted?.sessionId !== sessionId) return
    persistInteractiveSession(sessionStorageKey, {
      ...persisted,
      sessionId: persistedSessionId,
      lastEventId,
      turnActive,
      terminalState: isTerminalRunState(runState) && !sessionId ? runState : undefined,
      terminalMessage: isTerminalRunState(runState) && !sessionId ? streamError ?? undefined : undefined,
      draftReadyEventId,
      messages,
      questionBatches,
      timeline,
      draft,
      draftNeedsReconfirmation,
    })
  }, [draft, draftNeedsReconfirmation, draftReadyEventId, lastEventId, messages, persistedSession, questionBatches, runState, sessionId, sessionStorageKey, streamError, timeline, turnActive])

  const selectAgent = async (agent: InteractiveAgent) => {
    if (committing || cancelling || agent.id === selectedAgent) return
    if (assistantSession) {
      if (!agent.available) {
        setModeSwitchError(bootstrapQuery.data?.modes.find((mode) => mode.id === agent.id)?.disabledReason ?? '该模式当前不可用。')
        return
      }
      setModeSwitchError(null)
      if (sessionId) {
        try {
          await closeSessionWithCheckpoint(sessionId)
        } catch (error) {
          setModeSwitchError(error instanceof Error ? error.message : '保存当前草稿失败，请重试。')
          return
        }
      }
        void queryClient.invalidateQueries({ queryKey: ['assistant-bootstrap'], refetchType: 'active' })
        void queryClient.invalidateQueries({ queryKey: ['assistant-checkpoint'] })
      resetConversation(false)
        setModeMenuOpen(false)
      startPromiseRef.current = null
        const nextParams = new URLSearchParams({ mode: agent.id })
      const mode = agent.id as AssistantModeID
      const nextProjectId = projectId ?? bootstrapQuery.data?.projects[0]?.id
      if (nextProjectId && (mode !== 'profile' || projectId)) nextParams.set('project_id', nextProjectId)
      if (mode === 'review' && nextProjectId) {
        const nextProject = bootstrapQuery.data?.projects.find((project) => project.id === nextProjectId)
        const nextContentId = contentId ?? nextProject?.works[0]?.id
        if (nextContentId) nextParams.set('content_id', nextContentId)
      }
      committedRef.current = true
      navigate({ pathname: '/chat', search: `?${nextParams.toString()}` })
      return
    }
    if ((hasInputRef.current || draft) && !window.confirm(`切换 Agent 会丢弃当前${isProjectUpdate ? '更新' : '创建'}草稿，确定继续吗？`)) return
    resetConversation()
    startPromiseRef.current = null
    setSelectedAgent(agent.id)
    setModeMenuOpen(false)
    const nextParams = new URLSearchParams(params)
    nextParams.set('agent', agent.id)
    if (!nextParams.has('intent')) nextParams.set('intent', intent)
    navigate({ pathname: location.pathname, search: `?${nextParams.toString()}` })
  }

  useEffect(() => {
    const loading = assistantSession ? bootstrapQuery.isLoading : agentsQuery.isLoading
    if (!loading && selectedAgent && !activeAgent) {
      setSelectedAgent('')
    }
  }, [activeAgent, agentsQuery.isLoading, assistantSession, bootstrapQuery.isLoading, selectedAgent])

  const sendMessage = async (messageOverride?: string) => {
    const text = (messageOverride ?? composer).trim()
    if (!text || pendingQuestions.length > 0 || draftRef.current?.ready || runState === 'starting' || committing || turnActive || sendingMessageRef.current) return
    sendingMessageRef.current = true
    setSendingMessage(true)
    let activeSessionId = sessionRef.current
    let pendingSubmission: PendingSubmission | null = null
    try {
      if (!activeSessionId) {
        const agentForStart = activeAgent ?? (selectedAgent ? {
          id: selectedAgent,
          display_name: selectedAgent,
          description: '',
          available: true,
        } : undefined)
        if (!agentForStart || recoveryAvailable || runState !== 'idle') return
        activeSessionId = await startSession(agentForStart)
        if (!activeSessionId) return
      }
      setComposer('')
      hasInputRef.current = true
      pendingSubmission = {
        kind: 'message',
        sessionId: activeSessionId,
        message: { id: crypto.randomUUID(), role: 'user', text },
        status: 'pending',
      }
      pendingSubmissionRef.current = pendingSubmission
      recoveryDraftRef.current = null
      confirmationTokenRef.current = null
      setDraft((value) => value ? { ...value, ready: false } : value)
      setCommitError(null)
      setTurnActive(true)
      const persisted = readPersistedInteractiveSession(sessionStorageKey)
      persistInteractiveSession(sessionStorageKey, { sessionId: activeSessionId, lastEventId: persisted?.lastEventId, turnActive: true })
      await sendAssistantMessage(activeSessionId, text)
      void connectStream(activeSessionId, runGenerationRef.current)
      if (sessionRef.current !== activeSessionId) return
      if (portraitHandoffIdeaRef.current && projectId && text.includes(portraitHandoffIdeaRef.current)) {
        clearPortraitCreationHandoff(projectId)
        portraitHandoffIdeaRef.current = null
      }
      if (assistantSession) {
        clearAssistantMessageHandoff(assistantMessageHandoffKey(user?.id ?? 'signed-out', assistantModeId ?? 'profile', projectId, contentId))
      }
      if (pendingSubmissionRef.current === pendingSubmission && pendingSubmission) pendingSubmission.status = 'accepted'
    } catch (error) {
      if (!activeSessionId || sessionRef.current !== activeSessionId) return
      if (pendingSubmissionRef.current === pendingSubmission && pendingSubmission?.status === 'pending') pendingSubmission.status = 'failed'
      setTurnActive(false)
      const failed = readPersistedInteractiveSession(sessionStorageKey)
      persistInteractiveSession(sessionStorageKey, { sessionId: activeSessionId, lastEventId: failed?.lastEventId, turnActive: false })
      setComposer(text)
      setRunState('running')
      setStreamError(error instanceof Error ? error.message : '消息发送失败')
    } finally {
      if (!pendingSubmission || pendingSubmissionRef.current === pendingSubmission) {
        if (pendingSubmission?.status === 'failed') pendingSubmissionRef.current = null
        sendingMessageRef.current = false
        setSendingMessage(false)
      }
    }
  }

  useEffect(() => {
    if (!assistantSession || initialAssistantMessageSentRef.current || pendingQuestions.length > 0 || composer.trim()) return
    const key = assistantMessageHandoffKey(user?.id ?? 'signed-out', assistantModeId ?? 'profile', projectId, contentId)
    const initialMessage = readAssistantMessageHandoff(key)
    if (!initialMessage) return
    initialAssistantMessageSentRef.current = true
    setComposer(initialMessage)
    if (params.get('handoff') === '1') void sendMessage(initialMessage)
  }, [assistantModeId, assistantSession, composer, contentId, params, pendingQuestions.length, projectId, sendMessage, user?.id])

  const submitAnswers = async (batch: QuestionBatch, answer: DshQuestionAnswer) => {
    if (batch.answers) return
    if (!sessionId || runState === 'expired' || runState === 'closed' || runState === 'error') {
      throw new Error('会话已结束，请重新开始后再回答。')
    }
    hasInputRef.current = true
    const pendingSubmission: PendingSubmission = { kind: 'answer', sessionId, callId: batch.callId, answers: answer.answers, status: 'pending' }
    pendingSubmissionRef.current = pendingSubmission
    recoveryDraftRef.current = null
    setTurnActive(true)
    const persisted = readPersistedInteractiveSession(sessionStorageKey)
    persistInteractiveSession(sessionStorageKey, { sessionId, lastEventId: persisted?.lastEventId, turnActive: true })
    try {
      await answerAssistantQuestion(sessionId, batch.callId, answer.answers)
      if (sessionRef.current !== sessionId) return
      if (pendingSubmissionRef.current === pendingSubmission) pendingSubmission.status = 'accepted'
    } catch (error) {
      if (sessionRef.current !== sessionId) return
      if (pendingSubmissionRef.current === pendingSubmission && pendingSubmission.status === 'pending') pendingSubmission.status = 'failed'
      setTurnActive(false)
      const failed = readPersistedInteractiveSession(sessionStorageKey)
      persistInteractiveSession(sessionStorageKey, { sessionId, lastEventId: failed?.lastEventId, turnActive: false })
      throw error
    }
    if (sessionRef.current !== sessionId) return
    updateQuestionsNow((batches) => batches.map((item) => item.callId === batch.callId
      ? { ...item, answers: answer.answers }
      : item))
    if (pendingSubmissionRef.current === pendingSubmission) pendingSubmissionRef.current = null
  }

  const commitProject = async () => {
    const confirmationToken = confirmationTokenRef.current
    if (!sessionId || !draft?.ready || (assistantModeId === 'profile' && !confirmationToken) || committedRef.current || committing || turnActive || sendingMessage || pendingQuestions.length > 0) return
    setCommitting(true)
    setCommitError(null)
    try {
      const checkpoint = assistantSession && assistantModeId !== 'profile'
        ? await persistAssistantCheckpoint(sessionId, 'ready', draft)
        : null
      if (assistantSession && assistantModeId && assistantModeId !== 'profile') {
        if (!checkpoint?.id || !checkpoint.payload_digest) throw new Error('草稿确认信息未就绪，请重新保存后再确认。')
        if (!draftRef.current?.ready || JSON.stringify(assistantCheckpointPayload(draftRef.current, assistantModeId)) !== JSON.stringify(assistantCheckpointPayload(draft, assistantModeId))) {
          throw new Error('草稿已更新，请阅读最新内容后重新确认。')
        }
      }
      const result = assistantSession
        ? await api.assistant.commit(sessionId, assistantModeId === 'profile'
          ? { confirmation_token: confirmationToken ?? '', idempotency_key: idempotencyKeyRef.current }
          : { checkpoint_id: checkpoint!.id, checkpoint_digest: checkpoint!.payload_digest, idempotency_key: idempotencyKeyRef.current })
        : await api.interactive.commit(sessionId, confirmationToken ?? '', idempotencyKeyRef.current, draft?.memory_append)
      committedRef.current = true
      setCommitted(true)
      if ('task' in result && isRecord(result.task)) setCreatedTaskId(stringValue(result.task.id) ?? null)
      clearPersistedInteractiveSession(sessionStorageKey)
      const id = result.project?.id ?? (isProjectUpdate ? projectId : undefined)
      if (assistantSession) await queryClient.invalidateQueries({ queryKey: ['assistant-bootstrap'] })
      if (!assistantSession || assistantModeId === 'profile') {
        if (assistantSession && assistantModeId === 'profile' && id && !isProjectUpdate) {
          navigate(`/chat?mode=creation&project_id=${encodeURIComponent(id)}`)
        } else if (id && params.get('return_to') === 'portraits') {
          navigate(`/portraits/${encodeURIComponent(id)}`)
        } else {
          navigate(id ? isProjectUpdate ? `/projects?edit=${encodeURIComponent(id)}` : `/projects?onboarding=${encodeURIComponent(id)}` : '/projects')
        }
      }
    } catch (error) {
      setCommitError(error instanceof Error ? error.message : `创作项目${isProjectUpdate ? '更新' : '创建'}失败，请重试。`)
    } finally {
      setCommitting(false)
    }
  }

  const cancelConversation = async () => {
    if (cancelling || committing) return
    const previousId = sessionRef.current
    const pendingCreateKey = !previousId && startPromiseRef.current
      ? createIdempotencyKeyRef.current
      : undefined
    if (!previousId && !pendingCreateKey) return

    setCancelling(true)
    setCancelError(null)
    if (pendingCreateKey) abandonedStartKeysRef.current.add(pendingCreateKey)
    runGenerationRef.current += 1
    streamAbortRef.current?.abort()
    const message = '对话已取消。已保存的消息、问答和草稿仍保留。'
    if (previousId) {
      try {
        await closeSessionWithCheckpoint(previousId)
      } catch (error) {
        setCancelError(error instanceof Error ? error.message : '取消对话失败，请重试。')
        setCancelling(false)
        reconnectSession()
        return
      }
    }

    startPromiseRef.current = null
    sessionRef.current = null
    setSessionId(null)
    setRunState('closed')
    setTurnActive(false)
    sendingMessageRef.current = false
    setSendingMessage(false)
    updateTimelineNow((items) => items.map((item) => item.kind === 'activity' && (item.status === 'running' || item.status === 'started' || item.status === 'waiting')
      ? { ...item, status: 'stopped', ...(item.status === 'waiting' ? { text: '等待回答前已取消' } : {}) }
      : item))
    setStreamError(message)
    setRecoveryAvailable(true)
    confirmationTokenRef.current = null

    const persisted = readPersistedInteractiveSession(sessionStorageKey)
    const savedDraft = draftRef.current
    const pendingSubmission = pendingSubmissionRef.current
    const savedMessages = pendingSubmission?.kind === 'message' && pendingSubmission.sessionId === previousId && pendingSubmission.status !== 'failed'
      ? [...messagesRef.current.filter((item) => item.id !== pendingSubmission.message.id), pendingSubmission.message]
      : messagesRef.current
    const savedQuestions = pendingSubmission?.kind === 'answer' && pendingSubmission.sessionId === previousId && pendingSubmission.status !== 'failed'
      ? questionBatchesRef.current.map((batch) => batch.callId === pendingSubmission.callId ? { ...batch, answers: pendingSubmission.answers } : batch)
      : questionBatchesRef.current
    if (savedMessages !== messagesRef.current) updateMessagesNow(() => savedMessages)
    if (savedQuestions !== questionBatchesRef.current) updateQuestionsNow(() => savedQuestions)
    if (pendingSubmission?.kind === 'message' && pendingSubmission.sessionId === previousId && pendingSubmission.status !== 'failed') {
      updateTimelineNow((items) => upsertTimeline(items, { kind: 'message', message: pendingSubmission.message }, (item) => item.kind === 'message' && item.message.id === pendingSubmission.message.id))
    }
    replacePersistedInteractiveSession(sessionStorageKey, {
      ...persisted,
      sessionId: null,
      turnActive: false,
      terminalState: 'closed',
      terminalMessage: message,
      messages: savedMessages,
      questionBatches: savedQuestions,
      timeline: timelineRef.current,
      draft: savedDraft,
      draftNeedsReconfirmation: Boolean(savedDraft?.ready || persisted?.draftNeedsReconfirmation),
    })
    pendingSubmissionRef.current = null
    setCancelDialogOpen(false)
    setCancelling(false)
  }

  const awaitingConfirmation = Boolean(draft?.ready && !committed)
  const canCompose = Boolean(selectedAgent && !recoveryAvailable)
    && pendingQuestions.length === 0
    && !awaitingConfirmation
    && (sessionId ? runState !== 'starting' && runState !== 'error' : runState === 'idle')
    && !committing
    && !committed
    && !sendingMessage
    && !turnActive
  const currentActivity = [...timeline].reverse().find((item): item is Extract<TimelineItem, { kind: 'activity' }> => item.kind === 'activity' && (item.status === 'started' || item.status === 'running'))
  const liveStatus = awaitingConfirmation
    ? '请先确认上面的草稿'
    : runState === 'starting'
    ? messages.length === 0 ? '正在准备对话…' : '正在恢复会话…'
    : runState === 'waiting_answer'
      ? 'Agent 正在等待你的回答'
      : turnActive
        ? currentActivity?.text ?? 'Agent 正在工作…'
        : null
  const liveStatusLabel = awaitingConfirmation ? '等待确认' : runState === 'starting' ? '准备中' : runState === 'waiting_answer' ? '等待中' : '进行中'
  const emptyRegistry = assistantSession
    ? !bootstrapQuery.isLoading && (bootstrapQuery.isError || selectableAgents.length === 0)
    : !agentsQuery.isLoading && (agentsQuery.isError || selectableAgents.length === 0)
  const canRecoverSavedDraft = recoveryAvailable && (assistantSession
    ? Boolean(assistantRecoveryCheckpoint)
    : hasSavedMaterial(readPersistedInteractiveSession(sessionStorageKey)))
  const canCancelConversation = !committed && !committing && (Boolean(sessionId) || runState === 'starting')

  return (
    <div data-testid="chat-page" className="mx-auto flex min-h-0 w-full max-w-[1500px] flex-1 flex-col gap-4">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-4">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground"><Sparkles className="size-4" /></div>
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold">AI 助手</p>
            <p className="truncate text-xs text-muted-foreground">{activeAgent?.display_name ?? '选择画像、创作或复盘模式'}</p>
          </div>
        </div>
        <div className="relative flex items-center gap-2">
          <Badge variant={runState === 'error' || runState === 'expired' ? 'destructive' : runState === 'closed' ? 'secondary' : 'outline'}>
            {awaitingConfirmation ? '等待确认' : runState === 'starting' ? '正在启动' : runState === 'waiting_answer' ? '等待回答' : runState === 'running' ? '进行中' : runState === 'error' ? '连接异常' : runState === 'expired' ? '已过期' : runState === 'closed' ? '已取消' : '准备就绪'}
          </Badge>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={soundEnabled ? '关闭新消息声音' : '开启新消息声音'}
            title={soundEnabled ? '关闭新消息声音' : '开启新消息声音'}
            onClick={() => {
              const next = !soundEnabled
              setSoundEnabled(next)
              if (!next) pendingSoundRef.current = false
              try {
                localStorage.setItem(NEW_MESSAGE_SOUND_STORAGE_KEY, next ? 'on' : 'off')
              } catch {
                // Ignore storage policies such as private browsing.
              }
            }}
          >
            {soundEnabled ? <Volume2 /> : <VolumeX />}
          </Button>
          {canCancelConversation && <Button variant="outline" size="sm" disabled={cancelling} onClick={() => { setCancelError(null); setCancelDialogOpen(true) }}><Ban className="size-4" />取消对话</Button>}
          <Button variant="outline" size="sm" disabled={selectableAgents.length === 0 || committing || cancelling} onClick={() => setModeMenuOpen((open) => !open)} aria-expanded={modeMenuOpen}>
            <UserRound className="size-4" />
            {activeAgent?.display_name ?? '选择模式'}
            <ChevronDown className="size-3.5" />
          </Button>
          {modeMenuOpen && (
            <div className="absolute right-0 top-full z-20 mt-2 w-72 rounded-xl border border-border bg-popover p-2 shadow-lg" role="menu">
              {selectableAgents.map((agent) => (
                <button key={agent.id} type="button" role="menuitem" disabled={committing || cancelling} className="flex w-full items-start gap-3 rounded-lg px-3 py-2 text-left hover:bg-muted disabled:pointer-events-none disabled:opacity-50" onClick={() => void selectAgent(agent)}>
                  <Sparkles className="mt-0.5 size-4 shrink-0 text-primary" />
                  <span className="min-w-0"><span className="block text-sm font-medium">{agent.display_name}</span><span className="mt-0.5 block text-xs text-muted-foreground">{agent.available === false ? bootstrapQuery.data?.modes.find((mode) => mode.id === agent.id)?.disabledReason : agent.description}</span></span>
                </button>
              ))}
            </div>
          )}
        </div>
      </header>

      {modeSwitchError && <Alert variant="destructive"><AlertTriangle className="size-4" /><AlertDescription>{modeSwitchError}</AlertDescription></Alert>}

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
          <section className="relative flex min-h-0 flex-col rounded-xl border border-border bg-card">
            <div ref={scrollRegionRef} data-testid="chat-scroll-region" onScroll={handleScroll} className="min-h-0 flex-1 space-y-5 overflow-y-auto p-4 sm:p-6" aria-live="polite">
              {timeline.map((item) => {
                if (item.kind === 'message') {
                  const { message } = item
                  return <div key={`message-${message.id}`} className={`flex gap-3 ${message.role === 'user' ? 'justify-end' : ''}`}><div className={`flex max-w-[min(720px,90%)] gap-3 ${message.role === 'user' ? 'flex-row-reverse' : ''}`}><div className={`mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}>{message.role === 'user' ? <UserRound className="size-3.5" /> : <MessageSquare className="size-3.5" />}</div><div className={`rounded-2xl px-4 py-3 text-sm leading-6 ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted/70'}`}><Streamdown>{message.text}</Streamdown></div></div></div>
                }
                if (item.kind === 'question') {
                  const batch = questionBatches.find((candidate) => candidate.callId === item.callId)
                  if (!batch) return null
                  return <DshQuestionComposer key={`question-${batch.callId}`} questions={batch.questions} answers={batch.answers} submitted={Boolean(batch.answers)} disabled={!sessionId || runState === 'starting' || runState === 'expired' || runState === 'closed' || runState === 'error'} onSubmit={(answer) => submitAnswers(batch, answer)} />
                }
                if (item.kind === 'activity') {
                  const statusLabel = item.status === 'started' ? '已开始' : item.status === 'running' ? '进行中' : item.status === 'waiting' ? '等待回答' : item.status === 'completed' ? '已完成' : item.status === 'stopped' ? '已停止' : '出错'
                  const active = item.status === 'started' || item.status === 'running'
                  const text = active ? item.text.replace(/^Agent /, '') : item.status === 'waiting' ? '等待你的回答' : item.text
                  return <div key={`activity-${item.id}`} className="flex items-center gap-2 rounded-lg border border-border/70 bg-muted/30 px-3 py-2 text-sm text-muted-foreground"><Loader2 className={`size-4 shrink-0 ${active ? 'animate-spin text-primary' : 'hidden'}`} /><span className="min-w-0 flex-1 break-words">{text}</span><span className="shrink-0 text-xs">{statusLabel}</span></div>
                }
                if (!draft?.ready || committed) return null
                return <ConfirmCard key="draft-card" draft={draft} mode={assistantSession ? assistantModeId : 'profile'} isProjectUpdate={isProjectUpdate} needsReconfirmation={draftNeedsReconfirmation} canCommit={!turnActive && !sendingMessage && pendingQuestions.length === 0 && !draftNeedsReconfirmation && (assistantSession && assistantModeId !== 'profile' ? draft.ready === true : Boolean(confirmationTokenRef.current))} committing={committing} error={commitError} onCommit={() => void commitProject()} onContinue={async () => { if (!sessionId) return; confirmationTokenRef.current = null; setDraft((value) => value ? { ...value, ready: false } : value); setTimeline((items) => items.filter((entry) => entry.kind !== 'draft')); await sendAssistantMessage(sessionId, '继续编辑当前草稿').catch(() => undefined) }} />
              })}
              {streamError && <Alert variant={streamError.startsWith('对话已取消') ? 'default' : 'destructive'}>{streamError.startsWith('对话已取消') ? <Ban className="size-4" /> : <AlertTriangle className="size-4" />}<AlertTitle>{streamError.startsWith('对话已取消') ? '对话已取消' : canRecoverSavedDraft ? '原会话已失效' : runState === 'error' || runState === 'expired' || runState === 'closed' ? sessionId ? '连接已中断' : '会话需要重新开始' : '连接异常'}</AlertTitle><AlertDescription className="space-y-3"><p>{streamError}</p>{canRecoverSavedDraft && <div className="space-y-1 text-sm"><p>{assistantSession ? '将从服务端保存的结构化草稿继续。' : '本地保存的消息、问答和草稿仍然保留。'}</p><p>{assistantSession ? '新对话会沿用草稿摘要，逐字聊天不会回放。' : '旧 runtime 上下文不会恢复。'}</p><p>保存草稿需要重新确认</p></div>}<div className="flex flex-wrap items-center gap-3">{canRecoverSavedDraft && <Button size="sm" onClick={recoverSavedDraft}><RotateCcw className="size-4" />{assistantSession ? '恢复已保存草稿' : '恢复本地材料'}</Button>}{runState === 'error' && sessionId && <Button size="sm" variant="outline" onClick={reconnectSession}><RotateCcw className="size-4" />重新连接</Button>}{(runState === 'error' || runState === 'expired' || runState === 'closed') && !sessionId && <Button size="sm" variant="outline" onClick={() => { resetConversation(); startPromiseRef.current = null; setRestartNonce((value) => value + 1) }}><RotateCcw className="size-4" />重新开始{isProjectUpdate ? '更新' : '创建'}</Button>}</div></AlertDescription></Alert>}
              {committed && <Alert><Check className="size-4 text-emerald-600" /><AlertTitle>{assistantSession && assistantModeId === 'review' ? '复盘已确认，待数据验证' : assistantSession && assistantModeId === 'creation' ? '创作计划已确认' : isProjectUpdate ? '创作项目画像已更新' : '创作项目已创建'}</AlertTitle><AlertDescription>{assistantSession && assistantModeId === 'review' ? '这条建议会在后续独立数据分析验证后，才更新六维画像。' : assistantSession && assistantModeId === 'creation' ? '作品任务已创建，可以查看创作进度。' : '正在打开创作项目详情。'}{createdTaskId && <Button className="mt-3" size="sm" onClick={() => navigate(`/tasks/${encodeURIComponent(createdTaskId)}`)}>查看作品任务<ArrowRight className="size-4" /></Button>}</AlertDescription></Alert>}
            </div>
            {newMessageCount > 0 && <Button type="button" variant="secondary" size="sm" className="absolute bottom-20 left-1/2 z-10 -translate-x-1/2 shadow-md" onClick={() => scrollToLatest()}><ChevronDown className="size-4" />有 {newMessageCount} 条新消息</Button>}
            <div className="border-t border-border p-3 sm:p-4">
              {liveStatus && <div role="status" className="mb-3 flex min-h-9 items-center gap-2 border-b border-border/60 pb-3 text-sm text-muted-foreground">{liveStatusLabel === '等待中' ? <MessageSquare className="size-4 shrink-0 text-muted-foreground" /> : <Loader2 className="size-4 shrink-0 animate-spin text-primary" />}<span className="min-w-0 flex-1 break-words">{liveStatus}</span><span className="shrink-0 text-xs">{liveStatusLabel}</span></div>}
              <div className="flex items-end gap-2 rounded-xl border border-input bg-background p-2 focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/20"><Textarea value={composer} onChange={(event) => { const value = event.target.value; if (portraitHandoffIdeaRef.current && !value.includes(portraitHandoffIdeaRef.current) && projectId) { clearPortraitCreationHandoff(projectId); portraitHandoffIdeaRef.current = null }; setComposer(value) }} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void sendMessage() } }} disabled={!canCompose} placeholder={pendingQuestions.length > 0 ? '请先完成上面的回答' : awaitingConfirmation ? '请先确认上面的草稿' : runState === 'error' && sessionId ? '连接已中断，请先重新连接' : canCompose ? '说说你现在想做什么…' : '正在准备会话…'} aria-label="发送消息" className="min-h-10 resize-none border-0 bg-transparent p-1 shadow-none focus-visible:ring-0" /><Button size="icon-md" onClick={() => void sendMessage()} disabled={!canCompose || !composer.trim()} aria-label="发送消息" title="发送消息"><Send className="size-4" /></Button></div>
            </div>
          </section>
          <ProgressRail draft={draft} mode={assistantSession ? assistantModeId : 'profile'} />
        </div>
      )}
      <AlertDialog open={cancelDialogOpen} onOpenChange={(open) => { if (!cancelling) setCancelDialogOpen(open) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>取消当前对话？</AlertDialogTitle>
            <AlertDialogDescription>{assistantSession ? 'Agent 会立即停止。先保存当前结构化草稿，之后可在新对话中继续。' : 'Agent 会立即停止。当前已保存的消息、问答和草稿会保留，你可以恢复到新会话中继续。'}</AlertDialogDescription>
          </AlertDialogHeader>
          {cancelError && <p role="alert" className="text-sm text-destructive">取消失败：{cancelError}</p>}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cancelling}>继续对话</AlertDialogCancel>
            <Button variant="destructive" disabled={cancelling} onClick={() => void cancelConversation()}>
              {cancelling ? <Loader2 className="size-4 animate-spin" /> : <Ban className="size-4" />}
              {cancelling ? '正在取消' : '确认取消对话'}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

export default function ChatPage() {
  const [params] = useSearchParams()
  const { user } = useAuth()
  if (params.get('assistant_session') !== '1') return <AssistantHome />
  const scope = assistantSessionStorageKey(user?.id ?? 'signed-out', params.get('mode') ?? 'profile', params.get('project_id') || undefined, params.get('content_id') || undefined)
  return <ChatConversation key={`${scope}:${params.get('checkpoint_id') ?? ''}`} />
}

function ProgressRail({ draft, mode = 'profile' }: { draft: InteractiveDraft | null; mode?: AssistantModeID }) {
  if (mode !== 'profile') {
    const label = mode === 'creation' ? '创作计划' : '复盘结论'
    return <aside className="order-first rounded-lg border border-border bg-card p-4 lg:order-last"><p className="text-sm font-semibold">{label}</p><p className="mt-1 text-xs text-muted-foreground">{draft?.ready ? '等待确认' : '随对话持续整理'}</p><div className="mt-4"><AssistantDraftSummary draft={draft} mode={mode} compact /></div>{mode === 'review' && <p className="mt-4 border-t border-border pt-3 text-xs text-muted-foreground">确认后进入独立数据验证，不会立即修改画像。</p>}</aside>
  }
  return <aside className="order-first overflow-x-auto rounded-lg border border-border bg-card p-4 lg:order-last lg:overflow-visible"><div className="flex min-w-max gap-5 lg:min-w-0 lg:flex-col lg:gap-4"><div className="min-w-48 lg:min-w-0"><p className="text-sm font-semibold">创作画像</p><p className="mt-1 text-xs text-muted-foreground">六个维度逐步收集</p></div>{DIMENSIONS.map(([key, label]) => { const status = dimensionStatus(draft?.progress?.[key]); return <div key={key} className="min-w-28 border-l border-border pl-3 lg:min-w-0 lg:border-l-0 lg:border-t lg:pl-0 lg:pt-3"><div className="flex items-center justify-between gap-2"><span className="text-sm">{label}</span><span className={`text-xs ${status.className}`}>{status.label}</span></div><div className="mt-2 h-1 overflow-hidden rounded-full bg-muted"><div className={`h-full transition-all ${status.label === '待开始' ? 'w-0' : status.label === '已确认' ? 'w-full bg-emerald-500' : 'w-2/3 bg-primary'}`} /></div></div> })}</div></aside>
}

function ConfirmCard({ draft, mode = 'profile', isProjectUpdate, needsReconfirmation = false, canCommit, committing, error, onCommit, onContinue }: { draft: InteractiveDraft; mode?: AssistantModeID; isProjectUpdate: boolean; needsReconfirmation?: boolean; canCommit: boolean; committing: boolean; error: string | null; onCommit: () => void; onContinue: () => void }) {
  if (mode !== 'profile') {
    const title = mode === 'creation' ? '确认创作计划' : '确认复盘结论'
    return <Card className="border-primary/25"><CardHeader><CardTitle className="flex items-center gap-2"><Check className="size-4 text-emerald-600" />{title}</CardTitle><CardDescription>{mode === 'creation' ? '确认后会创建正式作品任务，创作计划将作为该任务的固定输入。' : '确认后会生成待验证的画像改进建议，不会立即改写六维画像。'}</CardDescription></CardHeader><CardContent className="space-y-4"><AssistantDraftSummary draft={draft} mode={mode} />{error && <p className="text-sm text-destructive">{error}</p>}<div className="flex flex-wrap gap-2"><Button onClick={onCommit} disabled={committing || !canCommit}>{committing && <Loader2 className="size-4 animate-spin" />}{mode === 'creation' ? '确认并创建作品任务' : '确认复盘建议'}</Button><Button variant="outline" onClick={onContinue} disabled={committing}><SquarePen className="size-4" />继续编辑</Button></div></CardContent></Card>
  }
  const project = draft.project ?? {}
  const dimensions = isRecord(draft.profile?.dimensions) ? draft.profile.dimensions : {}
  return <Card className="border-emerald-500/30 bg-emerald-500/[0.035]"><CardHeader><CardTitle className="flex items-center gap-2"><Check className="size-4 text-emerald-600" />{needsReconfirmation ? '已保存的画像草稿' : isProjectUpdate ? '确认更新创作项目画像' : '确认创作项目画像'}</CardTitle><CardDescription>{needsReconfirmation ? '这是本地保留的旧草稿，仅供参考。旧会话确认凭据不会恢复，请先由 Agent 核对并重新生成草稿。' : isProjectUpdate ? '信息已整理完成，确认后会更新现有项目画像并保存变更。' : '信息已整理完成，确认后会创建创作项目并保存六维画像。'}</CardDescription></CardHeader><CardContent className="space-y-4"><div className="grid gap-2 rounded-lg border border-border bg-background/70 p-3 text-sm sm:grid-cols-2"><div><span className="text-xs text-muted-foreground">创作项目名称</span><p className="font-medium">{String(project.name ?? draft.name ?? '未命名创作项目')}</p></div><div><span className="text-xs text-muted-foreground">平台</span><p className="font-medium">{String(project.platform ?? '待设定')}</p></div><div className="sm:col-span-2"><span className="text-xs text-muted-foreground">定位</span><p className="mt-1 leading-5">{String(project.instructions ?? project.description ?? '已收集')}</p></div></div><div><p className="text-xs font-medium text-muted-foreground">六维画像摘要</p><div className="mt-2 grid gap-2 sm:grid-cols-2">{DIMENSIONS.map(([key, label]) => <div key={key} className="rounded-lg border border-border bg-background/60 p-3"><p className="text-xs text-muted-foreground">{label}</p><p className="mt-1 text-sm leading-5">{summarizeDimension(dimensions[key])}</p></div>)}</div></div>{(draft.missing_fields?.length || draft.inferred_fields?.length) ? <div className="grid gap-3 sm:grid-cols-2"><div><p className="text-xs font-medium text-muted-foreground">待补充字段</p><p className="mt-1 text-sm leading-5">{draft.missing_fields?.length ? draft.missing_fields.join('、') : '暂无'}</p></div><div><p className="text-xs font-medium text-muted-foreground">推断字段</p><p className="mt-1 text-sm leading-5">{draft.inferred_fields?.length ? draft.inferred_fields.join('、') : '暂无'}</p></div></div> : null}{draft.memory_append && <div className="rounded-lg border border-border bg-background/70 p-3"><p className="text-xs font-medium text-muted-foreground">将追加到项目记忆的内容</p><p className="mt-1 whitespace-pre-wrap text-sm leading-5">{draft.memory_append}</p></div>}{needsReconfirmation ? <p className="text-sm text-amber-700 dark:text-amber-400">保存草稿需要重新确认</p> : !canCommit && <p className="text-sm text-muted-foreground">确认凭据尚未就绪，请等待会话继续同步。</p>}{error && <p className="text-sm text-destructive">{error}</p>}<div className="flex flex-wrap gap-2"><Button onClick={onCommit} disabled={committing || !canCommit}>{committing && <Loader2 className="size-4 animate-spin" />}{needsReconfirmation ? '等待重新确认' : `确认${isProjectUpdate ? '更新画像' : '创建创作项目'}`}</Button><Button variant="outline" onClick={onContinue} disabled={committing}><SquarePen className="size-4" />继续编辑</Button></div></CardContent></Card>
}

function summarizeDimension(value: unknown): string {
  if (!isRecord(value)) return '尚未整理'
  const content = isRecord(value.content) ? value.content : value
  const entries = Object.entries(content).filter(([, item]) => typeof item === 'string' || typeof item === 'number')
  if (entries.length === 0) return '尚未整理'
  return entries.slice(0, 3).map(([key, item]) => `${key}: ${String(item)}`).join('；')
}
