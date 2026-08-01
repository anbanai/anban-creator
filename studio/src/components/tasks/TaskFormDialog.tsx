import { useEffect, useMemo, useRef, useState, type BaseSyntheticEvent } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm, useWatch, type FieldPath, type FieldPathValue, type Resolver } from 'react-hook-form'
import { Link } from 'react-router-dom'
import { Images, Minus, Package, Plus, Stamp, Target } from 'lucide-react'
import { toast } from 'sonner'

import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { AgentPackSchemaFields } from '@/components/agent-pack/AgentPackSchemaFields'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import { Button } from '@/components/common/button'
import { ImageCapabilitySelector } from '@/components/ImageCapabilitySelector'
import { MontageCreationPanel } from '@/components/montage/MontageCreationPanel'
import { MultiImageUpload } from '@/components/projects/MultiImageUpload'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useImageCapabilities } from '@/hooks/useImageCapabilities'
import { useAgentExecutionProfiles } from '@/hooks/useAgentExecutionProfiles'
import { useAgentPacks } from '@/hooks/useAgentPacks'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { api } from '@/lib/api'
import { projectsReturnHref } from '@/lib/command-center'
import { getApiErrorMessage } from '@/lib/http-client'
import { contentTypeLabel, ecommerceLanguageOptions, ecommerceModuleCatalog, ecommerceTargetPlatformOptions } from '@/lib/labels'
import { initialMontageInput } from '@/lib/montage-form'
import { cheapestAvailableExecutionProfile } from '@/lib/pricing'
import { queryKeys } from '@/lib/query-keys'
import { createTaskSchema } from '@/lib/schemas'
import { taskCreationCostPreview } from '@/lib/studio-ux'
import { cloneTaskFormDefaults, createTaskFormDefaults, switchTaskFormDefaults, taskFormValuesToRequest, type TaskFormDefaults } from '@/lib/task-form'
import type { CreateTaskRequest, Project, Task, TaskType } from '@/types'
import { ImageAspectRatioField } from './ImageAspectRatioField'
import { ExecutionProfileSelector } from './ExecutionProfileSelector'
import { TaskTimePricingNotice } from '@/components/billing/TaskTimePricingNotice'

const SEEDNOTE_ATTACHMENT_POLICY = {
  allowedTypes: ['image'],
  maxCount: GENERAL_AGENT_ATTACHMENT_POLICY.maxCount,
  maxBytes: GENERAL_AGENT_ATTACHMENT_POLICY.maxBytes,
} as const
const SEEDNOTE_ATTACHMENT_BLOCKER = '种草笔记仅支持图片附件，请移除其他附件后继续。'

export interface TaskFormDialogProps {
  open: boolean
  mode: 'create' | 'clone'
  sourceTask?: Task
  initialProjectId?: string
  initialType?: TaskType
  onOpenChange: (open: boolean) => void
  onCreated: (task: Task, quantity: number) => void
  shouldHandleResult?: () => boolean
}

function createInitialDefaults(project?: Project, initialType?: TaskType): TaskFormDefaults {
  const defaults = createTaskFormDefaults(project)
  if (initialType === 'viral_analysis') {
    return {
      ...createTaskFormDefaults(project?.platform === 'seednote' ? project : undefined),
      type: 'viral_analysis',
    }
  }
  if (project || !initialType || initialType === defaults.type) return defaults
  return {
    ...defaults,
    type: initialType,
    montage_input: initialType === 'montage'
      ? initialMontageInput('')
      : undefined,
  }
}

