import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import {
  AlertTriangle,
  ArrowRight,
  FileText,
  ImageIcon,
  Paperclip,
  Send,
  Sparkles,
  X,
} from 'lucide-react'

import QueryErrorState from '@/components/QueryErrorState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/common/button'
import { api } from '@/lib/api'
import type { AIEntryAttachment, AIEntryAttachmentType } from '@/lib/api/ai-entry'
import { hasUsableModelConfig } from '@/lib/command-center'
import { uploadToOSS } from '@/lib/direct-upload'
import { contentTypeLabel } from '@/lib/labels'
import { queryKeys } from '@/lib/query-keys'
import { buildDashboardBlocker } from '@/lib/studio-ux'
import { getLocalExecutorStatus, isDesktop } from '@/lib/tauri'

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
  const [selectedProjectId, setSelectedProjectId] = useState('')
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
  const selectedProject = activeProjects.find((project) => project.id === selectedProjectId) ?? activeProjects[0]
  const localExecutionReady = desktopMode && Boolean(localExecutorStatus?.available)

  useEffect(() => {
    if (activeProjects.length === 0) {
      setSelectedProjectId('')
      return
    }
    if (!selectedProjectId || !activeProjects.some((project) => project.id === selectedProjectId)) {
      setSelectedProjectId(activeProjects[0].id)
    }
  }, [activeProjects, selectedProjectId])

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
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-1 pb-8">
      <section className="flex min-h-[58vh] flex-col justify-center gap-6 py-6 md:py-10">
        <div className="mx-auto flex w-full max-w-4xl flex-col items-center text-center">
          <div className="mb-4 flex size-11 items-center justify-center rounded-full border border-border bg-background shadow-sm">
            <Sparkles className="size-5 text-primary" />
          </div>
          <h1 className="text-balance text-3xl font-semibold tracking-normal text-foreground md:text-4xl">
            今天想让 Anban 帮你创作什么？
          </h1>
        </div>

        <div className="mx-auto w-full max-w-4xl rounded-xl border border-border bg-background shadow-sm">
          <textarea
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
            placeholder="描述你想创作的内容、目标和素材要求..."
            className="min-h-[180px] w-full resize-none rounded-t-xl bg-transparent px-5 py-5 text-base leading-7 text-foreground outline-none placeholder:text-muted-foreground md:min-h-[210px]"
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

          <div className="flex flex-col gap-3 border-t border-border px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex flex-wrap items-center gap-2">
              <label className="inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium text-foreground transition-colors hover:bg-accent" htmlFor="ai-entry-attachments">
                <Paperclip className="size-4" />
                上传参考素材
              </label>
              <input
                id="ai-entry-attachments"
                className="sr-only"
                type="file"
                multiple
                onChange={handleFileChange}
              />

              <div className="relative">
                <select
                  className="h-9 min-w-[180px] appearance-none rounded-md border border-border bg-background py-0 pl-3 pr-8 text-sm font-medium text-foreground outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50"
                  value={selectedProject?.id ?? ''}
                  onChange={(event) => setSelectedProjectId(event.target.value)}
                  disabled={projectsLoading || activeProjects.length === 0}
                  aria-label="选择项目"
                >
                  {activeProjects.length === 0 ? (
                    <option value="">暂无活跃项目</option>
                  ) : activeProjects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.name}
                    </option>
                  ))}
                </select>
                <ArrowRight className="pointer-events-none absolute right-2.5 top-1/2 size-3.5 -translate-y-1/2 rotate-90 text-muted-foreground" />
              </div>

              {selectedProject && (
                <Badge variant="secondary" className="h-9 rounded-md px-3">
                  {contentTypeLabel[selectedProject.platform]}
                </Badge>
              )}
            </div>

            <Button
              type="button"
              onClick={() => { void handleSubmit() }}
              disabled={!canSubmit}
              loading={submitMutation.isPending}
            >
              <Send className="size-4" />
              发送创建任务
            </Button>
          </div>
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
