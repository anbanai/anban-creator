import { useMemo, useRef, useState, type ChangeEvent, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import {
  AlertTriangle,
  ArrowRight,
  Clock3,
  Cloud,
  FileText,
  ImageIcon,
  Monitor,
  Paperclip,
  Send,
  Settings,
  Plus,
  X,
  type LucideIcon,
} from 'lucide-react'

import { PlatformAvatar } from '@/components/PlatformAvatar'
import QueryErrorState from '@/components/QueryErrorState'
import { Button } from '@/components/common/button'
import { api } from '@/lib/api'
import type { AIEntryAttachment, AIEntryAttachmentType } from '@/lib/api/ai-entry'
import { createTaskHref, hasUsableModelConfig, projectsReturnHref } from '@/lib/command-center'
import { uploadToOSS } from '@/lib/direct-upload'
import { contentTypeLabel } from '@/lib/labels'
import { queryKeys } from '@/lib/query-keys'
import { buildDashboardBlocker } from '@/lib/studio-ux'
import { getLocalExecutorStatus, isDesktop } from '@/lib/tauri'
import { isVideoCreator } from '@/lib/video-platforms'
import type { Project } from '@/types'

const MEDIA_LIMIT = 50 * 1024 * 1024
const DOCUMENT_LIMIT = 25 * 1024 * 1024

const DOCUMENT_EXTENSIONS = new Set(['pdf', 'doc', 'docx', 'ppt', 'pptx', 'xls', 'xlsx', 'csv', 'txt', 'md', 'markdown', 'json'])

interface UploadingFile {
  id: string
  name: string
  progress: number
}

interface EntryError {
  message: string
  actionUrl?: string
}

export default function DashboardPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const desktopMode = isDesktop()

  const [prompt, setPrompt] = useState('')
  const [attachments, setAttachments] = useState<AIEntryAttachment[]>([])
  const [uploading, setUploading] = useState<UploadingFile[]>([])
  const [entryError, setEntryError] = useState<EntryError | null>(null)
  const attachmentsRef = useRef<AIEntryAttachment[]>([])
  const uploadPromisesRef = useRef<Promise<void>[]>([])

  const { data: projects = [], isLoading: projectsLoading, isError: projectsError, refetch: refetchProjects } = useQuery({
    queryKey: ['projects', 'dashboard', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
    staleTime: 60_000,
  })

  const { data: localExecutorStatus } = useQuery({
    queryKey: ['dashboard', 'local-executor-status'],
    queryFn: getLocalExecutorStatus,
    enabled: desktopMode,
    staleTime: 30_000,
  })

  const { data: apiKeysResponse } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: () => api.apiKeys.list(),
    staleTime: 60_000,
  })

  const { data: modelConfig } = useQuery({
    queryKey: queryKeys.modelConfig.all,
    queryFn: () => api.modelConfig.get(),
    staleTime: 60_000,
  })

  const activeProjects = useMemo(() => projects.filter((project) => project.status === 'active'), [projects])
  const selectedProject = activeProjects[0]
  const localExecutionReady = desktopMode && Boolean(localExecutorStatus?.available)
  const dashboardTitle = selectedProject ? `今天用「${selectedProject.name}」创作什么？` : '先创建一个项目，再开始创作。'
  const newTaskHref = selectedProject ? createTaskHref({ type: selectedProject.platform, projectId: selectedProject.id, intent: 'new' }) : createTaskHref()
  const scheduleHref = selectedProject ? planCreateHref(selectedProject) : '/plans?create=true'
  const canCreateSchedule = canCreatePlanForProject(selectedProject)
  const ExecutionIcon = localExecutionReady ? Monitor : Cloud
  const executionLabel = localExecutionReady ? '本地模式' : '云端模式'

  const submitMutation = useMutation({
    mutationFn: (payload: Parameters<typeof api.aiEntry.submit>[0]) => api.aiEntry.submit(payload),
    onSuccess: async (result) => {
      if (result.status === 'created' && result.task?.id) {
        setEntryError(null)
        setPrompt('')
        setAttachments([])
        attachmentsRef.current = []
        await queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
        navigate(`/tasks/${result.task.id}`)
        return
      }
      if (result.status === 'needs_configuration') {
        setEntryError({ message: result.message || '还需要补充配置。', actionUrl: result.action_url })
        return
      }
      setEntryError({ message: result.message || 'AI 入口暂不可用，请稍后重试。' })
    },
    onError: (err) => {
      const message = err instanceof Error ? err.message : '创建任务失败，请重试。'
      setEntryError({ message })
    },
  })

  const hasError = projectsError
  const dashboardBlocker = buildDashboardBlocker({
    projectsLoading,
    projectsError,
    activeProjectCount: activeProjects.length,
    apiKeysReady: apiKeysResponse ? (apiKeysResponse.items || []).length > 0 : null,
    modelConfigReady: modelConfig ? hasUsableModelConfig(modelConfig) : null,
  })
  const canSubmit = Boolean(selectedProject) && !dashboardBlocker?.blocking && !submitMutation.isPending && uploading.length === 0

  async function handleSubmit() {
    const text = prompt.trim()
    if (!selectedProject) {
      setEntryError({ message: '请先创建或选择一个活跃项目。', actionUrl: '/projects' })
      return
    }
    if (!text && attachmentsRef.current.length === 0) {
      setEntryError({ message: '请输入创作需求，或上传参考素材后补一句说明。' })
      return
    }
    setEntryError(null)
    if (uploadPromisesRef.current.length > 0) {
      await Promise.allSettled(uploadPromisesRef.current)
    }
    await submitMutation.mutateAsync({
      channel: 'studio',
      project_id: selectedProject.id,
      text,
      execution_target: localExecutionReady ? 'local' : '',
      attachments: attachmentsRef.current,
    })
  }

  function addAttachment(attachment: AIEntryAttachment) {
    attachmentsRef.current = [...attachmentsRef.current, attachment]
    setAttachments(attachmentsRef.current)
  }

  function removeAttachment(index: number) {
    attachmentsRef.current = attachmentsRef.current.filter((_, i) => i !== index)
    setAttachments(attachmentsRef.current)
  }

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.target.files ?? [])
    event.target.value = ''
    if (files.length === 0) return
    setEntryError(null)
    const uploads = files.map((file) => uploadAttachment(file))
    uploadPromisesRef.current = [...uploadPromisesRef.current, ...uploads]
    void Promise.allSettled(uploads).then(() => {
      uploadPromisesRef.current = uploadPromisesRef.current.filter((promise) => !uploads.includes(promise))
    })
  }

  async function uploadAttachment(file: File) {
    const validation = validateAIEntryFile(file)
    if (!validation.ok) {
      setEntryError({ message: validation.message })
      toast.error(validation.message)
      return
    }
    const id = `${file.name}-${file.size}-${Date.now()}`
    setUploading((items) => [...items, { id, name: file.name, progress: 0 }])
    try {
      const result = await uploadToOSS({
        purpose: 'ai_entry_attachment',
        file,
        onProgress: (progress) => {
          setUploading((items) => items.map((item) => item.id === id ? { ...item, progress } : item))
        },
      })
      addAttachment({
        type: validation.type,
        url: result.publicUrl,
        file_name: file.name,
        content_type: result.contentType,
        size: result.size,
        upload_id: result.uploadId,
        key: result.key,
      })
    } catch (err) {
      const message = err instanceof Error ? err.message : '素材上传失败，请重试。'
      setEntryError({ message })
      toast.error(message)
    } finally {
      setUploading((items) => items.filter((item) => item.id !== id))
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col px-1 pb-8">
      <section className="mx-auto flex min-h-[calc(100dvh-9rem)] w-full max-w-4xl flex-col justify-center gap-5 py-8 md:py-12">
        <div className="mx-auto flex w-full flex-col items-center text-center">
          <h1 className="text-balance text-[1.7rem] font-semibold leading-tight tracking-normal text-foreground md:text-3xl">
            {dashboardTitle}
          </h1>
        </div>

        <div className="mx-auto w-full overflow-hidden rounded-xl border border-border/80 bg-white shadow-[0_10px_35px_rgba(15,23,42,0.10)] dark:bg-card">
          <textarea
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
            placeholder="描述你想创作的内容、目标和素材要求..."
            className="min-h-[116px] w-full resize-none bg-transparent px-5 py-4 text-[15px] leading-7 text-foreground outline-none placeholder:text-muted-foreground md:min-h-[132px]"
          />

          {(attachments.length > 0 || uploading.length > 0) && (
            <div className="border-t border-border px-4 py-3">
              <div className="flex flex-wrap gap-2">
                {attachments.map((attachment, index) => (
                  <AttachmentChip
                    key={`${attachment.url}-${index}`}
                    attachment={attachment}
                    onRemove={() => removeAttachment(index)}
                  />
                ))}
                {uploading.map((item) => (
                  <span key={item.id} className="inline-flex h-8 max-w-full items-center gap-2 rounded-md border border-border bg-muted/40 px-2.5 text-xs text-muted-foreground">
                    <Paperclip className="size-3.5" />
                    <span className="max-w-[180px] truncate">{item.name}</span>
                    <span>{item.progress}%</span>
                  </span>
                ))}
              </div>
            </div>
          )}

          <div className="flex items-start justify-between gap-3 border-t border-border/80 bg-muted/20 px-3 py-3">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <label className="inline-flex size-8 cursor-pointer items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" htmlFor="ai-entry-attachments">
                <Paperclip className="size-4" />
                <span className="sr-only">上传参考素材</span>
              </label>
              <input
                id="ai-entry-attachments"
                className="sr-only"
                type="file"
                multiple
                onChange={handleFileChange}
              />

              {selectedProject && (
                <>
                  <ComposerMetaItem>
                    <PlatformAvatar avatarUrl={selectedProject.avatar_url} name={selectedProject.name} platform={selectedProject.platform} size="sm" />
                    <span className="truncate">项目：{selectedProject.name}</span>
                  </ComposerMetaItem>
                  <ComposerMetaItem>
                    {contentTypeLabel[selectedProject.platform] || selectedProject.platform}
                  </ComposerMetaItem>
                </>
              )}
              <ComposerMetaItem>
                <ExecutionIcon className="size-3.5" />
                {executionLabel}
              </ComposerMetaItem>
            </div>

            <Button
              type="button"
              aria-label="发送创建任务"
              onClick={() => { void handleSubmit() }}
              disabled={!canSubmit}
              loading={submitMutation.isPending}
              className="size-9 rounded-full bg-foreground p-0 text-background hover:bg-foreground/90 disabled:bg-muted disabled:text-muted-foreground"
            >
              <Send className="size-4" />
              <span className="sr-only">发送创建任务</span>
            </Button>
          </div>
        </div>

        <div className="mx-auto grid w-full gap-3 md:grid-cols-3">
          {selectedProject ? (
            <>
              <DashboardActionCard
                to={newTaskHref}
                icon={Plus}
                title="新建创作任务"
                description={`使用项目：${selectedProject.name}`}
              />
              {canCreateSchedule && (
                <DashboardActionCard
                  to={scheduleHref}
                  icon={Clock3}
                  title="安排自动计划"
                  description="设置周期内容生产"
                />
              )}
              <DashboardActionCard
                to="/projects"
                icon={Settings}
                title="管理项目配置"
                description="调整定位、风格和发布"
              />
            </>
          ) : (
            <DashboardActionCard
              to={projectsReturnHref({ type: 'seednote', intent: 'new' })}
              icon={Plus}
              title="创建第一个项目"
              description="先建立创作上下文"
            />
          )}
        </div>

        {dashboardBlocker && !entryError && (
          <div className="mx-auto flex w-full max-w-4xl items-center justify-between gap-3 rounded-lg border border-border bg-background px-4 py-3 text-sm">
            <span className="min-w-0 text-muted-foreground">{dashboardBlocker.message}</span>
            <Link className="shrink-0 font-medium text-primary hover:text-primary/80" to={dashboardBlocker.actionHref}>
              {dashboardBlocker.actionLabel}
            </Link>
          </div>
        )}

        {entryError && (
          <div className="mx-auto flex w-full max-w-4xl items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
            <span className="flex min-w-0 items-center gap-2 text-destructive">
              <AlertTriangle className="size-4 shrink-0" />
              <span>{entryError.message}</span>
            </span>
            {entryError.actionUrl && (
              <Link className="shrink-0 font-medium text-destructive underline-offset-4 hover:underline" to={entryError.actionUrl}>
                补充配置
              </Link>
            )}
          </div>
        )}
      </section>

      {hasError ? (
        <QueryErrorState onRetry={() => { refetchProjects() }} />
      ) : null}
    </div>
  )
}

