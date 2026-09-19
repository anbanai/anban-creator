import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { AlertTriangle, ArrowUpRight, Lightbulb } from 'lucide-react'
import { toast } from 'sonner'

import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import QueryErrorState from '@/components/QueryErrorState'
import { TaskComposerParameters } from '@/components/tasks/TaskComposerParameters'
import { SeednoteTemplateGallery } from '@/components/templates/SeednoteTemplateGallery'
import { useAgentExecutionProfiles } from '@/hooks/useAgentExecutionProfiles'
import { useImageCapabilities } from '@/hooks/useImageCapabilities'
import { api } from '@/lib/api'
import { projectsReturnHref } from '@/lib/command-center'
import { cheapestAvailableExecutionProfile, taskCostFor } from '@/lib/pricing'
import { queryKeys } from '@/lib/query-keys'
import { normalizeImageRatio, type ImageRatio } from '@/lib/schemas'
import { buildDashboardBlocker } from '@/lib/studio-ux'
import type { AgentExecutionProfileID } from '@/types'
import type { PromptAttachment } from '@/types/input-attachment'

interface EntryError {
  message: string
  actionUrl?: string
}

const IMAGE_CAPABILITY_RESELECTION_PATTERNS = [
  /\bunknown image capability key\b/i,
  /\bimage capability\s+(?!(?:service|provider|runtime)\b)(?:"[^"]+"|'[^']+'|[a-z0-9._-]+)\s+(?:is\s+)?(?:unknown|disabled|unauthori[sz]ed|not\s+authorized)\b/i,
  /\bimage capability\s+(?!(?:service|provider|runtime)\b)(?:"[^"]+"|'[^']+'|[a-z0-9._-]+)\s+requires?\s+.+\s+tier\b/i,
  /(?:图片|图像)能力\s*(?!(?:service|provider|runtime)\b)(?:["'“][^"'”]+["'”]|[「『][^」』]+[」』]|[a-z0-9._-]+)\s*(?:不存在|已禁用|被禁用|未授权|无权限|需要.{0,20}(?:等级|套餐|层级))/i,
]

function requiresImageCapabilityReselection(message: string) {
  return IMAGE_CAPABILITY_RESELECTION_PATTERNS.some((pattern) => pattern.test(message))
}

