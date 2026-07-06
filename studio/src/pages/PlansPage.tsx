import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, FileText, Stamp, Target, Loader2, Images } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { Project, Plan, PlanType, CreatePlanRequest } from '@/types'
import type { Resolver } from 'react-hook-form'
import { ProjectSelector } from '@/components/ProjectSelector'
import { ImageModelSelector } from '@/components/ImageModelSelector'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import SchedulePicker from '@/components/SchedulePicker'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { planStatusLabel, contentTypeLabel, formatDateTimeCN, cronToHuman, getBadgeVariant } from '@/lib/labels'
import { platformBadgeVariant, platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { planSchema, type PlanFormValues } from '@/lib/schemas'
import { buildVideoFormConfig, normalizeVideoConfigForSubmit } from '@/lib/video-form'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useImageModels } from '@/hooks/useImageModels'
import PageHeader from '@/components/layout/PageHeader'
import { SimplePagination } from '@/components/SimplePagination'
import EmptyState from '@/components/EmptyState'
import { VideoCreationPanel } from '@/components/video/VideoCreationPanel'
import { VideoEstimateSummary } from '@/components/video/VideoEstimateSummary'
import { cn } from '@/lib/utils'
import { parseCreationIntent } from '@/lib/command-center'

const planTypeOptions: { value: PlanType; label: string }[] = [
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号文章' },
  { value: 'video', label: '视频生成' },
]

function planToFormValues(plan: Plan): PlanFormValues {
  return {
    project_id: plan.project_id || '',
    type: plan.type,
    cron_expr: plan.cron_expr,
    prompt: plan.prompt || '',
    image_model_key: plan.image_model_key || '',
    skip_reference_image: plan.skip_reference_image || false,
    watermark: plan.watermark || false,
    goal: plan.goal || '',
    goal_mode: plan.goal_mode || false,
    has_content_image: plan.has_content_image ?? true,
    has_tail_image: plan.has_tail_image ?? false,
    article_with_cover: plan.article_with_cover ?? true,
    article_with_content_images: plan.article_with_content_images ?? true,
    video_config: plan.video_config ? buildVideoFormConfig(undefined, plan.video_config) : undefined,
  }
}

