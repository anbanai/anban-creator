import { useCallback, useEffect, useMemo, useRef, useState, type BaseSyntheticEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  Inbox,
  Pause,
  Play,
  Plus,
  Sparkles,
  Trash2,
} from 'lucide-react'

import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { AgentExecutionProfileID, AgentPack, Plan, CreatePlanRequest, UpdatePlanRequest } from '@/types'
import { planSchema, type PlanFormValues, normalizeImageRatio } from '@/lib/schemas'
import { useAgentPacks } from '@/hooks/useAgentPacks'
import { useAgentExecutionProfiles } from '@/hooks/useAgentExecutionProfiles'
import { useImageCapabilities } from '@/hooks/useImageCapabilities'
import { parseCreationIntent } from '@/lib/command-center'
import { cheapestAvailableExecutionProfileForTasks } from '@/lib/pricing'
import { prepareReusableInputAttachments } from '@/lib/input-attachment-submit'
import type { ReferenceImageValue } from '@/types/asset'
import { planStatusLabel, cronToHuman, formatDateTimeCN, getBadgeVariant } from '@/lib/labels'

import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import QueryErrorState from '@/components/QueryErrorState'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { AgentIconStack } from '@/components/agent-prompt/AgentIconStack'
import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { CreationTypePicker } from '@/components/tasks/CreationTypePicker'
import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import SchedulePicker from '@/components/SchedulePicker'
import { TaskComposerParameters } from '@/components/tasks/TaskComposerParameters'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'

const DEFAULT_CRON = '0 9 * * 1,3,5'
const PLAN_ATTACHMENT_POLICY = { ...GENERAL_AGENT_ATTACHMENT_POLICY, maxCount: 16 }

type TaskPack = AgentPack & { channel: string }
type OutputPack = TaskPack & { plan_task_kind: string }

const outputLabels: Record<string, string> = {
  'wechat-article': '公众号文章',
  seednote: '种草笔记',
  'wechat-picture': '公众号贴图',
}

function canUseForTask(pack: AgentPack): pack is TaskPack {
  return pack.kind === 'managed'
    && pack.surfaces.includes('task')
    && typeof pack.channel === 'string'
    && pack.channel.trim() !== ''
    && (pack.bindings.task_kinds ?? []).length > 0
}

function canUseForPlan(pack: TaskPack): pack is OutputPack {
  return pack.kind === 'managed'
    && pack.surfaces.includes('plan')
    && typeof pack.plan_task_kind === 'string'
    && pack.plan_task_kind.trim() !== ''
    && (pack.bindings.task_kinds ?? []).includes(pack.plan_task_kind)
}

function outputLabel(pack: TaskPack) {
  return outputLabels[pack.id] ?? pack.display_name.replace(/^微信公众号/, '公众号')
}

function cronWithTime(cron: string, time: string) {
  const parts = cron.trim().split(/\s+/)
  const [hour, minute] = time.split(':')
  if (parts.length !== 5 || hour === undefined || minute === undefined) return cron
  parts[0] = String(Number(minute))
  parts[1] = String(Number(hour))
  return parts.join(' ')
}

function formValuesForPlan(plan: Plan): PlanFormValues {
  return {
    project_id: plan.project_id,
    agent_ids: plan.agent_ids ?? plan.entries?.map((entry) => entry.agent_id) ?? [],
    execution_profile: plan.execution_profile,
    cron_expr: plan.cron_expr,
    prompt: plan.prompt || '',
    image_capability_key: plan.image_capability_key || '',
    image_ratio: normalizeImageRatio(plan.image_ratio),
    skip_reference_image: plan.skip_reference_image ?? false,
    reference_image: plan.reference_image ?? null,
    input_attachments: plan.input_attachments ?? [],
    watermark: plan.watermark ?? false,
  }
}

function defaultValues(projectID = ''): PlanFormValues {
  return {
    project_id: projectID,
    agent_ids: [],
    execution_profile: '',
    cron_expr: DEFAULT_CRON,
    prompt: '',
    image_capability_key: '',
    image_ratio: 'auto',
    skip_reference_image: false,
    reference_image: null,
    input_attachments: [],
    watermark: false,
  }
}