export function TaskFormDialog({
  open,
  mode,
  sourceTask,
  initialProjectId,
  initialType,
  onOpenChange,
  onCreated,
  shouldHandleResult,
}: TaskFormDialogProps) {
  const queryClient = useQueryClient()
  const { submit } = useSubmitLock()
  const initializedKeyRef = useRef<string | undefined>(undefined)
  const [montageUploading, setMontageUploading] = useState(false)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)

  const { data: projects = [], isLoading: projectsLoading } = useQuery({
    queryKey: ['projects', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
  })
  const { data: billingWallet } = useQuery({
    queryKey: queryKeys.billing.wallet,
    queryFn: () => api.billing.wallet(),
  })
  const { data: billingCatalog, refetch: refetchBillingCatalog } = useQuery({
    queryKey: queryKeys.billing.catalog,
    queryFn: () => api.billing.catalog(),
    enabled: open,
    staleTime: 0,
	})
	const executionProfilesQuery = useAgentExecutionProfiles()
	const agentPacksQuery = useAgentPacks()
	const { items: imageCapabilityOptions, defaultCapability, isLoading: imageCapabilitiesLoading, isError: imageCapabilitiesError } = useImageCapabilities()

  const form = useForm<TaskFormDefaults>({
    resolver: zodResolver(createTaskSchema) as Resolver<TaskFormDefaults>,
    defaultValues: createTaskFormDefaults(),
  })
  const watchedType = useWatch({ control: form.control, name: 'type' })
  const watchedExecutionProfile = useWatch({ control: form.control, name: 'execution_profile' })
  const attachmentPolicy = watchedType === 'seednote'
    ? SEEDNOTE_ATTACHMENT_POLICY
    : GENERAL_AGENT_ATTACHMENT_POLICY
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'ai_entry_attachment' },
    policy: attachmentPolicy,
    initialAttachments: [],
    onAttachmentsChange: () => {
      form.setValue('input_attachments', attachmentController.toInputAttachments(), {
        shouldDirty: true,
        shouldValidate: true,
      })
    },
  })
  const resetAttachments = attachmentController.reset

  const watchedProjectId = useWatch({ control: form.control, name: 'project_id' })
  const watchedPrompt = useWatch({ control: form.control, name: 'prompt' }) ?? ''
  const watchedImageCapabilityKey = useWatch({ control: form.control, name: 'image_capability_key' }) ?? ''
  const watchedImageRatio = useWatch({ control: form.control, name: 'image_ratio' }) ?? ''
  const quantity = useWatch({ control: form.control, name: 'quantity' }) ?? 1

  useEffect(() => {
    const transition = billingCatalog?.task_time_pricing?.next_transition_at
    if (!open || !transition) return
    const delay = new Date(transition).getTime() - Date.now() + 250
    if (delay <= 0) {
      void refetchBillingCatalog()
      return
    }
    const timer = window.setTimeout(() => { void refetchBillingCatalog() }, delay)
    return () => window.clearTimeout(timer)
  }, [billingCatalog?.task_time_pricing?.next_transition_at, open, refetchBillingCatalog])
  const watermark = useWatch({ control: form.control, name: 'watermark' }) ?? false
  const goalMode = useWatch({ control: form.control, name: 'goal_mode' }) ?? false
  const goal = useWatch({ control: form.control, name: 'goal' }) ?? ''
  const hasContentImage = useWatch({ control: form.control, name: 'has_content_image' }) ?? true
  const hasTailImage = useWatch({ control: form.control, name: 'has_tail_image' }) ?? false
  const articleWithCover = useWatch({ control: form.control, name: 'article_with_cover' }) ?? true
  const articleWithContentImages = useWatch({ control: form.control, name: 'article_with_content_images' }) ?? true
  const watchedSelectedModules = useWatch({ control: form.control, name: 'selected_modules' })
  const watchedAgentInput = useWatch({ control: form.control, name: 'agent_input' }) ?? {}
  const watchedProductPhotos = useWatch({ control: form.control, name: 'product_photos' })
  const isMontageTask = watchedType === 'montage'
  const isViralAnalysisTask = watchedType === 'viral_analysis'
  const hasIncompatibleSeednoteAttachments = watchedType === 'seednote'
    && attachmentController.attachments.some((attachment) => attachment.type !== 'image')
  const projectMap = useMemo(() => new Map(projects.map((project) => [project.id, project])), [projects])
  const availableProjects = useMemo(
    () => isViralAnalysisTask ? projects.filter((project) => project.platform === 'seednote') : projects,
    [isViralAnalysisTask, projects],
	)
	const selectedProject = projectMap.get(watchedProjectId ?? '')
	const selectedAgentPack = useMemo(
		() => agentPacksQuery.data?.packs.find((pack) => pack.bindings.task_types?.includes(watchedType)),
		[agentPacksQuery.data, watchedType],
	)
	const imageCapabilityOptionsForValue = useMemo(() => {
		if (!watchedImageCapabilityKey || imageCapabilityOptions.some((option) => option.key === watchedImageCapabilityKey)) {
			return imageCapabilityOptions
		}
    return [
      ...imageCapabilityOptions,
      {
        key: watchedImageCapabilityKey,
        display_name: '已停用图像能力（当前任务配置）',
        enabled: false,
        features: {
          quality_levels: [], size_presets: [], default_size: '', max_batch: 1,
          max_reference_images: 0, supports_reference: false, supports_mask: false,
          output_formats: [], has_background: false, has_compression: false, watermark: false,
        },
      },
    ]
  }, [imageCapabilityOptions, watchedImageCapabilityKey])
  const effectiveImageCapabilityKey = watchedImageCapabilityKey || defaultCapability
  const selectedImageCapability = imageCapabilityOptions.find((option) => option.key === effectiveImageCapabilityKey)
  const imageCapabilityUnavailable = !isViralAnalysisTask && Boolean(effectiveImageCapabilityKey)
    && !imageCapabilitiesLoading
    && !imageCapabilitiesError
    && (
      !selectedImageCapability
      || selectedImageCapability.enabled !== true
      || selectedImageCapability.price_available !== true
    )
  const imageRatioUnsupported = Boolean(
    watchedImageRatio
    && selectedImageCapability?.features?.size_presets
    && !selectedImageCapability.features.size_presets.includes(watchedImageRatio)
  )

  useEffect(() => {
    if (imageRatioUnsupported) {
      form.setError('image_ratio', { type: 'validate', message: '当前图像能力不支持此比例' })
      return
    }
    form.clearErrors('image_ratio')
  }, [form, imageRatioUnsupported])
  useFormDirtyCheck(form, open)

  useEffect(() => {
    if (!open) {
      initializedKeyRef.current = undefined
      return
    }
    if (mode === 'clone' && !sourceTask) return
    if ((mode === 'clone' || initialProjectId) && projectsLoading) return

    const initializationKey = mode === 'clone'
      ? `clone:${sourceTask?.id ?? ''}:${sourceTask?.project_id ?? ''}:${sourceTask?.type ?? ''}:${sourceTask?.execution_profile ?? ''}`
      : `create:${initialProjectId ?? ''}:${initialType ?? ''}`
    if (initializedKeyRef.current === initializationKey) return

    const project = initialProjectId ? projectMap.get(initialProjectId) : undefined
    const defaults = mode === 'clone' && sourceTask
      ? (() => {
          const cloned = cloneTaskFormDefaults(sourceTask)
          const sourceProject = projectMap.get(sourceTask.project_id ?? '')
          return sourceProject
            ? cloned.type === sourceProject.platform
              ? { ...cloned, project_id: sourceProject.id }
              : switchTaskFormDefaults(cloned, sourceProject)
            : { ...cloned, project_id: '' }
        })()
      : createInitialDefaults(project, initialType)
    initializedKeyRef.current = initializationKey
    form.reset(defaults)
    resetAttachments(defaults.input_attachments)
    setMontageUploading(false)
    setShowDirtyDialog(false)
    const focusTimeout = setTimeout(() => form.setFocus('prompt'), 100)
    return () => clearTimeout(focusTimeout)
  }, [form, initialProjectId, initialType, mode, open, projectMap, projectsLoading, resetAttachments, sourceTask])

  const defaultExecutionProfile = cheapestAvailableExecutionProfile(
    executionProfilesQuery.data,
    billingCatalog,
    watchedType,
  )

  useEffect(() => {
    if (!open || form.getValues('execution_profile') || !defaultExecutionProfile) return
    form.setValue('execution_profile', defaultExecutionProfile, { shouldValidate: true })
  }, [defaultExecutionProfile, form, open])

  const taskMutation = useMutation({
    mutationFn: ({ request }: { request: CreateTaskRequest; quantity: number }) => {
      if (mode === 'clone') {
        if (!sourceTask) return Promise.reject(new Error('缺少要克隆的源任务'))
        return api.tasks.clone(sourceTask.id, request)
      }
      return api.tasks.create(request)
    },
    onSuccess: (task, variables) => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      if (shouldHandleResult && !shouldHandleResult()) return
      toast.success(mode === 'clone'
        ? variables.quantity > 1 ? `已克隆 ${variables.quantity} 个任务` : '任务克隆成功'
        : '任务创建成功')
      onCreated(task, variables.quantity)
      form.reset(createTaskFormDefaults())
      attachmentController.clear()
      setShowDirtyDialog(false)
      onOpenChange(false)
    },
    onError: (error) => {
      if (shouldHandleResult && !shouldHandleResult()) return
      toast.error(getApiErrorMessage(error, mode === 'clone' ? '克隆任务失败，请稍后再试' : '创建任务失败，请重试'))
    },
  })
  const isSubmitting = form.formState.isSubmitting || taskMutation.isPending

  function setFormValue<K extends FieldPath<TaskFormDefaults>>(name: K, value: FieldPathValue<TaskFormDefaults, K>) {
    form.setValue(name, value, { shouldDirty: true, shouldValidate: true })
  }

  function resetAndClose() {
    if (isSubmitting) return
    initializedKeyRef.current = undefined
    form.reset(createTaskFormDefaults())
    attachmentController.clear()
    setMontageUploading(false)
    setShowDirtyDialog(false)
    onOpenChange(false)
  }

  function requestClose() {
    if (isSubmitting) return
    if (form.formState.isDirty) {
      setShowDirtyDialog(true)
      return
    }
    resetAndClose()
  }

  function changeProject(id: string | null) {
    setMontageUploading(false)
    const project = id ? projectMap.get(id) : undefined
    const current = {
      ...form.getValues(),
      input_attachments: attachmentController.toInputAttachments(),
    }
    const switched = switchTaskFormDefaults(current, project)
    form.reset(switched, { keepDefaultValues: true })
  }

  function setModuleQuantity(key: string, nextQuantity: number) {
    const current = form.getValues('selected_modules') ?? {}
    const next = { ...current }
    if (nextQuantity >= 1) next[key] = nextQuantity
    else delete next[key]
    setFormValue('selected_modules', next)
  }

  async function onSubmit(values: TaskFormDefaults) {
    const submittedValues: TaskFormDefaults = {
      ...values,
      input_attachments: attachmentController.toInputAttachments(),
    }
    const request = taskFormValuesToRequest(submittedValues)
    await submit(() => taskMutation.mutateAsync({ request, quantity: submittedValues.quantity })).catch(() => {})
  }

  function handleSubmit(event?: BaseSyntheticEvent) {
    if (creationBlocker || hasIncompatibleSeednoteAttachments || attachmentController.uploading || attachmentController.hasFailures || (isMontageTask && montageUploading)) {
      event?.preventDefault()
      return
    }
    return form.handleSubmit(onSubmit)(event)
  }

  const costPreview = taskCreationCostPreview({
    catalog: billingCatalog,
    type: watchedType,
    quantity,
    balance: billingWallet?.balance ?? 0,
    executionProfile: watchedExecutionProfile || undefined,
  })
  const selectedExecutionProfile = executionProfilesQuery.data?.find((profile) => profile.id === watchedExecutionProfile)
  const selectedExecutionProfileUnavailable = Boolean(watchedExecutionProfile)
    && !executionProfilesQuery.isLoading
    && selectedExecutionProfile?.available !== true
  const creationBlocker = executionProfilesQuery.isError
    ? { message: '执行配置暂时无法加载，请稍后重试。', href: '' }
    : !watchedExecutionProfile
      ? { message: '请选择可用的执行配置。', href: '' }
      : selectedExecutionProfileUnavailable
        ? { message: '当前执行配置不可用，请重新选择。', href: '' }
      : !costPreview.priceAvailable
    ? { message: '固定价格目录暂不可用，请稍后重试。', href: '' }
    : mode === 'clone' && projectsLoading
      ? { message: '正在加载可用项目，请稍候。', href: '' }
      : mode === 'clone' && !selectedProject
        ? { message: '源任务项目不可用，请选择一个有效项目。', href: '' }
        : hasIncompatibleSeednoteAttachments
          ? { message: SEEDNOTE_ATTACHMENT_BLOCKER, href: '' }
          : (billingWallet?.debt ?? 0) > 0 || costPreview.insufficient
              ? { message: '积分不足或存在欠费，充值后再创建。', href: '/billing' }
              : imageCapabilityUnavailable
                ? { message: '当前图像能力不可用，请重新选择。', href: '' }
                : imageRatioUnsupported
                  ? { message: '当前图像能力不支持所选比例，请重新选择比例或智能适配。', href: '' }
                : watchedType !== 'ecommerce' && goalMode && !goal.trim()
                  ? { message: '强目标模式需要填写目标条件。', href: '' }
                  : watchedType === 'ecommerce' && (!watchedProductPhotos || watchedProductPhotos.length === 0)
                    ? { message: '电商出图需要先上传产品图。', href: '' }
                    : null

  const projectContextBar = (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <ProjectContextControl
        mode="select"
        projects={availableProjects}
        value={watchedProjectId || null}
        allowNoProject={false}
        loading={projectsLoading}
        placeholder="选择项目"
        createProjectHref={projectsReturnHref({ type: isViralAnalysisTask ? 'seednote' : watchedType, intent: 'new' })}
        onValueChange={changeProject}
      />
      <Badge variant="secondary">{contentTypeLabel[watchedType] || watchedType}</Badge>
    </div>
  )

  const promptComposer = isViralAnalysisTask ? (
    <div data-slot="viral-analysis-prompt" className="space-y-2">
      {projectContextBar}
      <Textarea
        aria-label="源笔记链接或分享文本"
        value={watchedPrompt}
        onChange={(event) => setFormValue('prompt', event.target.value)}
        placeholder="粘贴种草笔记链接或分享文本..."
        className="min-h-32 resize-y"
      />
    </div>
  ) : (
    <AgentPromptInput
      value={{ prompt: watchedPrompt, attachments: attachmentController.attachments }}
      onChange={(value) => {
        setFormValue('prompt', value.prompt)
        if (watchedType === 'montage') {
          form.setValue('montage_input.brief', value.prompt, { shouldDirty: true, shouldValidate: true })
        }
      }}
      onSubmit={() => handleSubmit()}
      attachmentController={attachmentController}
      attachmentPolicy={attachmentPolicy}
      submitMode="external"
      placeholder="描述创作目标、内容要求和素材使用方式..."
      submitLabel={mode === 'clone' ? '克隆任务' : '创建任务'}
      submitting={isSubmitting}
      submitDisabled={Boolean(creationBlocker)}
      contextBar={projectContextBar}
    />
  )

  const submitLabel = mode === 'clone' ? '克隆' : quantity > 1 ? `创建 ${quantity} 个任务` : '创建'

  return (
    <>
      <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen && !isSubmitting) requestClose() }}>
        <DialogContent closeButtonDisabled={isSubmitting} className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-5xl">
          <DialogHeader className="border-b border-border px-4 py-3">
            <DialogTitle>{mode === 'clone' ? '克隆任务' : '新建任务'}</DialogTitle>
            <DialogDescription>
              {mode === 'clone' ? '编辑完整配置并创建一份新任务。' : '配置内容目标、图片选项和执行方式。'}
            </DialogDescription>
          </DialogHeader>
          <Form {...form}>
            <form id="task-create-form" onSubmit={handleSubmit} className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-4">
              {selectedProject && watchedType !== 'viral_analysis' ? (
                <div className="space-y-1 rounded-lg border border-dashed border-border bg-muted/30 p-3">
                  <p className="text-xs font-medium text-foreground/80">将使用项目「{selectedProject.name}」的快照</p>
                  <p className="text-xs text-muted-foreground">
                    {isMontageTask ? (
                      <>项目定位 {selectedProject.instructions || selectedProject.positioning || '—'}</>
                    ) : (
                      <>视觉风格 {selectedProject.visual_style || '—'}</>
                    )}
                    {watchedType === 'article' ? (
                      <> · 署名 {selectedProject.author || '—'} · 写作风格 {selectedProject.writer || '—'} · 排版 {selectedProject.theme || '默认'}</>
                    ) : null}
                  </p>
                  <p className="text-[11px] text-muted-foreground/80">创建后项目再修改，不会影响这个任务。</p>
                </div>
              ) : null}

              <FormField control={form.control} name="execution_profile" render={({ field }) => (
                <FormItem>
                  <FormLabel>执行配置</FormLabel>
                  <FormControl>
                    <ExecutionProfileSelector
                      profiles={executionProfilesQuery.data ?? []}
                      value={field.value}
                      onChange={field.onChange}
                      loading={executionProfilesQuery.isLoading}
                      catalog={billingCatalog}
                      taskType={watchedType}
                      priceUnit="task"
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <TaskTimePricingNotice
                catalog={billingCatalog}
                taskType={watchedType}
                executionProfile={watchedExecutionProfile || undefined}
              />

              <div className="pt-1">
                <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">目标/提示词</p>
                {!isMontageTask ? promptComposer : null}
                {hasIncompatibleSeednoteAttachments ? (
                  <p role="alert" className="mt-2 text-sm font-medium text-red-500">{SEEDNOTE_ATTACHMENT_BLOCKER}</p>
                ) : null}
              </div>

              <div className="space-y-4 pt-1">
                {!isViralAnalysisTask ? <p className="text-xs font-medium uppercase text-muted-foreground">图片/高级</p> : null}

                {watchedType !== 'ecommerce' && !isMontageTask && !isViralAnalysisTask ? (
                  <div className="space-y-2">
                    <FormLabel>数量</FormLabel>
                    <div className="flex gap-2">
                      {[1, 2, 3, 4, 5].map((nextQuantity) => (
                        <Button
                          key={nextQuantity}
                          type="button"
                          variant={quantity === nextQuantity ? 'default' : 'outline'}
                          size="sm"
                          onClick={() => setFormValue('quantity', nextQuantity)}
                        >
                          {nextQuantity}
                        </Button>
                      ))}
                    </div>
                  </div>
                ) : null}

                {!isMontageTask && !isViralAnalysisTask ? (
                  <div className="grid gap-4 md:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
                    <FormField control={form.control} name="image_ratio" render={({ field }) => (
                        <FormItem>
                          <FormLabel>图像比例</FormLabel>
                          <FormControl>
                            <ImageAspectRatioField
                              value={field.value || ''}
                              defaultValue={selectedProject?.image_ratio || ''}
                              supportedSizes={selectedImageCapability?.features?.size_presets}
                              onChange={field.onChange}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )} />
                    <FormField control={form.control} name="image_capability_key" render={({ field }) => (
                      <FormItem>
                        <FormLabel>图像能力</FormLabel>
                        <FormControl>
                          {imageCapabilitiesLoading ? (
                            <Skeleton className="h-10 w-full rounded-xl" />
                          ) : (
                            <ImageCapabilitySelector options={imageCapabilityOptionsForValue} value={field.value || defaultCapability} onChange={field.onChange} />
                          )}
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                  </div>
                ) : null}

                {isMontageTask ? (
                  <MontageCreationPanel
                    form={form}
                    fieldRoot="montage_input"
                    onUploadingChange={setMontageUploading}
                    briefField={promptComposer}
                  />
                ) : null}

                {!isMontageTask && !isViralAnalysisTask ? (
                  <button
                    type="button"
                    onClick={() => setFormValue('watermark', !watermark)}
                    className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${watermark ? 'border-primary bg-primary/5' : 'border-border hover:border-foreground/20'}`}
                  >
                    <Stamp className={`mt-0.5 h-5 w-5 shrink-0 ${watermark ? 'text-primary' : 'text-muted-foreground'}`} />
                    <div className="min-w-0">
                      <p className={`text-sm font-medium ${watermark ? 'text-foreground' : 'text-muted-foreground'}`}>水印</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">仅在所选图像能力支持水印时生效</p>
                    </div>
                  </button>
                ) : null}

                {watchedType === 'seednote' ? (
                  <div className="rounded-lg border border-border p-3">
                    <div className="flex items-start gap-3">
                      <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-foreground">图片构成</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">封面始终生成；勾选要额外生成的图。</p>
                      </div>
                    </div>
                    <div className="mt-3 divide-y divide-border">
                      <div className="flex items-center justify-between py-2">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium text-foreground">封面图</span>
                          <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">必选</span>
                        </div>
                        <Switch checked disabled />
                      </div>
                      <div className="flex items-center justify-between py-2">
                        <div className="min-w-0">
                          <p className="text-sm font-medium text-foreground">内容图</p>
                          <p className="mt-0.5 text-xs text-muted-foreground">承载 2-4 个信息点（image_01.png）</p>
                        </div>
                        <Switch checked={hasContentImage} onCheckedChange={(checked) => setFormValue('has_content_image', checked)} />
                      </div>
                      <div className="flex items-center justify-between py-2">
                        <div className="min-w-0">
                          <p className="text-sm font-medium text-foreground">尾图</p>
                          <p className="mt-0.5 text-xs text-muted-foreground">行动召唤 / 关注引导（tail.png）</p>
                        </div>
                        <Switch checked={hasTailImage} onCheckedChange={(checked) => setFormValue('has_tail_image', checked)} />
                      </div>
                    </div>
                    <p className="mt-2 text-xs text-muted-foreground">当前将生成 {1 + (hasContentImage ? 1 : 0) + (hasTailImage ? 1 : 0)} 张图片</p>
                  </div>
                ) : null}

                {watchedType === 'article' ? (
                  <div className="rounded-lg border border-border p-3">
                    <div className="flex items-start gap-3">
                      <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-foreground">图片构成</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">独立选择是否生成封面与正文配图。两者都关 = 纯文字文章。</p>
                      </div>
                    </div>
                    <div className="mt-3 divide-y divide-border">
                      <div className="flex items-center justify-between py-2">
                        <div className="min-w-0">
                          <p className="text-sm font-medium text-foreground">封面图</p>
                          <p className="mt-0.5 text-xs text-muted-foreground">公众号头图（900×383，作为发布草稿封面）</p>
                        </div>
                        <Switch checked={articleWithCover} onCheckedChange={(checked) => setFormValue('article_with_cover', checked)} />
                      </div>
                      <div className="flex items-center justify-between py-2">
                        <div className="min-w-0">
                          <p className="text-sm font-medium text-foreground">正文配图</p>
                          <p className="mt-0.5 text-xs text-muted-foreground">按排版节奏插入的章节插图</p>
                        </div>
                        <Switch checked={articleWithContentImages} onCheckedChange={(checked) => setFormValue('article_with_content_images', checked)} />
                      </div>
                    </div>
                    <p className="mt-2 text-xs text-muted-foreground">
                      {articleWithCover && articleWithContentImages
                        ? '将生成封面 + 正文配图（默认）'
                        : articleWithCover
                          ? '仅生成封面图，不生成正文配图'
                          : articleWithContentImages
                            ? '仅生成正文配图；发布草稿不设封面'
                            : '纯文字文章，不生成任何图片；发布草稿不设封面'}
                    </p>
                  </div>
                ) : null}

                {watchedType === 'ecommerce' ? (
                  <div className="space-y-4 rounded-lg border border-border p-3">
                    <div className="flex items-start gap-3">
                      <Package className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-foreground">电商素材包</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">上传产品图，选择交付模块。创建只扣基础任务费，后续图片生成和理解按实际用量结算。</p>
                      </div>
                    </div>
                    <FormField control={form.control} name="product_photos" render={({ field }) => (
                      <FormItem>
                        <FormLabel>产品图（必填）</FormLabel>
                        <FormControl>
                          <MultiImageUpload value={field.value ?? []} onChange={field.onChange} purpose="reference" max={16} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                    <div className="space-y-2">
                      <FormLabel>交付模块（至少选一项）</FormLabel>
                      <div className="divide-y divide-border">
                        {ecommerceModuleCatalog.map((module) => {
                          const moduleQuantity = watchedSelectedModules?.[module.key] ?? 0
                          const enabled = moduleQuantity >= 1
                          return (
                            <div key={module.key} className="flex items-center justify-between gap-3 py-2">
                              <div className="min-w-0 flex-1">
                                <div className="flex flex-wrap items-center gap-2">
                                  <p className="text-sm font-medium text-foreground">{module.label}</p>
                                  <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{module.ratio}</span>
                                </div>
                                <p className="mt-0.5 text-xs text-muted-foreground">{module.hint}</p>
                              </div>
                              <div className="flex items-center gap-2">
                                {enabled ? (
                                  <div className="flex items-center gap-1">
                                    <Button type="button" variant="outline" size="sm" className="h-7 w-7 p-0" onClick={() => setModuleQuantity(module.key, Math.max(module.minQty, moduleQuantity - module.qtyStep))} aria-label="减少">
                                      <Minus className="h-3 w-3" />
                                    </Button>
                                    <span className="w-8 text-center text-sm tabular-nums">{moduleQuantity}{module.qtyLabel}</span>
                                    <Button type="button" variant="outline" size="sm" className="h-7 w-7 p-0" onClick={() => setModuleQuantity(module.key, Math.min(module.maxQty, moduleQuantity + module.qtyStep))} aria-label="增加">
                                      <Plus className="h-3 w-3" />
                                    </Button>
                                  </div>
                                ) : null}
                                <Switch checked={enabled} onCheckedChange={(checked) => setModuleQuantity(module.key, checked ? module.defaultQty : 0)} aria-label={`启用 ${module.label}`} />
                              </div>
                            </div>
                          )
                        })}
                      </div>
                      {(!watchedSelectedModules || Object.values(watchedSelectedModules).every((value) => !value || value < 1)) ? (
                        <p className="text-xs text-destructive">请至少选择一个交付模块</p>
                      ) : null}
                    </div>
                    <FormField control={form.control} name="target_platform" render={({ field }) => (
                      <FormItem>
                        <FormLabel>目标平台</FormLabel>
                        <Select value={field.value || undefined} onValueChange={field.onChange}>
                          <FormControl><SelectTrigger className="w-full"><SelectValue placeholder="选择投放平台（影响尺寸与合规规范）" /></SelectTrigger></FormControl>
                          <SelectContent>
                            {ecommerceTargetPlatformOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )} />
                    <FormField control={form.control} name="selling_points" render={({ field }) => (
                      <FormItem>
                        <FormLabel>核心卖点（可选）</FormLabel>
                        <FormControl><Textarea placeholder="列出产品核心卖点（材质 / 功能 / 使用场景 / 价格优势等）。留空则由 AI 从产品图分析提炼" className="min-h-[72px] resize-y" {...field} /></FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                    <FormField control={form.control} name="language" render={({ field }) => (
                      <FormItem>
                        <FormLabel>语言</FormLabel>
                        <Select value={field.value || undefined} onValueChange={field.onChange}>
                          <FormControl><SelectTrigger className="w-full"><SelectValue placeholder="中文" /></SelectTrigger></FormControl>
                          <SelectContent>
                            {ecommerceLanguageOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )} />
                  </div>
                ) : null}

                <AgentPackSchemaFields
                  pack={selectedAgentPack}
                  surface="task"
                  value={watchedAgentInput}
                  onChange={(value) => setFormValue('agent_input', value)}
                />

                {watchedType !== 'ecommerce' && !isMontageTask && !isViralAnalysisTask ? (
                  <div className={`rounded-lg border p-3 transition-colors ${goalMode ? 'border-primary bg-primary/5' : 'border-border'}`}>
                    <div className="flex w-full items-start gap-3">
                      <label htmlFor="task-goal-mode" className="flex min-w-0 flex-1 cursor-pointer items-start gap-3 text-left">
                        <Target className={`mt-0.5 h-5 w-5 shrink-0 ${goalMode ? 'text-primary' : 'text-muted-foreground'}`} />
                        <span className="min-w-0 flex-1">
                          <span className={`block text-sm font-medium ${goalMode ? 'text-foreground' : 'text-muted-foreground'}`}>强目标模式</span>
                          <span className="mt-0.5 block text-xs text-muted-foreground">开启后扣费 ×3，最多尝试 3 次。任务执行后由 AI 评估产出是否满足「目标条件」，未达成自动重试。</span>
                        </span>
                      </label>
                      <Switch id="task-goal-mode" checked={goalMode} onCheckedChange={(checked) => setFormValue('goal_mode', checked)} />
                    </div>
                    {goalMode ? (
                      <div className="mt-3 space-y-2">
                        <Textarea
                          value={goal}
                          onChange={(event) => setFormValue('goal', event.target.value)}
                          placeholder="例：文章字数 ≥ 1500 字；必须包含 3 个真实案例；开头必须设置钩子；种草笔记必须包含具体使用感受…"
                          className="min-h-[80px] resize-y text-sm"
                          maxLength={4000}
                        />
                        <div className="flex flex-wrap gap-1.5">
                          {['文章字数 ≥ 1500 字', '必须包含 3 个真实案例', '开头必须设置钩子，吸引读者继续阅读', '必须包含数据或引用来源'].map((example) => (
                            <button key={example} type="button" onClick={() => setFormValue('goal', example)} className="rounded-full border border-border bg-background px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground">
                              {example}
                            </button>
                          ))}
                        </div>
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>

              <div className="pt-1">
                <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">固定价格</p>
                <div className="space-y-1 rounded-md border border-border bg-muted/50 p-3 text-sm">
                  {costPreview.priceAvailable ? (
                    <div className="space-y-1 text-muted-foreground">
                      <p>{billingCatalog?.pricing_tier ? `${billingCatalog.pricing_tier === 'enterprise' ? '企业版' : billingCatalog.pricing_tier === 'pro' ? '专业版' : '免费版'}任务价` : '固定任务价'}：{costPreview.baseCost.toLocaleString()} × {costPreview.billableQuantity} = <span className="font-medium text-foreground">{costPreview.totalCost.toLocaleString()}</span> 积分</p>
                      {costPreview.discountPerTask > 0 ? (
                        <p className="text-xs">标准价 <span className="line-through">{costPreview.listBaseCost.toLocaleString()}</span> · 每个任务优惠 {costPreview.discountPerTask.toLocaleString()} 积分</p>
                      ) : null}
                    </div>
                  ) : <p className="font-medium text-red-500">固定任务价暂不可用</p>}
                  <p className="text-xs text-muted-foreground">云端 Agent 运行成本由平台承担，不额外预留或补扣。</p>
                  <p className="text-xs text-muted-foreground">任务内成功交付的图片、视频等增值操作按开始前确认的固定 SKU 另行记账。</p>
                  {costPreview.priceAvailable ? (
                    <p className="text-muted-foreground">余额：{(billingWallet?.balance ?? 0).toLocaleString()} → <span className={`font-medium ${costPreview.remaining < 0 ? 'text-red-500' : 'text-foreground'}`}>{costPreview.remaining.toLocaleString()}</span></p>
                  ) : null}
                  {creationBlocker && !hasIncompatibleSeednoteAttachments ? (
                    <p className="text-sm font-medium text-red-500">{creationBlocker.href ? <Link to={creationBlocker.href}>{creationBlocker.message}</Link> : creationBlocker.message}</p>
                  ) : null}
                </div>
              </div>
            </form>
          </Form>
          <DialogFooter className="mx-0 mb-0 border-t border-border bg-popover px-4 py-3 sm:flex-row sm:items-center sm:justify-end">
            <Button variant="secondary" disabled={isSubmitting} onClick={requestClose}>取消</Button>
            <Button
              type="submit"
              form="task-create-form"
              loading={isSubmitting}
              disabled={isSubmitting || Boolean(creationBlocker) || attachmentController.uploading || attachmentController.hasFailures || (isMontageTask && montageUploading)}
            >
              {submitLabel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={showDirtyDialog} onOpenChange={(nextOpen) => { if (!isSubmitting) setShowDirtyDialog(nextOpen) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃编辑？</AlertDialogTitle>
            <AlertDialogDescription>你有未保存的更改，确定要关闭吗？</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isSubmitting}>继续编辑</AlertDialogCancel>
            <AlertDialogAction disabled={isSubmitting} onClick={resetAndClose}>放弃</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