export default function PlansPage() {
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const createIntent = parseCreationIntent(searchParams)
  const [projectFilter, setProjectFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [page, setPage] = useState(1)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const { submit } = useSubmitLock()
  const { items: imageModelOptions, isLoading: imageModelsLoading } = useImageModels()
  const highlightedPlanId = searchParams.get('highlight') || ''
  const planRefs = useRef<Record<string, HTMLDivElement | null>>({})

  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planSchema) as Resolver<PlanFormValues>,
    defaultValues: {
      project_id: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_model_key: '',
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
      video_config: undefined,
    },
  })

  // Auto-focus title field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('cron_expr'), 100)
    }
  }, [modalOpen, form])

  const watchedType = useWatch({ control: form.control, name: 'type' })
  const watchedGoalMode = useWatch({ control: form.control, name: 'goal_mode' })
  const watchedProjectId = useWatch({ control: form.control, name: 'project_id' })
  const watchedPrompt = useWatch({ control: form.control, name: 'prompt' })
  const watchedVideoConfig = useWatch({ control: form.control, name: 'video_config' })
  const [debouncedVideoEstimateInput, setDebouncedVideoEstimateInput] = useState<{
    project_id: string
    prompt?: string
    video_config?: PlanFormValues['video_config']
  } | null>(null)

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

  useEffect(() => { setPage(1) }, [projectFilter, searchFilter])

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['plans', projectFilter, page],
    queryFn: () => api.plans.list({
      limit: 50,
      offset: (page - 1) * 50,
      project_id: projectFilter || undefined,
    }),
  })

  const plans = data?.items ?? []
  const totalPlans = data?.total ?? 0
  const totalPages = Math.ceil(totalPlans / 50)
  const filteredPlans = useMemo(() => {
    if (!searchFilter.trim()) return plans
    const q = searchFilter.toLowerCase()
    return plans.filter((p) => (p.prompt || '').toLowerCase().includes(q))
  }, [plans, searchFilter])

  useEffect(() => {
    if (!highlightedPlanId || isLoading) return
    planRefs.current[highlightedPlanId]?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }, [highlightedPlanId, isLoading, filteredPlans.length])

  // Fetch projects for name/avatar display
  const { data: allProjects } = useQuery({
    queryKey: ['projects-for-plans'],
    queryFn: () => api.projects.list(),
    staleTime: 60_000,
  })

  const projectMap = useMemo(() => {
    const map: Record<string, Project> = {}
    if (allProjects) {
      for (const ch of allProjects) {
        map[ch.id] = ch
      }
    }
    return map
  }, [allProjects])
  const selectedProject = projectMap[watchedProjectId ?? ''] ?? undefined

  // 每次执行（单次触发）的基础服务费。计划是定时单任务生成器，无 quantity；
  // 仅强目标模式 ×3。镜像 TasksPage 的 taskCostFor，缺定价时回落默认 3600。
  const { data: pricing } = useQuery({
    queryKey: ['credits', 'pricing'],
    queryFn: () => api.credits.pricing(),
    staleTime: 60_000,
  })
  const { data: creditsBalance } = useQuery({
    queryKey: ['credits', 'balance'],
    queryFn: () => api.credits.balance(),
    staleTime: 30_000,
  })
  const taskCostFor = (type: string) => pricing?.task_costs[type] ?? 3600

  useEffect(() => {
    if (!modalOpen || watchedType !== 'video' || !watchedProjectId) {
      setDebouncedVideoEstimateInput(null)
      return
    }
    const timer = setTimeout(() => {
      setDebouncedVideoEstimateInput({
        project_id: watchedProjectId,
        prompt: watchedPrompt || '',
        video_config: watchedVideoConfig,
      })
    }, 300)
    return () => clearTimeout(timer)
  }, [modalOpen, watchedType, watchedProjectId, watchedPrompt, watchedVideoConfig])

  const videoEstimateQuery = useQuery({
    queryKey: ['video-estimate', 'plan', debouncedVideoEstimateInput],
    queryFn: () => api.video.estimate(debouncedVideoEstimateInput!),
    enabled: Boolean(debouncedVideoEstimateInput),
    retry: false,
  })
  const availableVideoModels = videoEstimateQuery.data?.available_models ?? []

  const createMutation = useMutation({
    mutationFn: (data: CreatePlanRequest) => api.plans.create(data),
    onSuccess: () => {
      toast.success('计划创建成功')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '创建计划失败'))
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: CreatePlanRequest }) => api.plans.update(id, data),
    onSuccess: () => {
      toast.success('计划更新成功')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '更新计划失败'))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.plans.delete(id),
    onSuccess: () => {
      toast.success('计划已删除')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      setDeleteTarget(null)
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '删除计划失败'))
    },
  })

  const pauseMutation = useMutation({
    mutationFn: (id: string) => api.plans.pause(id),
    onSuccess: () => {
      toast.success('计划已暂停')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '暂停计划失败'))
    },
  })

  const resumeMutation = useMutation({
    mutationFn: (id: string) => api.plans.resume(id),
    onSuccess: () => {
      toast.success('计划已恢复')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '恢复计划失败'))
    },
  })

  const openCreate = useCallback(() => {
    const requestedType = createIntent.type && createIntent.type !== 'ecommerce'
      ? (createIntent.type as PlanType)
      : 'seednote'
    setEditingPlan(null)
    form.reset({
      project_id: createIntent.projectId ?? '',
      type: requestedType,
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_model_key: '',
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
      video_config: requestedType === 'video'
        ? buildVideoFormConfig(projectMap[createIntent.projectId ?? '']?.video_defaults)
        : undefined,
    })
    setModalOpen(true)
  }, [createIntent.projectId, createIntent.type, form, projectMap])

  useEffect(() => {
    if (!createIntent.shouldCreate) return
    if (createIntent.projectId && !allProjects) return
    openCreate()
    setSearchParams({}, { replace: true })
  }, [allProjects, createIntent.projectId, createIntent.shouldCreate, openCreate, setSearchParams])

  function openEdit(plan: Plan) {
    setEditingPlan(plan)
    form.reset(planToFormValues(plan))
    setModalOpen(true)
  }

  function closeModal() {
    if (form.formState.isDirty) {
      setShowDirtyDialog(true)
      return
    }
    resetModal()
  }

  function resetModal() {
    setModalOpen(false)
    setShowDirtyDialog(false)
    setEditingPlan(null)
    form.reset({
      project_id: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_model_key: '',
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
      video_config: undefined,
    })
  }

  async function onSubmit(values: PlanFormValues) {
    // For edit (PUT), image_model_key is a *string on the backend: nil = leave
    // unchanged, "" = clear to system default. Always send it so explicit
    // "system default" selection actually clears the previously saved value.
    // For create (POST), "" is also valid (means system default).
    const payload: CreatePlanRequest = {
      type: values.type,
      cron_expr: values.cron_expr.trim(),
      prompt: values.prompt?.trim() || undefined,
      project_id: values.project_id || undefined,
      image_model_key: values.image_model_key,
      watermark: values.watermark || undefined,
      goal_mode: values.goal_mode || undefined,
      goal: values.goal_mode ? (values.goal?.trim() || undefined) : undefined,
      has_content_image: values.type === 'seednote' ? values.has_content_image : undefined,
      has_tail_image: values.type === 'seednote' ? values.has_tail_image : undefined,
      // Article image toggles (公众号文章): both default true; non-article omits.
      article_with_cover: values.type === 'article' ? values.article_with_cover : undefined,
      article_with_content_images: values.type === 'article' ? values.article_with_content_images : undefined,
      video_config: values.type === 'video' ? normalizeVideoConfigForSubmit(values.video_config) : undefined,
    }

    if (editingPlan) {
      await submit(async () => updateMutation.mutateAsync({ id: editingPlan.id, data: payload }))
    } else {
      await submit(async () => createMutation.mutateAsync(payload))
    }
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      <PageHeader title="计划" description="管理你的内容计划，定时自动创作发布。">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建计划
        </Button>
      </PageHeader>

      {/* Project filter + Search */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="w-full sm:w-48">
          <ProjectSelector
            value={projectFilter}
            onChange={(id) => setProjectFilter(id)}
            excludePlatforms={['ecommerce']}
          />
        </div>
        <div className="w-full sm:w-48 sm:ml-auto">
          <SearchInput
            value={searchFilter}
            onChange={setSearchFilter}
            placeholder="搜索计划..."
          />
        </div>
      </div>

      {isError ? (
        <QueryErrorState onRetry={() => refetch()} />
      ) : isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-4 border-l-4 border-l-muted">
              <div className="flex items-start gap-3">
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="min-w-0 flex-1 space-y-2">
                  <Skeleton className="h-4 w-2/3" />
                  <Skeleton className="h-3 w-1/4" />
                  <div className="flex gap-2">
                    <Skeleton className="h-5 w-14 rounded-full" />
                    <Skeleton className="h-3 w-32" />
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : filteredPlans.length === 0 ? (
        <EmptyState
          icon={FileText}
          title="还没有计划"
          description="创建你的第一个内容计划，让 AI 定时帮你创作。"
          action={{ label: '新建计划', onClick: openCreate }}
        />
      ) : (
        <>
        <div className="space-y-3">
          {filteredPlans.map((plan) => {
            const project = projectMap[plan.project_id]
            const borderColor = platformBorderColor[plan.type] || ''
            const hoverBorderColor = platformHoverBorderColor[plan.type] || ''
            const platformBadge = platformBadgeVariant[plan.type] || ('neutral' as const)

            return (
              <div
                key={plan.id}
                ref={(node) => {
                  planRefs.current[plan.id] = node
                }}
                data-testid={`plan-card-${plan.id}`}
                data-plan-id={plan.id}
                data-highlighted={highlightedPlanId === plan.id ? 'true' : undefined}
                className={cn(
                  'group rounded-lg border border-border bg-card p-4 border-l-4 transition-all duration-200 hover:shadow-md',
                  borderColor,
                  hoverBorderColor,
                  highlightedPlanId === plan.id && 'ring-2 ring-primary/30 bg-primary/5',
                )}
              >
                <div className="flex items-start gap-3">
                  <PlatformAvatar avatarUrl={project?.avatar_url} name={project?.name} platform={plan.type} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-start justify-between gap-2">
                      <span className="truncate text-sm font-medium text-foreground">
                        {plan.prompt || contentTypeLabel[plan.type] + '计划'}
                      </span>
                      <Badge variant={getBadgeVariant(plan.status, 'plan')} className="shrink-0">
                        {planStatusLabel[plan.status] || plan.status}
                      </Badge>
                    </div>
                    {project?.name && (
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">{project.name}</p>
                    )}
                    <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      <Badge variant={platformBadge} className="text-[10px]">
                        {contentTypeLabel[plan.type] || plan.type}
                      </Badge>
                      <span>{cronToHuman(plan.cron_expr)}</span>
                      <span>下次：{formatDateTimeCN(plan.next_run_at)}</span>
                    </div>
                  </div>
                </div>
                <div className="mt-2 flex justify-end gap-1.5 border-t border-border pt-2">
                  {plan.status === 'active' && (
                    <Button variant="ghost" size="xs" loading={pauseMutation.isPending} onClick={() => { void submit(async () => pauseMutation.mutateAsync(plan.id)).catch(() => {}) }}>
                      暂停
                    </Button>
                  )}
                  {plan.status === 'paused' && (
                    <Button variant="ghost" size="xs" loading={resumeMutation.isPending} onClick={() => { void submit(async () => resumeMutation.mutateAsync(plan.id)).catch(() => {}) }}>
                      恢复
                    </Button>
                  )}
                  <Button variant="ghost" size="xs" onClick={() => openEdit(plan)}>
                    编辑
                  </Button>
                  <Button
                    variant="destructive"
                    size="xs"
                    onClick={() => setDeleteTarget(plan.id)}
                  >
                    删除
                  </Button>
                </div>
              </div>
            )
          })}
        </div>

        {totalPages > 1 && (
          <div className="mt-4 flex justify-center">
            <SimplePagination
              page={page}
              totalPages={totalPages}
              onPageChange={setPage}
            />
          </div>
        )}
        </>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingPlan ? '编辑计划' : '新建计划'}</DialogTitle>
          </DialogHeader>
          <Form {...form}>
            <form id="plan-form" onSubmit={form.handleSubmit(onSubmit)} className="max-h-[60vh] space-y-4 overflow-y-auto">
              <FormField control={form.control} name="project_id" render={({ field }) => (
                <FormItem>
                  <FormLabel>项目</FormLabel>
                  <FormControl>
                    <ProjectSelector
                      value={field.value || ''}
                      onChange={(id, platform) => {
                        field.onChange(id)
                        if (id) {
                          form.setValue('type', platform as PlanType)
                          const project = allProjects?.find((p) => p.id === id)
                          if (platform === 'video') {
                            form.setValue('video_config', buildVideoFormConfig(project?.video_defaults), { shouldDirty: false })
                          } else {
                            form.setValue('video_config', undefined, { shouldDirty: false })
                          }
                        } else {
                          form.setValue('video_config', undefined, { shouldDirty: false })
                        }
                      }}
                      excludePlatforms={['ecommerce']}
                      disabled={!!editingPlan}
                    />
                  </FormControl>
                  {editingPlan && (
                    <p className="text-xs text-muted-foreground">计划创建后项目不可更换。</p>
                  )}
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="type" render={({ field }) => (
                <FormItem>
                  <FormLabel>内容类型</FormLabel>
                  <FormControl>
                    <Select value={field.value} onValueChange={(v) => field.onChange(v as PlanType)} disabled={!!editingPlan || !!form.watch('project_id')}>
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="选择类型" />
                      </SelectTrigger>
                      <SelectContent>
                        {planTypeOptions.map((opt) => (
                          <SelectItem key={opt.value} value={opt.value} label={opt.label}>{opt.label}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </FormControl>
                  {form.watch('project_id') && (
                    <FormDescription>内容类型随所选项目自动确定</FormDescription>
                  )}
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="cron_expr" render={({ field }) => (
                <FormItem>
                  <FormLabel>排期设置</FormLabel>
                  <FormControl>
                    <SchedulePicker value={field.value} onChange={field.onChange} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              {watchedType !== 'video' && <FormField control={form.control} name="prompt" render={({ field }) => (
                <FormItem>
                  <FormLabel>Prompt（可选）</FormLabel>
                  <FormControl>
                    <Input placeholder="留空则根据项目信息自动生成" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />}

              {watchedType !== 'video' && <FormField control={form.control} name="image_model_key" render={({ field }) => (
                <FormItem>
                  <FormLabel>图像模型</FormLabel>
                  <FormControl>
                    {imageModelsLoading ? (
                      <Skeleton className="h-10 w-full rounded-xl" />
                    ) : (
                      <ImageModelSelector
                        options={imageModelOptions}
                        value={field.value || ''}
                        onChange={field.onChange}
                      />
                    )}
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />}

              {watchedType === 'video' && (
                <VideoCreationPanel
                  form={form}
                  selectedProject={selectedProject}
                  availableVideoModels={availableVideoModels}
                  modelsLoading={videoEstimateQuery.isLoading}
                  title="视频计划"
                  minimumBalanceHint="视频任务需至少 100,000 积分余额；计划触发时也会再次检查。"
                  promptField={(
                    <FormField control={form.control} name="prompt" render={({ field }) => (
                      <FormItem>
                        <FormControl>
                          <Textarea placeholder="描述每次计划要生成的视频方向，留空则根据项目自动生成" {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                  )}
                  estimateSummary={(
                    <VideoEstimateSummary
                      estimate={videoEstimateQuery.data}
                      isLoading={videoEstimateQuery.isLoading}
                      error={videoEstimateQuery.error ? getApiErrorMessage(videoEstimateQuery.error, '视频参数不可用') : undefined}
                    />
                  )}
                />
              )}

              {watchedType !== 'video' && <FormField control={form.control} name="watermark" render={({ field }) => (
                <FormItem>
                  <button
                    type="button"
                    onClick={() => field.onChange(!field.value)}
                    className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${
                      field.value
                        ? 'border-primary bg-primary/5'
                        : 'border-border hover:border-foreground/20'
                    }`}
                  >
                    <Stamp className={`mt-0.5 h-5 w-5 shrink-0 ${field.value ? 'text-primary' : 'text-muted-foreground'}`} />
                    <div className="min-w-0">
                      <p className={`text-sm font-medium ${field.value ? 'text-foreground' : 'text-muted-foreground'}`}>
                        水印
                      </p>
                      <p className="mt-0.5 text-xs text-muted-foreground">开启后生成的图片将带有水印（仅火山引擎支持）</p>
                    </div>
                  </button>
                  <FormMessage />
                </FormItem>
              )} />}

              {/* Image composition (seednote only) */}
              {watchedType === 'seednote' && (
                <FormField control={form.control} name="has_content_image" render={({ field }) => {
                  const hasTail = form.watch('has_tail_image')
                  const total = 1 + (field.value ? 1 : 0) + (hasTail ? 1 : 0)
                  return (
                    <FormItem>
                      <div className="rounded-lg border border-border p-3">
                        <div className="flex items-start gap-3">
                          <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                          <div className="min-w-0 flex-1">
                            <p className="text-sm font-medium text-foreground">图片构成</p>
                            <p className="mt-0.5 text-xs text-muted-foreground">
                              封面始终生成；勾选要额外生成的图。
                            </p>
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
                            <Switch checked={!!field.value} onCheckedChange={field.onChange} />
                          </div>
                          <div className="flex items-center justify-between py-2">
                            <div className="min-w-0">
                              <p className="text-sm font-medium text-foreground">尾图</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">行动召唤 / 关注引导（tail.png）</p>
                            </div>
                            <Switch checked={!!hasTail} onCheckedChange={(v) => form.setValue('has_tail_image', v, { shouldDirty: true })} />
                          </div>
                        </div>
                        <p className="mt-2 text-xs text-muted-foreground">
                          当前将生成 {total} 张图片
                        </p>
                      </div>
                      <FormMessage />
                    </FormItem>
                  )
                }} />
              )}

              {/* Image composition (公众号 article): cover + content images each
                  independently toggleable — unlike seednote, the article cover is
                  NOT mandatory (both default on → legacy behavior). */}
              {watchedType === 'article' && (
                <FormField control={form.control} name="article_with_cover" render={({ field }) => {
                  const withContent = form.watch('article_with_content_images')
                  const summary = field.value && withContent
                    ? '将生成封面 + 正文配图（默认）'
                    : field.value
                      ? '仅生成封面图，不生成正文配图'
                      : withContent
                        ? '仅生成正文配图；发布草稿不设封面'
                        : '纯文字文章，不生成任何图片；发布草稿不设封面'
                  return (
                    <FormItem>
                      <div className="rounded-lg border border-border p-3">
                        <div className="flex items-start gap-3">
                          <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                          <div className="min-w-0 flex-1">
                            <p className="text-sm font-medium text-foreground">图片构成</p>
                            <p className="mt-0.5 text-xs text-muted-foreground">
                              独立选择是否生成封面与正文配图。两者都关 = 纯文字文章。
                            </p>
                          </div>
                        </div>
                        <div className="mt-3 divide-y divide-border">
                          <div className="flex items-center justify-between py-2">
                            <div className="min-w-0">
                              <p className="text-sm font-medium text-foreground">封面图</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">公众号头图（900×383，作为发布草稿封面）</p>
                            </div>
                            <Switch checked={!!field.value} onCheckedChange={field.onChange} />
                          </div>
                          <div className="flex items-center justify-between py-2">
                            <div className="min-w-0">
                              <p className="text-sm font-medium text-foreground">正文配图</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">按排版节奏插入的章节插图</p>
                            </div>
                            <Switch checked={!!withContent} onCheckedChange={(v) => form.setValue('article_with_content_images', v, { shouldDirty: true })} />
                          </div>
                        </div>
                        <p className="mt-2 text-xs text-muted-foreground">{summary}</p>
                      </div>
                      <FormMessage />
                    </FormItem>
                  )
                }} />
              )}


              <FormField control={form.control} name="goal_mode" render={({ field }) => (
                <FormItem>
                  <div className={`rounded-lg border p-3 transition-colors ${
                    field.value ? 'border-primary bg-primary/5' : 'border-border'
                  }`}>
                    <button
                      type="button"
                      onClick={() => field.onChange(!field.value)}
                      className="flex w-full items-start gap-3 text-left"
                    >
                      <Target className={`mt-0.5 h-5 w-5 shrink-0 ${field.value ? 'text-primary' : 'text-muted-foreground'}`} />
                      <div className="min-w-0 flex-1">
                        <p className={`text-sm font-medium ${field.value ? 'text-foreground' : 'text-muted-foreground'}`}>
                          强目标模式
                        </p>
                        <p className="mt-0.5 text-xs text-muted-foreground">
                          每次执行扣费 ×3，最多尝试 3 次。AI 自动评估产出是否满足目标条件，未达成自动重试。
                        </p>
                      </div>
                      <Switch checked={!!field.value} onCheckedChange={field.onChange} />
                    </button>
                    {field.value && (
                      <FormField control={form.control} name="goal" render={({ field: goalField }) => (
                        <FormItem className="mt-3 space-y-2">
                          <FormControl>
                            <Textarea
                              {...goalField}
                              value={goalField.value ?? ''}
                              placeholder="例：文章字数 ≥ 1500 字；必须包含 3 个真实案例；开头必须设置钩子…"
                              className="min-h-[80px] resize-y text-sm"
                              maxLength={4000}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )} />
                    )}
                  </div>
                  <FormMessage />
                </FormItem>
              )} />

              {/* 每次执行（每次触发）的基础服务费。计划无 quantity，仅强目标 ×3。
                  模型、图片、视频等额外 MCP 操作按实际用量结算。 */}
              {(() => {
                const cost = taskCostFor(watchedType as string)
                const multiplier = watchedGoalMode ? 3 : 1
                const perRun = cost * multiplier
                const balance = creditsBalance?.balance ?? 0
                const remaining = balance - perRun
                return (
                  <div className="space-y-1 rounded-md border border-border bg-muted/50 p-3 text-sm">
                    <p className="text-muted-foreground">
                      每次执行基础费用：{cost}{multiplier > 1 ? ` × ${multiplier}` : ''} ={' '}
                      <span className="font-medium text-foreground">{perRun.toLocaleString()}</span> 积分
                      {multiplier > 1 && <span className="ml-1 text-xs text-amber-600">（含目标重试）</span>}
                    </p>
                    <p className="text-xs text-muted-foreground">模型、图片、视频等 MCP 操作费用按实际用量另计。</p>
                    <p className="text-muted-foreground">
                      余额：{balance.toLocaleString()} →{' '}
                      <span className={`font-medium ${remaining < 0 ? 'text-red-500' : 'text-foreground'}`}>
                        {remaining.toLocaleString()}
                      </span>
                    </p>
                    {remaining < 0 && (
                      <p className="text-sm font-medium text-red-500">积分不足，将无法触发执行</p>
                    )}
                  </div>
                )
              })()}
            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button type="submit" form="plan-form" loading={isSubmitting}>
              {editingPlan ? '更新' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(v) => { if (!v) setDeleteTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定删除此计划？</AlertDialogTitle>
            <AlertDialogDescription>此操作不可撤销。删除后计划将无法恢复。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={deleteMutation.isPending} onClick={() => { if (deleteTarget) void submit(async () => deleteMutation.mutateAsync(deleteTarget)).catch(() => {}) }}>
              {deleteMutation.isPending && <Loader2 className="size-3.5 animate-spin" />}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Dirty form confirmation */}
      <AlertDialog open={showDirtyDialog} onOpenChange={setShowDirtyDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃编辑？</AlertDialogTitle>
            <AlertDialogDescription>你有未保存的更改，确定要关闭吗？</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction onClick={resetModal}>放弃</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