export default function PlansPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const createIntent = parseCreationIntent(searchParams)
  const queryClient = useQueryClient()
  const { submit } = useSubmitLock()
  const [projectFilter, setProjectFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [page, setPage] = useState(1)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [scheduleValid, setScheduleValid] = useState(true)
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [recommendationUnavailable, setRecommendationUnavailable] = useState(false)
  const recommendationRequestRef = useRef(0)
  const scheduleManuallyChangedRef = useRef(false)

  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planSchema) as Resolver<PlanFormValues>,
    defaultValues: defaultValues(),
  })
  useFormDirtyCheck(form, modalOpen)
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'ai_entry_attachment' },
    policy: PLAN_ATTACHMENT_POLICY,
    onAttachmentsChange: () => form.setValue('input_attachments', attachmentController.toInputAttachments(), { shouldDirty: true, shouldValidate: true }),
  })
  const resetAttachments = attachmentController.reset
  const attachmentUploading = attachmentController.uploading
  const attachmentFailed = attachmentController.hasFailures


  const watchedProjectID = useWatch({ control: form.control, name: 'project_id' })
  const watchedAgentIDs = useWatch({ control: form.control, name: 'agent_ids' }) ?? []
  const watchedExecutionProfile = useWatch({ control: form.control, name: 'execution_profile' })
  const watchedImageCapabilityKey = useWatch({ control: form.control, name: 'image_capability_key' }) ?? ''
  const watchedImageRatio = useWatch({ control: form.control, name: 'image_ratio' }) ?? 'auto'
  const watchedSkipReference = useWatch({ control: form.control, name: 'skip_reference_image' }) ?? false

  const packsQuery = useAgentPacks()
  const taskPacks = useMemo(() => (packsQuery.data?.packs ?? []).filter(canUseForTask), [packsQuery.data?.packs])
  const outputPacks = useMemo(() => taskPacks.filter(canUseForPlan), [taskPacks])
  const { data: platformConfigs = [] } = useQuery({
    queryKey: ['platform-configs'],
    queryFn: () => api.projects.platformConfigs(),
    staleTime: Infinity,
  })
  const sharedImageRatios = useMemo(() => {
    const ratiosByOutput = watchedAgentIDs.map((id) => {
      const channel = taskPacks.find((pack) => pack.id === id)?.channel
      const platform = channel === 'wechat-article' || channel === 'wechat-picture' ? 'wechat' : channel
      return platformConfigs.find((config) => config.id === platform)?.supported_image_ratios ?? []
    })
    return (ratiosByOutput[0] ?? []).filter((ratio) => ratiosByOutput.every((ratios) => ratios.includes(ratio)))
  }, [platformConfigs, taskPacks, watchedAgentIDs])
  useEffect(() => {
    if (!modalOpen || platformConfigs.length === 0 || watchedAgentIDs.length === 0
      || watchedAgentIDs.some((id) => !outputPacks.some((pack) => pack.id === id))) return
    if (watchedImageRatio !== 'auto' && !sharedImageRatios.includes(watchedImageRatio)) {
      form.setValue('image_ratio', 'auto', { shouldValidate: true })
    }
  }, [form, modalOpen, outputPacks, platformConfigs.length, sharedImageRatios, watchedAgentIDs, watchedImageRatio])
  const executionProfilesQuery = useAgentExecutionProfiles()
  const { items: imageCapabilities, defaultCapability: defaultImageCapability, isLoading: imageCapabilitiesLoading, isError: imageCapabilitiesError } = useImageCapabilities(modalOpen)
  const effectiveImageCapability = watchedImageCapabilityKey || defaultImageCapability || ''

  const { data: projects = [], isLoading: projectsLoading } = useQuery({
    queryKey: ['projects-for-plans'],
    queryFn: () => api.projects.list({ status: 'active' }),
    staleTime: 60_000,
  })
  const projectMap = useMemo(() => Object.fromEntries(projects.map((project) => [project.id, project])), [projects])
  const selectedProject = projectMap[watchedProjectID]

  const { data: planData, isLoading, isError, refetch } = useQuery({
    queryKey: ['plans', projectFilter, page],
    queryFn: () => api.plans.list({ limit: 50, offset: (page - 1) * 50, project_id: projectFilter || undefined }),
  })
  const plans = useMemo(() => {
    const items = planData?.items ?? []
    const query = searchFilter.trim().toLowerCase()
    return query ? items.filter((plan) => `${plan.prompt} ${plan.title}`.toLowerCase().includes(query)) : items
  }, [planData?.items, searchFilter])
  const totalPages = Math.max(1, Math.ceil((planData?.total ?? 0) / 50))

  const executionProfileOptions = executionProfilesQuery.data ?? []
  const recommendedProfile = useMemo(
    () => cheapestAvailableExecutionProfileForTasks(executionProfileOptions, undefined, watchedAgentIDs),
    [executionProfileOptions, watchedAgentIDs],
  )
  const defaultExecutionProfile = recommendedProfile
    ?? executionProfileOptions.find((profile) => profile.available)?.id

  useEffect(() => {
    if (!modalOpen || watchedExecutionProfile || !defaultExecutionProfile) return
    form.setValue('execution_profile', defaultExecutionProfile, { shouldValidate: true })
  }, [defaultExecutionProfile, form, modalOpen, watchedExecutionProfile])

  const createMutation = useMutation({
    mutationFn: (payload: CreatePlanRequest) => api.plans.create(payload),
    onSuccess: () => {
      toast.success('计划创建成功')
      void queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '创建计划失败')),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdatePlanRequest }) => api.plans.update(id, payload),
    onSuccess: () => {
      toast.success('计划更新成功')
      void queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '更新计划失败')),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.plans.delete(id),
    onSuccess: () => {
      toast.success('计划已删除')
      void queryClient.invalidateQueries({ queryKey: ['plans'] })
      setDeleteTarget(null)
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '删除计划失败')),
  })
  const pauseMutation = useMutation({
    mutationFn: (id: string) => api.plans.pause(id),
    onSuccess: () => { toast.success('计划已暂停'); void queryClient.invalidateQueries({ queryKey: ['plans'] }) },
    onError: (error) => toast.error(getApiErrorMessage(error, '暂停计划失败')),
  })
  const resumeMutation = useMutation({
    mutationFn: (id: string) => api.plans.resume(id),
    onSuccess: () => { toast.success('计划已恢复'); void queryClient.invalidateQueries({ queryKey: ['plans'] }) },
    onError: (error) => toast.error(getApiErrorMessage(error, '恢复计划失败')),
  })

  const resetModal = useCallback(() => {
    recommendationRequestRef.current += 1
    setModalOpen(false)
    setEditingPlan(null)
    setShowDirtyDialog(false)
    setScheduleValid(true)
    setRecommendationUnavailable(false)
    resetAttachments([])
    setReferenceUploading(false)
    form.reset(defaultValues())
  }, [form, resetAttachments])

  const openCreate = useCallback(() => {
    const projectID = createIntent.projectId && projectMap[createIntent.projectId] ? createIntent.projectId : ''
    setEditingPlan(null)
    setRecommendationUnavailable(false)
    scheduleManuallyChangedRef.current = false
    resetAttachments([])
    form.reset(defaultValues(projectID))
    setModalOpen(true)
    const requestID = ++recommendationRequestRef.current
    void api.plans.scheduleRecommendation().then((recommendation) => {
      if (requestID !== recommendationRequestRef.current || scheduleManuallyChangedRef.current) return
      form.setValue('cron_expr', cronWithTime(form.getValues('cron_expr'), recommendation.time), { shouldDirty: false, shouldValidate: true })
      setRecommendationUnavailable(!recommendation.load_balanced)
    }).catch(() => { if (requestID === recommendationRequestRef.current) setRecommendationUnavailable(true) })
  }, [createIntent.projectId, form, projectMap, resetAttachments])

  useEffect(() => {
    if (!createIntent.shouldCreate || (createIntent.projectId && projects.length === 0)) return
    openCreate()
    setSearchParams({}, { replace: true })
  }, [createIntent.projectId, createIntent.shouldCreate, openCreate, projects.length, setSearchParams])

  function openEdit(plan: Plan) {
    recommendationRequestRef.current += 1
    setEditingPlan(plan)
    setRecommendationUnavailable(false)
    resetAttachments(plan.input_attachments ?? [])
    form.reset(formValuesForPlan(plan))
    setModalOpen(true)
  }

  function closeModal() {
    if (isSubmitting) return
    if (form.formState.isDirty || attachmentUploading || attachmentFailed || referenceUploading) { setShowDirtyDialog(true); return }
    resetModal()
  }

  function changeProject(id: string | null) {
    form.setValue('project_id', id ?? '', { shouldDirty: true, shouldValidate: true })
    // Output selection is independent from the project context.
    if (id) form.setValue('image_ratio', normalizeImageRatio(projectMap[id]?.image_ratio), { shouldDirty: true })
  }

  async function onSubmit(values: PlanFormValues) {
    if (values.agent_ids.length === 0) { form.setError('agent_ids', { type: 'validate', message: '至少选择一种输出类型' }); return }
    if (!outputsAvailable || !selectedProfileAvailable || imageUnavailable || referenceUploading || attachmentUploading || attachmentFailed || !scheduleValid) return
    const prepared = prepareReusableInputAttachments(values.input_attachments ?? [], { allowExternalURLs: true })
    if (prepared.error) { toast.error(prepared.error); return }
    const shared = {
      agent_ids: values.agent_ids,
      execution_profile: values.execution_profile as AgentExecutionProfileID,
      cron_expr: values.cron_expr.trim(),
      prompt: values.prompt?.trim() || undefined,
      image_capability_key: values.image_capability_key || undefined,
      image_ratio: values.image_ratio,
      skip_reference_image: values.skip_reference_image,
      reference_image: values.skip_reference_image ? null : values.reference_image,
      input_attachments: prepared.attachments ?? [],
      watermark: values.watermark,
    }
    if (editingPlan) await submit(async () => updateMutation.mutateAsync({ id: editingPlan.id, payload: shared })).catch(() => {})
    else await submit(async () => createMutation.mutateAsync({ project_id: values.project_id, ...shared })).catch(() => {})
  }

  const handleScheduleChange = useCallback((value: string) => {
    scheduleManuallyChangedRef.current = true
    form.setValue('cron_expr', value, { shouldDirty: true, shouldValidate: true })
  }, [form])
  const handleScheduleInteraction = useCallback(() => {
    scheduleManuallyChangedRef.current = true
  }, [])
  const handleScheduleValidity = useCallback((valid: boolean) => {
    setScheduleValid(valid)
    if (valid) form.clearErrors('cron_expr')
  }, [form])

  function handleSubmit(event?: BaseSyntheticEvent) {
    event?.preventDefault()
    void form.handleSubmit(onSubmit)(event)
  }

  const isSubmitting = form.formState.isSubmitting || createMutation.isPending || updateMutation.isPending
  const selectedImageCapability = imageCapabilities.find((item) => item.key === effectiveImageCapability)
  const imageUnavailable = imageCapabilitiesLoading || imageCapabilitiesError || !effectiveImageCapability
    || !selectedImageCapability || selectedImageCapability.enabled !== true || selectedImageCapability.price_available !== true
  const selectedProfileAvailable = executionProfileOptions.some((profile) => profile.id === watchedExecutionProfile && profile.available)
  const outputsAvailable = watchedAgentIDs.length > 0 && watchedAgentIDs.every((id) => outputPacks.some((pack) => pack.id === id))
  const submitDisabled = isSubmitting || !outputsAvailable || !watchedProjectID || !selectedProfileAvailable || !scheduleValid || referenceUploading || attachmentUploading || attachmentFailed || imageUnavailable


  return (
    <div className="space-y-6">
      <PageHeader title="计划" description="一次排期可同时生成多种输出，每种输出独立执行。">
        <Button onClick={openCreate}><Plus className="h-4 w-4" />新建计划</Button>
      </PageHeader>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <ProjectContextControl
          mode="select"
          projects={projects}
          value={projectFilter || null}
          allowNoProject
          noProjectLabel="全部项目"
          loading={projectsLoading}
          onValueChange={(id) => { setProjectFilter(id ?? ''); setPage(1) }}
          ariaLabel="筛选项目"
          compact
        />
        <Input value={searchFilter} onChange={(event) => setSearchFilter(event.target.value)} placeholder="搜索计划" className="w-full sm:w-64" />
      </div>

      {isError ? <QueryErrorState onRetry={() => void refetch()} /> : isLoading ? (
        <div className="rounded-lg border border-border bg-card p-6 text-sm text-muted-foreground">正在加载计划...</div>
      ) : plans.length === 0 ? (
        <EmptyState icon={Inbox} title="还没有计划" description="创建计划后，系统会按排期自动生成任务。" action={{ label: '新建计划', onClick: openCreate }} />
      ) : (
        <div className="space-y-3">
          {plans.map((plan) => {
            const project = projectMap[plan.project_id]
            const outputIDs = plan.agent_ids ?? plan.entries?.map((entry) => entry.agent_id) ?? []
            return (
              <div key={plan.id} className="rounded-lg border border-border bg-card p-4" data-plan-id={plan.id}>
                <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                  <div className="min-w-0 space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="truncate text-sm font-semibold">{plan.prompt || '定时创作计划'}</h2>
                      <Badge variant={getBadgeVariant(plan.status, 'plan')}>{planStatusLabel[plan.status] ?? plan.status}</Badge>
                    </div>
                    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      {project ? <ProjectIdentity project={project} compact /> : <span>未命名项目</span>}<span aria-hidden="true">·</span><span>{cronToHuman(plan.cron_expr)}</span>
                      {plan.next_run_at ? <><span aria-hidden="true">·</span><span>下次 {formatDateTimeCN(plan.next_run_at)}</span></> : null}
                    </div>
                    <div className="flex items-center gap-1.5" aria-label="输出类型">
                      <span className="sr-only">输出类型：</span>
                      <AgentIconStack agentIds={outputIDs} compact maxVisible={5} />
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Link to={`/tasks?plan_id=${encodeURIComponent(plan.id)}`} className="mr-2 text-sm text-primary hover:underline">查看任务</Link>
                    <Button type="button" size="icon-sm" variant="ghost" aria-label="编辑" onClick={() => openEdit(plan)}><Sparkles className="h-4 w-4" /></Button>
                    {plan.status === 'active' ? <Button type="button" size="icon-sm" variant="ghost" aria-label="暂停" onClick={() => void pauseMutation.mutateAsync(plan.id)}><Pause className="h-4 w-4" /></Button> : <Button type="button" size="icon-sm" variant="ghost" aria-label="恢复" onClick={() => void resumeMutation.mutateAsync(plan.id)}><Play className="h-4 w-4" /></Button>}
                    <Button type="button" size="icon-sm" variant="ghost" aria-label="删除" onClick={() => setDeleteTarget(plan.id)}><Trash2 className="h-4 w-4" /></Button>
                  </div>
                </div>
              </div>
            )
          })}
          {totalPages > 1 ? <div className="flex items-center justify-between pt-2 text-sm text-muted-foreground"><span>第 {page} / {totalPages} 页</span><div className="flex gap-2"><Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((current) => current - 1)}>上一页</Button><Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage((current) => current + 1)}>下一页</Button></div></div> : null}
        </div>
      )}

      <Dialog open={modalOpen} onOpenChange={(open) => { if (!open) closeModal() }}>
        <DialogContent closeButtonDisabled={isSubmitting} className="flex max-h-[90dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
          <DialogHeader className="shrink-0 border-b border-border px-4 py-3"><DialogTitle>{editingPlan ? '编辑计划' : '新建计划'}</DialogTitle><DialogDescription>选择创作类型，设置每次创作的要求和排期。生成的内容可在任务中查看。</DialogDescription></DialogHeader>
          <Form {...form}>
            <form id="plan-form" onSubmit={handleSubmit} className="min-h-0 flex-1 space-y-5 overflow-y-auto px-4 py-4">
              <fieldset disabled={isSubmitting} className="min-w-0 space-y-5">
              <section className="space-y-2">
                <div className="flex items-baseline justify-between gap-3">
                  <div><h3 className="text-sm font-semibold">创作类型</h3><p className="mt-1 text-xs text-muted-foreground">可多选，每次按排期为每种类型创建一个任务。</p></div>
                  <span className="shrink-0 text-xs text-muted-foreground">已选 {watchedAgentIDs.length} 项</span>
                </div>
                <FormField control={form.control} name="agent_ids" render={({ field }) => <FormItem>
                  <CreationTypePicker multiple options={taskPacks.map((pack) => ({
                    id: pack.id,
                    label: outputLabel(pack),
                    description: pack.description,
                    disabled: !canUseForPlan(pack),
                    disabledReason: canUseForPlan(pack) ? undefined : '需要任务级输入，暂不支持定时计划',
                  }))} value={field.value} onChange={field.onChange} disabled={isSubmitting} />
                  {packsQuery.isLoading ? <p className="text-sm text-muted-foreground">正在加载可用输出...</p> : null}
                  <FormMessage />
                  {!packsQuery.isLoading && (packsQuery.isError || outputPacks.length === 0) ? <p role="alert" className="text-sm text-destructive">暂时没有可用于计划的输出类型。</p> : null}
                  {watchedAgentIDs.some((id) => !outputPacks.some((pack) => pack.id === id)) && !packsQuery.isLoading ? <p role="alert" className="text-sm text-destructive">部分创作类型已不可用，请重新选择。<Button type="button" variant="link" size="sm" onClick={() => field.onChange(field.value.filter((id) => outputPacks.some((pack) => pack.id === id)))}>移除不可用类型</Button></p> : null}
                </FormItem>} />
              </section>

              <section className="space-y-2">
                <h3 className="text-sm font-semibold">创作提示</h3>
                <AgentPromptInput
                  value={{ prompt: form.watch('prompt') ?? '', attachments: attachmentController.attachments }}
                  onChange={(value) => form.setValue('prompt', value.prompt, { shouldDirty: true, shouldValidate: true })}
                  onSubmit={() => handleSubmit()}
                  attachmentController={attachmentController}
                  attachmentPolicy={PLAN_ATTACHMENT_POLICY}
                  attachmentPreviewOwner={editingPlan ? { ownerType: 'plan', ownerId: editingPlan.id } : undefined}
                  ariaLabel="创作提示"
                  placeholder="描述每次创作的方向、要求和素材使用方式"
                  submitMode="external"
                  disabled={isSubmitting}
                  leadingTools={(
                    <div className="flex min-w-0 flex-wrap items-center gap-1">
                      {editingPlan ? <ProjectContextControl mode="readonly" project={selectedProject ?? null} compact /> : <ProjectContextControl mode="select" projects={projects} value={watchedProjectID || null} onValueChange={changeProject} loading={projectsLoading} disabled={isSubmitting} allowNoProject={false} placeholder="选择项目" ariaLabel="项目" compact />}
                      <TaskComposerParameters
                        execution={{ profiles: executionProfileOptions, value: watchedExecutionProfile as AgentExecutionProfileID | '', onChange: (value) => form.setValue('execution_profile', value, { shouldDirty: true, shouldValidate: true }), loading: executionProfilesQuery.isLoading, disabled: isSubmitting || executionProfilesQuery.isError }}
                        image={{ ratios: sharedImageRatios, ratio: watchedImageRatio, onRatioChange: (value) => form.setValue('image_ratio', value, { shouldDirty: true }), capabilities: imageCapabilities, capabilityKey: effectiveImageCapability, onCapabilityChange: (value) => form.setValue('image_capability_key', value, { shouldDirty: true, shouldValidate: true }), loading: imageCapabilitiesLoading, disabled: isSubmitting || imageCapabilitiesError }}
                        disabled={isSubmitting}
                      />
                    </div>
                  )}
                  status={<span className="hidden text-xs text-muted-foreground sm:inline">每次创作均使用</span>}
                />
                <p className="text-xs text-muted-foreground">项目提供定位、关键词、视觉风格和作者设置；附件会用于每次创作。</p>
                {form.formState.errors.prompt ? <p role="alert" className="text-sm text-destructive">{form.formState.errors.prompt.message}</p> : null}
                {attachmentUploading || attachmentFailed ? <p role="status" className="text-xs text-destructive">{attachmentUploading ? '素材上传中，请稍候。' : '素材上传失败，请重试或移除后保存。'}</p> : null}
              </section>

              <div className="grid items-start gap-5 lg:grid-cols-2">
              <section className="space-y-2"><h3 className="text-sm font-semibold">排期</h3><FormField control={form.control} name="cron_expr" render={({ field }) => <FormItem><FormControl><SchedulePicker value={field.value} onChange={handleScheduleChange} onInteraction={handleScheduleInteraction} onValidityChange={handleScheduleValidity} /></FormControl>{recommendationUnavailable ? <p className="text-xs text-muted-foreground">智能排期暂不可用，已保留默认时间。</p> : null}<FormMessage /></FormItem>} /></section>

              <div className="space-y-3">
                <h3 className="text-sm font-semibold">执行设置</h3>
                <div className="rounded-lg border border-border p-4">
                  <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                    <dt className="text-muted-foreground">执行配置</dt><dd>{executionProfileOptions.find((profile) => profile.id === watchedExecutionProfile)?.display_name || '请选择'}</dd>
                    <dt className="text-muted-foreground">图片能力</dt><dd>{selectedImageCapability?.display_name || '请选择'}</dd>
                    <dt className="text-muted-foreground">图片比例</dt><dd>{form.watch('image_ratio') === 'auto' ? '智能适配' : form.watch('image_ratio')}</dd>
                  </dl>
                  <p className="mt-3 text-xs text-muted-foreground">可在提示框的「创作参数」中调整，应用于所有选中的创作类型。</p>
                  <details className="mt-4 border-t border-border pt-3">
                    <summary className="cursor-pointer text-sm font-medium">高级设置</summary>
                    <div className="mt-3 space-y-4">
                      <FormField control={form.control} name="skip_reference_image" render={({ field }) => <FormItem className="flex items-center justify-between gap-3 space-y-0"><div><FormLabel>跳过参考图</FormLabel><FormDescription>本次计划不使用参考图。</FormDescription></div><FormControl><Switch checked={!!field.value} onCheckedChange={field.onChange} /></FormControl></FormItem>} />
                      {!watchedSkipReference ? <FormField control={form.control} name="reference_image" render={({ field }) => <FormItem><FormLabel>参考图</FormLabel><FormControl><ReferenceAssetUpload value={(field.value as ReferenceImageValue | null) ?? null} onChange={field.onChange} purpose="task_reference" onUploadingChange={setReferenceUploading} disabled={isSubmitting} /></FormControl><FormMessage /></FormItem>} /> : null}
                      <FormField control={form.control} name="watermark" render={({ field }) => <FormItem className="flex items-center justify-between gap-3 space-y-0"><div><FormLabel>水印</FormLabel><FormDescription>在支持的图片能力中启用水印。</FormDescription></div><FormControl><Switch checked={!!field.value} onCheckedChange={field.onChange} /></FormControl></FormItem>} />
                    </div>
                  </details>
                </div>
                {imageUnavailable && !imageCapabilitiesLoading ? <p role="alert" className="text-sm text-destructive">当前图像能力不可用，请在创作参数中重新选择。</p> : null}
                {!selectedProfileAvailable && !executionProfilesQuery.isLoading ? <p role="alert" className="text-sm text-destructive">当前执行配置不可用，请在创作参数中重新选择。</p> : null}
              </div>
              </div>
              </fieldset>
            </form>
          </Form>
          <DialogFooter className="mx-0 mb-0 shrink-0 border-t border-border bg-popover px-4 py-3 sm:items-center sm:justify-between">
            <p className="text-xs text-muted-foreground">每次生成 {watchedAgentIDs.length} 个任务 · {cronToHuman(form.watch('cron_expr'))} · 北京时间</p>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={closeModal} disabled={isSubmitting}>取消</Button>
              <Button type="submit" form="plan-form" loading={isSubmitting} disabled={submitDisabled}>{editingPlan ? '保存' : '创建'}</Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={showDirtyDialog} onOpenChange={setShowDirtyDialog}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle><AlertDialogDescription>关闭后当前表单内容会丢失。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>继续编辑</AlertDialogCancel><AlertDialogAction onClick={resetModal}>放弃修改</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>删除这个计划？</AlertDialogTitle><AlertDialogDescription>删除后不会再触发新的任务。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction disabled={deleteMutation.isPending} onClick={() => { if (deleteTarget) void deleteMutation.mutateAsync(deleteTarget) }}>删除</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
    </div>
  )
}
