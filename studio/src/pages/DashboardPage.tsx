import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import {
  AlertTriangle,
  Check,
  ChevronDown,
  Cloud,
  FolderPlus,
  Monitor,
  Send,
  Search,
} from 'lucide-react'

import { PlatformAvatar } from '@/components/PlatformAvatar'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import QueryErrorState from '@/components/QueryErrorState'
import { Button } from '@/components/common/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { api } from '@/lib/api'
import { hasUsableModelConfig, projectsReturnHref } from '@/lib/command-center'
import { contentTypeLabel } from '@/lib/labels'
import { queryKeys } from '@/lib/query-keys'
import { buildDashboardBlocker } from '@/lib/studio-ux'
import { getLocalExecutorStatus, isDesktop } from '@/lib/tauri'
import { cn } from '@/lib/utils'
import type { Project } from '@/types'
import type { InputAttachment } from '@/types/input-attachment'

interface EntryError {
  message: string
  actionUrl?: string
}

export default function DashboardPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const desktopMode = isDesktop()

  const [prompt, setPrompt] = useState('')
  const [attachments, setAttachments] = useState<InputAttachment[]>([])
  const [attachmentsUploading, setAttachmentsUploading] = useState(false)
  const [entryError, setEntryError] = useState<EntryError | null>(null)
  const [selectedProjectId, setSelectedProjectId] = useState<string | null>(null)

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
  const ExecutionIcon = localExecutionReady ? Monitor : Cloud
  const executionLabel = localExecutionReady ? '本地模式' : '云端模式'

  useEffect(() => {
    if (projectsLoading) return
    const nextProject = activeProjects.find((project) => project.id === selectedProjectId) ?? activeProjects[0]
    if (!nextProject) {
      if (selectedProjectId !== null) setSelectedProjectId(null)
      return
    }
    if (selectedProjectId !== nextProject.id) {
      setSelectedProjectId(nextProject.id)
    }
  }, [activeProjects, projectsLoading, selectedProjectId])

  const submitMutation = useMutation({
    mutationFn: (payload: Parameters<typeof api.aiEntry.submit>[0]) => api.aiEntry.submit(payload),
    onSuccess: async (result) => {
      if (result.status === 'created' && result.task?.id) {
        setEntryError(null)
        setPrompt('')
        setAttachments([])
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
  const canSubmit = Boolean(selectedProjectId && selectedProject)
    && !dashboardBlocker?.blocking
    && !submitMutation.isPending
    && !attachmentsUploading
    && Boolean(prompt.trim())

  async function handleSubmit() {
    const text = prompt.trim()
    if (!selectedProject) {
      setEntryError({ message: '请先创建或选择一个活跃项目。', actionUrl: '/projects' })
      return
    }
    if (!text) {
      setEntryError({ message: '请输入创作需求。' })
      return
    }
    setEntryError(null)
    await submitMutation.mutateAsync({
      channel: 'studio',
      project_id: selectedProject.id,
      text,
      execution_target: localExecutionReady ? 'local' : '',
      attachments,
    })
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col px-1 pb-8">
      <section className="mx-auto flex min-h-[calc(100dvh-9rem)] w-full max-w-4xl flex-col justify-center gap-5 py-8 md:py-12">
        <div className="mx-auto flex w-full flex-col items-center text-center">
          <h1 className="text-balance text-3xl font-medium leading-tight tracking-normal text-foreground md:text-[2rem]">
            首页
          </h1>
        </div>

        <div className="mx-auto w-full max-w-3xl overflow-hidden rounded-[20px] border border-border/70 bg-white shadow-[0_18px_55px_rgba(15,23,42,0.12)] dark:bg-card">
          <div className="relative">
            <textarea
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="描述你想创作的内容、目标和素材要求..."
              className="min-h-[92px] w-full resize-none bg-transparent px-5 py-4 pb-14 pr-16 text-[15px] leading-7 text-foreground outline-none placeholder:text-muted-foreground md:min-h-[102px]"
            />
            <Button
              type="button"
              aria-label="发送创建任务"
              onClick={() => { void handleSubmit() }}
              disabled={!canSubmit}
              loading={submitMutation.isPending}
              className="absolute bottom-3 right-3 size-9 rounded-full bg-foreground p-0 text-background shadow-sm hover:bg-foreground/90 disabled:bg-muted disabled:text-muted-foreground"
            >
              <Send className="size-4" />
              <span className="sr-only">发送创建任务</span>
            </Button>
          </div>

          <div className="border-t border-border px-4 py-3">
            <ReferenceMaterialInput
              value={attachments}
              onChange={setAttachments}
              allowedTypes={['image', 'audio', 'video', 'document', 'text']}
              instructionEnabled
              compact
              hint="可添加图片、音频、视频、文档或文本素材；图片可填写说明。"
              onUploadingChange={setAttachmentsUploading}
            />
          </div>

          <div className="flex min-h-12 items-center justify-between gap-3 border-t border-border/70 bg-muted/35 px-3 py-2">
            <div className="flex min-w-0 flex-wrap items-center gap-1.5">
              <ProjectSelectControl
                projects={activeProjects}
                selectedProject={selectedProject}
                onSelect={(projectId) => setSelectedProjectId(projectId)}
              />
              {selectedProject && (
                <>
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
          </div>
        </div>

        {dashboardBlocker && !entryError && (
          <div className="mx-auto flex w-full max-w-3xl items-center justify-between gap-3 rounded-lg border border-border bg-background px-4 py-3 text-sm">
            <span className="min-w-0 text-muted-foreground">{dashboardBlocker.message}</span>
            <Link className="shrink-0 font-medium text-primary hover:text-primary/80" to={dashboardBlocker.actionHref}>
              {dashboardBlocker.actionLabel}
            </Link>
          </div>
        )}

        {entryError && (
          <div className="mx-auto flex w-full max-w-3xl items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
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

function ProjectSelectControl({
  projects,
  selectedProject,
  onSelect,
}: {
  projects: Project[]
  selectedProject?: Project
  onSelect: (projectId: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const createProjectHref = projectsReturnHref({ type: 'seednote', intent: 'new' })
  const filteredProjects = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return projects
    return projects.filter((project) => {
      const label = `${project.name} ${contentTypeLabel[project.platform] || project.platform}`.toLowerCase()
      return label.includes(keyword)
    })
  }, [projects, search])

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (!nextOpen) setSearch('')
      }}
    >
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label={selectedProject ? `选择项目 ${selectedProject.name}` : '选择项目'}
            aria-expanded={open}
            className="inline-flex h-8 max-w-[220px] items-center gap-1.5 rounded-md bg-background px-2 text-xs text-foreground shadow-sm ring-1 ring-border/70 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          />
        }
      >
        {selectedProject ? (
          <>
            <MiniProjectAvatar project={selectedProject} />
            <span className="truncate">{selectedProject.name}</span>
          </>
        ) : (
          <span className="truncate text-muted-foreground">选择项目</span>
        )}
        <ChevronDown className={cn('size-3.5 shrink-0 text-muted-foreground transition-transform', open && 'rotate-180')} />
      </PopoverTrigger>
      <PopoverContent className="w-72 gap-0 rounded-xl border border-border/70 bg-white p-0 shadow-[0_16px_45px_rgba(15,23,42,0.18)] ring-0 dark:bg-card" align="start" side="top" sideOffset={8}>
        <div className="flex h-9 items-center gap-2 border-b border-border/70 px-3">
          <Search className="size-3.5 shrink-0 text-muted-foreground" />
          <input
            aria-label="搜索项目"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="搜索项目"
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>

        <div className="max-h-64 overflow-y-auto p-1">
          {filteredProjects.length > 0 ? (
            filteredProjects.map((project) => {
              const selected = selectedProject?.id === project.id
              return (
                <button
                  key={project.id}
                  type="button"
                  onClick={() => {
                    onSelect(project.id)
                    setOpen(false)
                    setSearch('')
                  }}
                  className={cn(
                    'flex h-10 w-full items-center gap-2 rounded-lg px-2 text-left text-sm transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40',
                    selected && 'bg-accent/70',
                  )}
                >
                  <MiniProjectAvatar project={project} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium text-foreground">{project.name}</span>
                    <span className="block truncate text-[11px] text-muted-foreground">
                      {contentTypeLabel[project.platform] || project.platform}
                    </span>
                  </span>
                  <Check className={cn('size-4 shrink-0 text-foreground', selected ? 'opacity-100' : 'opacity-0')} />
                </button>
              )
            })
          ) : (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">没有找到项目</div>
          )}
        </div>

        <div className="border-t border-border/70 p-1">
          <Link
            to={createProjectHref}
            onClick={() => setOpen(false)}
            className="flex h-9 items-center gap-2 rounded-lg px-2 text-sm text-foreground transition-colors hover:bg-accent"
          >
            <FolderPlus className="size-4 text-muted-foreground" />
            <span className="flex-1">创建项目</span>
          </Link>
        </div>
      </PopoverContent>
    </Popover>
  )
}

function MiniProjectAvatar({ project }: { project: Project }) {
  return (
    <span className="grid size-4 shrink-0 place-items-center overflow-hidden rounded-full">
      <span className="scale-50">
        <PlatformAvatar avatarUrl={project.avatar_url} name={project.name} platform={project.platform} size="sm" />
      </span>
    </span>
  )
}
