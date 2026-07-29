import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { AlertTriangle } from 'lucide-react'

import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import QueryErrorState from '@/components/QueryErrorState'
import { ExecutionProfileSelector } from '@/components/tasks/ExecutionProfileSelector'
import { useAgentExecutionProfiles } from '@/hooks/useAgentExecutionProfiles'
import { api } from '@/lib/api'
import { hasUsableModelConfig, projectsReturnHref } from '@/lib/command-center'
import { contentTypeLabel } from '@/lib/labels'
import { cheapestAvailableExecutionProfile, taskCostFor } from '@/lib/pricing'
import { queryKeys } from '@/lib/query-keys'
import { buildDashboardBlocker } from '@/lib/studio-ux'
import type { AgentExecutionProfileID } from '@/types'
import type { PromptAttachment } from '@/types/input-attachment'

interface EntryError {
  message: string
  actionUrl?: string
}

export default function DashboardPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [prompt, setPrompt] = useState('')
  const [attachments, setAttachments] = useState<PromptAttachment[]>([])
  const [entryError, setEntryError] = useState<EntryError | null>(null)
  const [selectedProjectId, setSelectedProjectId] = useState<string | null>(null)
  const [executionProfile, setExecutionProfile] = useState<AgentExecutionProfileID | ''>('')
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'ai_entry_attachment' },
    policy: GENERAL_AGENT_ATTACHMENT_POLICY,
    attachments,
    onAttachmentsChange: setAttachments,
  })

  const { data: projects = [], isLoading: projectsLoading, isError: projectsError, refetch: refetchProjects } = useQuery({
    queryKey: ['projects', 'dashboard', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
    staleTime: 60_000,
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

  const profilesQuery = useAgentExecutionProfiles()
  const billingCatalogQuery = useQuery({
    queryKey: queryKeys.billing.catalog,
    queryFn: () => api.billing.catalog(),
    staleTime: 60_000,
  })

  const activeProjects = useMemo(() => projects.filter((project) => project.status === 'active'), [projects])
  const selectedProject = activeProjects.find((project) => project.id === selectedProjectId) ?? activeProjects[0]
  const defaultExecutionProfile = cheapestAvailableExecutionProfile(
    profilesQuery.data,
    billingCatalogQuery.data,
    selectedProject?.platform ?? '',
  )
  const executionProfilePrice = selectedProject && executionProfile
    ? taskCostFor(billingCatalogQuery.data, selectedProject.platform, executionProfile)
    : undefined
  const selectedExecutionProfile = profilesQuery.data?.find((profile) => profile.id === executionProfile)
  const executionProfileReady = Boolean(
    executionProfile
    && selectedExecutionProfile?.available
    && executionProfilePrice !== undefined,
  )

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

  useEffect(() => {
    if (!selectedProject || !defaultExecutionProfile) return
    const selectedProfile = profilesQuery.data?.find((profile) => profile.id === executionProfile)
    const selectionIsValid = selectedProfile?.available
      && taskCostFor(billingCatalogQuery.data, selectedProject.platform, executionProfile || undefined) !== undefined
    if (!selectionIsValid) setExecutionProfile(defaultExecutionProfile)
  }, [billingCatalogQuery.data, defaultExecutionProfile, executionProfile, profilesQuery.data, selectedProject])

  const submitMutation = useMutation({
    mutationFn: (payload: Parameters<typeof api.aiEntry.submit>[0]) => api.aiEntry.submit(payload),
    onSuccess: async (result) => {
      if (result.status === 'created' && result.task?.id) {
        setEntryError(null)
        setPrompt('')
        attachmentController.clear()
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
    && executionProfileReady
    && !dashboardBlocker?.blocking
    && !submitMutation.isPending
    && !attachmentController.uploading
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
    if (!executionProfileReady) {
      setEntryError({ message: '所选执行配置当前不可用，请重新选择。' })
      return
    }
    setEntryError(null)
    await submitMutation.mutateAsync({
      channel: 'studio',
      project_id: selectedProject.id,
      text,
      execution_profile: executionProfile as AgentExecutionProfileID,
      attachments: attachmentController.toInputAttachments(),
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

        <div className="mx-auto w-full max-w-3xl">
          <AgentPromptInput
            value={{ prompt, attachments }}
            onChange={(value) => {
              setPrompt(value.prompt)
              setAttachments(value.attachments)
            }}
            onSubmit={handleSubmit}
            attachmentController={attachmentController}
            attachmentPolicy={GENERAL_AGENT_ATTACHMENT_POLICY}
            placeholder="描述你想创作的内容、目标和素材要求..."
            submitLabel="发送创建任务"
            submitting={submitMutation.isPending}
            submitDisabled={!canSubmit}
            contextBar={(
              <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                <ProjectContextControl
                  mode="select"
                  projects={activeProjects}
                  value={selectedProjectId}
                  onValueChange={(projectId) => setSelectedProjectId(projectId)}
                  allowNoProject={false}
                  loading={projectsLoading}
                  placeholder="选择项目"
                  createProjectHref={projectsReturnHref({ type: 'seednote', intent: 'new' })}
                />
                {selectedProject ? <ComposerMetaItem>{contentTypeLabel[selectedProject.platform] || selectedProject.platform}</ComposerMetaItem> : null}
              </div>
            )}
          />
        </div>

        <div className="mx-auto flex w-full max-w-3xl flex-col gap-2">
          <div className="flex items-center justify-between gap-3">
            <p className="text-sm font-medium text-foreground">执行配置</p>
            {executionProfilePrice !== undefined ? (
              <p aria-live="polite" className="text-sm tabular-nums text-muted-foreground">
                {executionProfilePrice.toLocaleString()} 积分
              </p>
            ) : null}
          </div>
          <ExecutionProfileSelector
            profiles={profilesQuery.data ?? []}
            value={executionProfile}
            onChange={setExecutionProfile}
            loading={profilesQuery.isLoading || billingCatalogQuery.isLoading}
            disabled={submitMutation.isPending}
          />
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