export default function DashboardPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const composerRef = useRef<HTMLDivElement>(null)
  const [prompt, setPrompt] = useState('')
  const [attachments, setAttachments] = useState<PromptAttachment[]>([])
  const [entryError, setEntryError] = useState<EntryError | null>(null)
  const [selectedProjectId, setSelectedProjectId] = useState<string | null>(null)
  const [executionProfile, setExecutionProfile] = useState<AgentExecutionProfileID | ''>('')
  const [quantity, setQuantity] = useState(1)
  const [imageRatio, setImageRatio] = useState<ImageRatio>('auto')
  const [imageCapabilityKey, setImageCapabilityKey] = useState('')
  const [imageCapabilityReselectionRequired, setImageCapabilityReselectionRequired] = useState(false)
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
  const activeProjects = useMemo(() => projects.filter((project) => project.status === 'active'), [projects])
  const selectedProject = activeProjects.find((project) => project.id === selectedProjectId) ?? activeProjects[0]
	const usesImageSettings = Boolean(selectedProject)

  const {
    data: apiKeysResponse,
    isLoading: apiKeysLoading,
    isError: apiKeysError,
    refetch: refetchApiKeys,
  } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: () => api.apiKeys.list(),
    staleTime: 60_000,
  })

  const profilesQuery = useAgentExecutionProfiles()
  const {
    items: imageCapabilities,
    defaultCapability: defaultImageCapability,
    isLoading: imageCapabilitiesLoading,
    isError: imageCapabilitiesError,
  } = useImageCapabilities()
  const billingCatalogQuery = useQuery({
    queryKey: queryKeys.billing.catalog,
    queryFn: () => api.billing.catalog(),
    staleTime: 60_000,
  })
  const platformConfigsQuery = useQuery({
    queryKey: queryKeys.projects.platformConfigs,
    queryFn: () => api.projects.platformConfigs(),
    staleTime: Infinity,
  })

  const platformConfigMap = useMemo(
    () => new Map((platformConfigsQuery.data ?? []).map((config) => [config.id, config])),
    [platformConfigsQuery.data],
  )
  const supportedImageRatios = platformConfigMap.get(selectedProject?.platform ?? '')?.supported_image_ratios ?? []
  const taskQuantityMax = selectedProject
    && ['article', 'seednote', 'moments'].includes(selectedProject.platform)
    ? 5
    : 1
  const selectedImageCapabilityAvailable = imageCapabilities.some((capability) => (
    capability.key === imageCapabilityKey
    && capability.enabled === true
    && capability.price_available === true
  ))
  const defaultCapabilityAvailable = imageCapabilities.some((capability) => (
    capability.key === defaultImageCapability
    && capability.enabled === true
    && capability.price_available === true
  ))
  const effectiveImageCapabilityKey = !imageCapabilityReselectionRequired && selectedImageCapabilityAvailable
    ? imageCapabilityKey
    : !imageCapabilityReselectionRequired && defaultCapabilityAvailable ? defaultImageCapability : ''
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
  const requiredComposerQueryError = apiKeysError
    || (usesImageSettings && imageCapabilitiesError)
    || profilesQuery.isError
    || billingCatalogQuery.isError

  useEffect(() => {
    if (projectsLoading || platformConfigsQuery.isLoading || platformConfigsQuery.isError) return
    const nextProject = activeProjects.find((project) => project.id === selectedProjectId) ?? activeProjects[0]
    if (!nextProject) {
      if (selectedProjectId !== null) setSelectedProjectId(null)
      return
    }
    if (selectedProjectId !== nextProject.id) {
      setSelectedProjectId(nextProject.id)
      setImageRatio(normalizeImageRatio(
        nextProject.image_ratio || platformConfigMap.get(nextProject.platform)?.default_image_ratio,
      ))
      const nextQuantityMax = ['article', 'seednote', 'moments'].includes(nextProject.platform) ? 5 : 1
      setQuantity((current) => Math.min(current, nextQuantityMax))
    }
  }, [activeProjects, platformConfigMap, platformConfigsQuery.isError, platformConfigsQuery.isLoading, projectsLoading, selectedProjectId])

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
      const createdTasks = (result.tasks ?? []).filter((task) => Boolean(task?.id))
      if (result.status === 'created' && createdTasks.length > 0) {
        setEntryError(null)
        setPrompt('')
        attachmentController.clear()
        await queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
        if (createdTasks.length === 1) {
          navigate(`/tasks/${createdTasks[0].id}`)
          return
        }
        toast.success(result.message || `已创建 ${createdTasks.length} 个任务`)
        navigate('/tasks')
        return
      }
      if (result.status === 'needs_configuration') {
        setEntryError({ message: result.message || '还需要补充配置。', actionUrl: result.action_url })
        return
      }
      const message = result.message || 'AI 入口暂不可用，请稍后重试。'
      if (result.status === 'error' && requiresImageCapabilityReselection(message)) {
        setImageCapabilityReselectionRequired(true)
        setImageCapabilityKey('')
        setEntryError({ message: `${message} 请重新选择图片能力。` })
        await queryClient.refetchQueries({ queryKey: queryKeys.imageCapabilities.all, exact: true })
        return
      }
      setEntryError({ message })
    },
    onError: (err) => {
      const message = err instanceof Error ? err.message : '创建任务失败，请重试。'
      setEntryError({ message })
    },
  })

  const hasError = projectsError || platformConfigsQuery.isError || requiredComposerQueryError
  const dashboardBlocker = buildDashboardBlocker({
    projectsLoading,
    projectsError,
    activeProjectCount: activeProjects.length,
    apiKeysReady: apiKeysResponse ? (apiKeysResponse.items || []).length > 0 : null,
  })
  const canSubmit = Boolean(selectedProjectId && selectedProject)
    && executionProfileReady
    && (!usesImageSettings || Boolean(effectiveImageCapabilityKey))
    && !projectsError
    && !apiKeysLoading
    && !platformConfigsQuery.isLoading
    && !platformConfigsQuery.isError
    && !requiredComposerQueryError
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
    if (usesImageSettings && !effectiveImageCapabilityKey) {
      setEntryError({ message: '当前图像能力不可用，请稍后重试。' })
      return
    }
    setEntryError(null)
    await submitMutation.mutateAsync({
      channel: 'studio',
      project_id: selectedProject.id,
      text,
      execution_profile: executionProfile as AgentExecutionProfileID,
      quantity,
      attachments: attachmentController.toInputAttachments(),
      ...(usesImageSettings ? {
        image_ratio: imageRatio,
        image_capability_key: effectiveImageCapabilityKey,
      } : {}),
    })
  }

  function handleProjectChange(projectId: string | null) {
    setSelectedProjectId(projectId)
    const nextProject = activeProjects.find((candidate) => candidate.id === projectId)
    if (!nextProject) {
      setQuantity(1)
      setImageRatio('auto')
      return
    }
    const nextQuantityMax = ['article', 'seednote', 'moments'].includes(nextProject.platform) ? 5 : 1
    setQuantity((current) => Math.min(current, nextQuantityMax))
    setImageRatio(normalizeImageRatio(
      nextProject.image_ratio || platformConfigMap.get(nextProject.platform)?.default_image_ratio,
    ))
  }

  function handleImageCapabilityChange(capabilityKey: string) {
    setImageCapabilityKey(capabilityKey)
    if (imageCapabilityReselectionRequired) {
      setImageCapabilityReselectionRequired(false)
      setEntryError(null)
    }
  }

  const starterPrompts = selectedProject?.platform === 'montage'
    ? ['制作一条 30 秒的品牌介绍视频，突出产品特点，画面简洁。', '把上传的口播视频剪成节奏紧凑的短片，保留重点并加上字幕。', '根据项目定位，制作一个适合社交平台传播的产品演示视频。']
    : ['围绕项目定位，创作一篇适合新手阅读的实用指南，给出具体步骤。', '把上传的素材整理成一篇内容，突出重点，语言自然、有条理。', '围绕项目主题，创作一份值得收藏的实用清单，附上使用建议。']

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col pb-8">
      <section className="mx-auto flex w-full max-w-4xl flex-col gap-6 py-8 md:pt-[min(12vh,7rem)] md:pb-12">
        <div className="mx-auto flex w-full flex-col items-center text-center">
          <p className="mb-3 text-sm font-medium text-primary">从一个想法开始</p>
          <h1 className="text-balance text-3xl font-semibold leading-tight tracking-tight text-foreground md:text-4xl">
            今天想创作什么？
          </h1>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">选好项目，写下想法或上传素材，剩下的交给 AI 助手。</p>
        </div>

        <div ref={composerRef} className="mx-auto w-full max-w-3xl">
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
            leadingTools={(
              <div className="flex min-w-0 flex-wrap items-center gap-1">
                <ProjectContextControl
                  mode="select"
                  projects={activeProjects}
                  value={selectedProjectId}
                  onValueChange={handleProjectChange}
                  allowNoProject={false}
                  loading={projectsLoading || platformConfigsQuery.isLoading}
                  disabled={submitMutation.isPending || platformConfigsQuery.isError}
                  placeholder="选择项目"
                  createProjectHref={projectsReturnHref({ type: 'seednote', intent: 'new' })}
                  ariaLabel={selectedProject
                    ? `项目：${selectedProject.name}`
                    : projectsLoading || platformConfigsQuery.isLoading
                      ? '项目：加载中'
                      : '项目：未选择'}
                  compact
                />
                <TaskComposerParameters
                  execution={{
                    profiles: profilesQuery.data ?? [],
                    value: executionProfile,
                    onChange: setExecutionProfile,
                    loading: profilesQuery.isLoading || billingCatalogQuery.isLoading,
                    disabled: profilesQuery.isError || billingCatalogQuery.isError,
                    catalog: billingCatalogQuery.data,
                    taskType: selectedProject?.platform,
                  }}
                  image={usesImageSettings ? {
                    ratios: supportedImageRatios,
                    ratio: imageRatio,
                    onRatioChange: setImageRatio,
                    capabilities: imageCapabilities,
                    capabilityKey: effectiveImageCapabilityKey,
                    onCapabilityChange: handleImageCapabilityChange,
                    loading: imageCapabilitiesLoading,
                    disabled: imageCapabilitiesError,
                  } : undefined}
                  quantity={{
                    label: '任务数量',
                    value: quantity,
                    min: 1,
                    max: taskQuantityMax,
                    onChange: setQuantity,
                  }}
                  disabled={submitMutation.isPending}
                />
              </div>
            )}
          />
        </div>

        <div className="mx-auto flex w-full max-w-3xl flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>任务创建后，可在「任务」中查看进度与作品</span>
          {executionProfilePrice !== undefined && <span className="tabular-nums">任务固定费 { (executionProfilePrice * quantity).toLocaleString() } 积分{quantity > 1 ? ` / ${quantity} 个任务` : ''} · 图片、视频等按用量另计</span>}
        </div>

        {!prompt.trim() && (
          <section aria-label="创作灵感" className="mx-auto w-full max-w-3xl">
            <p className="mb-3 flex items-center gap-2 text-xs text-muted-foreground"><Lightbulb className="size-4" />还没有头绪？试试这样开始</p>
            <div className="grid gap-2 sm:grid-cols-3">
              {starterPrompts.map((starter, index) => (
                <button key={starter} type="button" disabled={submitMutation.isPending}
                  onClick={() => { setPrompt(starter); composerRef.current?.querySelector('textarea')?.focus() }}
                  className="group rounded-xl border border-border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-primary/5 disabled:opacity-50"
                >
                  <span className="flex items-center justify-between text-sm font-medium">{(selectedProject?.platform === 'montage' ? ['品牌介绍视频', '口播精剪', '产品演示视频'] : ['从零开始创作', '把素材变成内容', '整理实用清单'])[index]}<ArrowUpRight className="size-4 text-muted-foreground group-hover:text-primary" /></span>
                  <span className="mt-2 block text-xs leading-5 text-muted-foreground">{starter}</span>
                </button>
              ))}
            </div>
          </section>
        )}

        <SeednoteTemplateGallery
          platform={selectedProject?.platform}
          onApply={(templatePrompt) => setPrompt(templatePrompt)}
          className="mx-auto w-full max-w-3xl"
        />

        {dashboardBlocker && !entryError && (
          <div className="mx-auto flex w-full max-w-3xl items-center justify-between gap-3 rounded-lg border border-border bg-background px-4 py-3 text-sm">
            <span className="min-w-0 text-muted-foreground">{dashboardBlocker.message}</span>
            <Link className="shrink-0 font-medium text-primary hover:text-primary/80" to={dashboardBlocker.actionHref}>
              {dashboardBlocker.actionLabel}
            </Link>
          </div>
        )}

        {entryError && (
          <div role="alert" className="mx-auto flex w-full max-w-3xl items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
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
        <QueryErrorState onRetry={() => {
          void refetchProjects()
          void refetchApiKeys()
          void platformConfigsQuery.refetch()
          void queryClient.refetchQueries({ queryKey: queryKeys.imageCapabilities.all, exact: true })
          void profilesQuery.refetch()
          void billingCatalogQuery.refetch()
        }} />
      ) : null}
    </div>
  )
}