function ComposerMetaItem({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex h-8 max-w-full items-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground">
      {children}
    </span>
  )
}

function DashboardActionCard({
  to,
  icon: Icon,
  title,
  description,
}: {
  to: string
  icon: LucideIcon
  title: string
  description: string
}) {
  return (
    <Link
      to={to}
      aria-label={title}
      className="group flex min-h-[86px] items-center gap-3 rounded-lg border border-border/80 bg-white px-4 py-3 text-left shadow-sm transition-colors hover:border-foreground/20 hover:bg-white dark:bg-card dark:hover:bg-card"
    >
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md text-foreground">
        <Icon className="size-4" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-foreground">{title}</span>
        <span className="mt-1 block truncate text-sm text-muted-foreground">{description}</span>
      </span>
      <ArrowRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
    </Link>
  )
}

function planCreateHref(project: Project) {
  const params = new URLSearchParams()
  params.set('create', 'true')
  params.set('type', project.platform)
  params.set('project_id', project.id)
  params.set('intent', 'schedule')
  return `/plans?${params.toString()}`
}

function canCreatePlanForProject(project?: Project) {
  return Boolean(project && (project.platform === 'article' || project.platform === 'seednote' || isVideoCreator(project.platform)))
}

function AttachmentChip({ attachment, onRemove }: { attachment: AIEntryAttachment; onRemove: () => void }) {
  const Icon = attachment.type === 'image' ? ImageIcon : FileText
  return (
    <span className="inline-flex h-8 max-w-full items-center gap-2 rounded-md border border-border bg-muted/40 px-2.5 text-xs text-foreground">
      <Icon className="size-3.5 text-muted-foreground" />
      <span className="max-w-[220px] truncate">{attachment.file_name || attachment.url || '素材'}</span>
      <button type="button" className="text-muted-foreground hover:text-foreground" onClick={onRemove} aria-label="移除素材">
        <X className="size-3.5" />
      </button>
    </span>
  )
}

function validateAIEntryFile(file: File): { ok: true; type: AIEntryAttachmentType } | { ok: false; message: string } {
  const type = classifyFile(file)
  if (!type) {
    return { ok: false, message: '暂不支持该素材格式。' }
  }
  const limit = type === 'document' || type === 'text' ? DOCUMENT_LIMIT : MEDIA_LIMIT
  if (file.size > limit) {
    return { ok: false, message: `文件大小不能超过 ${Math.round(limit / 1024 / 1024)}MB` }
  }
  return { ok: true, type }
}

function classifyFile(file: File): AIEntryAttachmentType | null {
  const mime = file.type.toLowerCase()
  const ext = file.name.split('.').pop()?.toLowerCase() ?? ''
  if (mime.startsWith('image/')) return 'image'
  if (mime.startsWith('audio/')) return 'audio'
  if (mime.startsWith('video/')) return 'video'
  if (mime.startsWith('text/') || ext === 'txt' || ext === 'md' || ext === 'markdown' || ext === 'csv') return 'text'
  if (DOCUMENT_EXTENSIONS.has(ext) || mime === 'application/pdf' || mime.includes('word') || mime.includes('presentation') || mime.includes('spreadsheet') || mime === 'application/json') {
    return 'document'
  }
  return null
}
