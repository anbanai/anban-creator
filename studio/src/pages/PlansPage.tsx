import { useCallback, useEffect, useMemo, useRef, useState, type BaseSyntheticEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  BookOpen,
  FileImage,
  FileText,
  Inbox,
  Layers3,
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
import type { InputAttachment } from '@/types/input-attachment'
import type { ReferenceImageValue } from '@/types/asset'
import { planStatusLabel, cronToHuman, formatDateTimeCN, getBadgeVariant } from '@/lib/labels'
import { cn } from '@/lib/utils'

import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import QueryErrorState from '@/components/QueryErrorState'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import SchedulePicker from '@/components/SchedulePicker'
import { ImageCapabilitySelector } from '@/components/ImageCapabilitySelector'
import { ImageAspectRatioField } from '@/components/tasks/ImageAspectRatioField'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'

const DEFAULT_CRON = '0 9 * * 1,3,5'

type OutputPack = AgentPack & { channel: string; plan_task_kind: string }

const outputLabels: Record<string, string> = {
  'wechat-article': '公众号文章',
  seednote: '种草笔记',
  'wechat-picture': '公众号贴图',
}

const outputIcons: Record<string, typeof FileText> = {
  'wechat-article': FileText,
  seednote: BookOpen,
  'wechat-picture': FileImage,
}

const outputTones: Record<string, string> = {
  'wechat-article': 'aria-pressed:border-sky-500 aria-pressed:bg-sky-50 aria-pressed:text-sky-700 dark:aria-pressed:bg-sky-950/40 dark:aria-pressed:text-sky-300',
  seednote: 'aria-pressed:border-rose-500 aria-pressed:bg-rose-50 aria-pressed:text-rose-700 dark:aria-pressed:bg-rose-950/40 dark:aria-pressed:text-rose-300',
  'wechat-picture': 'aria-pressed:border-amber-500 aria-pressed:bg-amber-50 aria-pressed:text-amber-700 dark:aria-pressed:bg-amber-950/40 dark:aria-pressed:text-amber-300',
}

function canUseForPlan(pack: AgentPack): pack is OutputPack {
  return pack.kind === 'managed'
    && pack.surfaces.includes('plan')
    && typeof pack.channel === 'string'
    && pack.channel.trim() !== ''
    && typeof pack.plan_task_kind === 'string'
    && pack.plan_task_kind.trim() !== ''
    && (pack.bindings.task_kinds ?? []).includes(pack.plan_task_kind)
}

function outputLabel(pack: OutputPack) {
  return outputLabels[pack.id] ?? pack.display_name.replace(/^微信公众号/, '公众号')
}

function outputIcon(pack: OutputPack) {
  return outputIcons[pack.id] ?? Layers3
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
  const [attachmentUploading, setAttachmentUploading] = useState(false)
  const [attachmentFailed, setAttachmentFailed] = useState(false)
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [recommendationUnavailable, setRecommendationUnavailable] = useState(false)
  const recommendationRequestRef = useRef(0)
  const scheduleManuallyChangedRef = useRef(false)

  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planSchema) as Resolver<PlanFormValues>,
    defaultValues: defaultValues(),
  })
  useFormDirtyCheck(form, modalOpen)

  const watchedProjectID = useWatch({ control: form.control, name: 'project_id' })
  const watchedAgentIDs = useWatch({ control: form.control, name: 'agent_ids' }) ?? []
  const watchedExecutionProfile = useWatch({ control: form.control, name: 'execution_profile' })
  const watchedImageCapabilityKey = useWatch({ control: form.control, name: 'image_capability_key' }) ?? ''
  const watchedSkipReference = useWatch({ control: form.control, name: 'skip_reference_image' }) ?? false

  const packsQuery = useAgentPacks()
  const outputPacks = useMemo(() => (packsQuery.data?.packs ?? []).filter(canUseForPlan), [packsQuery.data?.packs])
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
    setAttachmentUploading(false)
    setAttachmentFailed(false)
    setReferenceUploading(false)
    form.reset(defaultValues())
  }, [form])

  const openCreate = useCallback(() => {
    const projectID = createIntent.projectId && projectMap[createIntent.projectId] ? createIntent.projectId : ''
    setEditingPlan(null)
    setRecommendationUnavailable(false)
    scheduleManuallyChangedRef.current = false
    form.reset(defaultValues(projectID))
    setModalOpen(true)
    const requestID = ++recommendationRequestRef.current
    void api.plans.scheduleRecommendation().then((recommendation) => {
      if (requestID !== recommendationRequestRef.current || scheduleManuallyChangedRef.current) return
      form.setValue('cron_expr', cronWithTime(form.getValues('cron_expr'), recommendation.time), { shouldDirty: false, shouldValidate: true })
      setRecommendationUnavailable(!recommendation.load_balanced)
    }).catch(() => setRecommendationUnavailable(true))
  }, [createIntent.projectId, form, projectMap])

  useEffect(() => {
    if (!createIntent.shouldCreate || (createIntent.projectId && projects.length === 0)) return
    openCreate()
    setSearchParams({}, { replace: true })
  }, [createIntent.projectId, createIntent.shouldCreate, openCreate, projects.length, setSearchParams])

  function openEdit(plan: Plan) {
    recommendationRequestRef.current += 1
    setEditingPlan(plan)
    setRecommendationUnavailable(false)
    form.reset(formValuesForPlan(plan))
    setModalOpen(true)
  }

  function closeModal() {
    if (form.formState.isDirty) { setShowDirtyDialog(true); return }
    resetModal()
  }

  function changeProject(id: string | null) {
    form.setValue('project_id', id ?? '', { shouldDirty: true, shouldValidate: true })
    // Output selection is independent from the project context.
    if (id) form.setValue('image_ratio', normalizeImageRatio(projectMap[id]?.image_ratio), { shouldDirty: true })
  }

  async function onSubmit(values: PlanFormValues) {
    if (values.agent_ids.length === 0) { form.setError('agent_ids', { type: 'validate', message: '至少选择一种输出类型' }); return }
    if (referenceUploading || attachmentUploading || attachmentFailed || !scheduleValid) return
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

  const isSubmitting = createMutation.isPending || updateMutation.isPending
  const selectedPackSet = useMemo(() => new Set(watchedAgentIDs), [watchedAgentIDs])
  const selectedImageCapability = imageCapabilities.find((item) => item.key === effectiveImageCapability)
  const imageUnavailable = !imageCapabilitiesLoading && !imageCapabilitiesError && selectedImageCapability !== undefined
    && (selectedImageCapability.enabled !== true || selectedImageCapability.price_available !== true)

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
                      <span>{project?.name || '未命名项目'}</span><span aria-hidden="true">·</span><span>{cronToHuman(plan.cron_expr)}</span>
                      {plan.next_run_at ? <><span aria-hidden="true">·</span><span>下次 {formatDateTimeCN(plan.next_run_at)}</span></> : null}
                    </div>
                    <div className="flex flex-wrap gap-1.5" aria-label="输出类型">
                      {outputIDs.map((agentID) => {
                        const pack = outputPacks.find((item) => item.id === agentID)
                        return <Badge key={agentID} variant="outline" className="font-normal">{pack ? outputLabel(pack) : outputLabels[agentID] ?? agentID}</Badge>
                      })}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
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
        <DialogContent className="max-w-2xl">
          <DialogHeader><DialogTitle>{editingPlan ? '编辑计划' : '新建计划'}</DialogTitle></DialogHeader>
          <Form {...form}>
            <form id="plan-form" onSubmit={handleSubmit} className="max-h-[78vh] space-y-5 overflow-y-auto px-1">
              <section className="space-y-2">
                <div className="flex items-baseline justify-between gap-3"><div><h3 className="text-sm font-semibold">输出类型</h3><p className="mt-1 text-xs text-muted-foreground">可多选，下一次触发会为每种输出分别创建任务。</p></div>{watchedAgentIDs.length > 0 ? <span className="text-xs text-muted-foreground">已选 {watchedAgentIDs.length} 项</span> : null}</div>
                <FormField control={form.control} name="agent_ids" render={({ field }) => <FormItem><FormControl><ToggleGroup multiple value={field.value} onValueChange={field.onChange} variant="outline" spacing={2} className="grid w-full grid-cols-1 sm:grid-cols-3" aria-label="输出类型">
                  {packsQuery.isLoading ? <div className="col-span-full rounded-md border border-dashed p-4 text-sm text-muted-foreground">正在加载可用输出...</div> : null}
                  {outputPacks.map((pack) => { const Icon = outputIcon(pack); const selected = selectedPackSet.has(pack.id); return <ToggleGroupItem key={pack.id} value={pack.id} aria-label={outputLabel(pack)} aria-pressed={selected} className={cn('h-auto min-h-20 justify-start gap-2 px-3 py-3 text-left', outputTones[pack.id])}><Icon className="h-5 w-5 shrink-0" /><span className="min-w-0"><span className="block truncate font-medium">{outputLabel(pack)}</span><span className="mt-0.5 block truncate text-xs text-muted-foreground">{pack.description}</span></span></ToggleGroupItem> })}
                </ToggleGroup></FormControl><FormMessage />{packsQuery.isError || outputPacks.length === 0 ? <p className="text-sm text-destructive">暂时没有可用于计划的输出类型。</p> : null}</FormItem>} />
              </section>

              <section className="space-y-2"><h3 className="text-sm font-semibold">项目</h3><FormField control={form.control} name="project_id" render={({ field }) => <FormItem><FormControl>{editingPlan ? <ProjectContextControl mode="readonly" project={selectedProject ?? projectMap[field.value] ?? null} compact /> : <ProjectContextControl mode="select" projects={projects} value={field.value || null} onValueChange={changeProject} loading={projectsLoading} disabled={isSubmitting} allowNoProject={false} placeholder="选择项目" ariaLabel="项目" />}</FormControl><FormMessage /></FormItem>} /></section>

              <section className="space-y-2"><h3 className="text-sm font-semibold">创作提示</h3><FormField control={form.control} name="prompt" render={({ field }) => <FormItem><FormControl><Textarea aria-label="创作提示" {...field} value={field.value ?? ''} rows={4} placeholder="描述每次创作的方向、要求和素材使用方式" /></FormControl><FormDescription>项目中的定位、关键词、视觉风格和作者设置会自动作为公共上下文。</FormDescription><FormMessage /></FormItem>} /></section>

              <section className="space-y-2"><h3 className="text-sm font-semibold">排期</h3><FormField control={form.control} name="cron_expr" render={({ field }) => <FormItem><FormControl><SchedulePicker value={field.value} onChange={handleScheduleChange} onInteraction={handleScheduleInteraction} onValidityChange={handleScheduleValidity} /></FormControl>{recommendationUnavailable ? <p className="text-xs text-muted-foreground">智能排期暂不可用，已保留默认时间。</p> : null}<FormMessage /></FormItem>} /></section>

              <section className="space-y-2"><h3 className="text-sm font-semibold">执行配置</h3><FormField control={form.control} name="execution_profile" render={({ field }) => <FormItem><FormControl><ToggleGroup value={field.value ? [field.value] : []} onValueChange={(value) => { if (value[0]) field.onChange(value[0]) }} variant="outline" className="grid w-full grid-cols-1 sm:grid-cols-3" aria-label="执行配置">{executionProfileOptions.map((profile) => <ToggleGroupItem key={profile.id} value={profile.id} disabled={!profile.available} className="h-auto min-h-16 flex-col items-start gap-1 px-3 py-2 text-left"><span className="font-medium">{profile.display_name}</span><span className="text-xs text-muted-foreground">{profile.available ? profile.description : '当前不可用'}</span></ToggleGroupItem>)}</ToggleGroup></FormControl><FormMessage /></FormItem>} /></section>

              <section className="space-y-2"><h3 className="text-sm font-semibold">共享图片设置</h3><div className="rounded-md border border-border/70 p-3"><div className="space-y-3"><div><p className="mb-2 text-xs font-medium text-muted-foreground">图片能力</p><ImageCapabilitySelector options={imageCapabilities} value={effectiveImageCapability} onChange={(value) => form.setValue('image_capability_key', value, { shouldDirty: true, shouldValidate: true })} disabled={imageCapabilitiesLoading || imageCapabilitiesError} /></div><div><p className="mb-2 text-xs font-medium text-muted-foreground">图片比例</p><ImageAspectRatioField value={form.watch('image_ratio') || 'auto'} ratios={['9:16', '3:4', '1:1', '4:3', '16:9']} onChange={(value) => form.setValue('image_ratio', value, { shouldDirty: true })} /></div></div><details className="mt-3 border-t border-border/70 pt-3"><summary className="cursor-pointer text-sm font-medium">高级设置</summary><div className="mt-3 space-y-4"><FormField control={form.control} name="skip_reference_image" render={({ field }) => <FormItem className="flex items-center justify-between gap-3 space-y-0"><div><FormLabel>跳过参考图</FormLabel><FormDescription>本次计划不使用参考图。</FormDescription></div><FormControl><Switch checked={!!field.value} onCheckedChange={field.onChange} /></FormControl></FormItem>} />{!watchedSkipReference ? <FormField control={form.control} name="reference_image" render={({ field }) => <FormItem><FormLabel>参考图</FormLabel><FormControl><ReferenceAssetUpload value={(field.value as ReferenceImageValue | null) ?? null} onChange={field.onChange} purpose="task_reference" onUploadingChange={setReferenceUploading} disabled={isSubmitting} /></FormControl><FormMessage /></FormItem>} /> : null}<FormField control={form.control} name="watermark" render={({ field }) => <FormItem className="flex items-center justify-between gap-3 space-y-0"><div><FormLabel>水印</FormLabel><FormDescription>在支持的图片能力中启用水印。</FormDescription></div><FormControl><Switch checked={!!field.value} onCheckedChange={field.onChange} /></FormControl></FormItem>} /><FormField control={form.control} name="input_attachments" render={({ field }) => <FormItem><FormLabel>附件</FormLabel><FormControl><ReferenceMaterialInput value={(field.value ?? []) as InputAttachment[]} onChange={field.onChange} allowedTypes={['image', 'audio', 'video', 'document', 'text']} maxCount={16} compact onUploadingChange={setAttachmentUploading} onFailuresChange={setAttachmentFailed} hint="可添加图片、文档或其他公共素材。" /></FormControl><FormMessage /></FormItem>} /></div></details></div>{imageUnavailable ? <p role="alert" className="text-sm text-destructive">当前图像能力不可用，请重新选择。</p> : null}</section>

              <DialogFooter><Button type="button" variant="outline" onClick={closeModal}>取消</Button><Button type="submit" loading={isSubmitting} disabled={isSubmitting || watchedAgentIDs.length === 0 || !watchedProjectID || !watchedExecutionProfile || !scheduleValid || referenceUploading || attachmentUploading || attachmentFailed || imageUnavailable}>{editingPlan ? '保存' : '创建'}</Button></DialogFooter>
            </form>
          </Form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={showDirtyDialog} onOpenChange={setShowDirtyDialog}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle><AlertDialogDescription>关闭后当前表单内容会丢失。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>继续编辑</AlertDialogCancel><AlertDialogAction onClick={resetModal}>放弃修改</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>删除这个计划？</AlertDialogTitle><AlertDialogDescription>删除后不会再触发新的任务。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction disabled={deleteMutation.isPending} onClick={() => { if (deleteTarget) void deleteMutation.mutateAsync(deleteTarget) }}>删除</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
    </div>
  )
}
