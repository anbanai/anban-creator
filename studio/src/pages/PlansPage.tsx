import { useState, useEffect, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, FileText, Stamp, Target, Loader2, Images } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import type { Project, Plan, PlanType, CreatePlanRequest, Template } from '@/types'
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
import { TemplatePicker } from '@/components/templates/TemplatePicker'
import { PersonaBlock } from '@/components/templates/PersonaBlock'
import { ThemePicker } from '@/components/templates/ThemePicker'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { planStatusLabel, contentTypeLabel, contentTypeOptions, formatDateTimeCN, cronToHuman, getBadgeVariant } from '@/lib/labels'
import { platformBadgeVariant, platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { planSchema, type PlanFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useImageModels } from '@/hooks/useImageModels'
import PageHeader from '@/components/layout/PageHeader'
import { SimplePagination } from '@/components/SimplePagination'
import EmptyState from '@/components/EmptyState'

function planToFormValues(plan: Plan): PlanFormValues {
  return {
    project_id: plan.project_id || '',
    type: plan.type,
    cron_expr: plan.cron_expr,
    prompt: plan.prompt || '',
    image_model_key: plan.image_model_key || '',
    skip_reference_image: plan.skip_reference_image || false,
    style: plan.style || '',
    writing_style: plan.writing_style || '',
    theme: plan.theme || '',
    author: plan.author || '',
    author_style_intro: plan.author_style_intro || '',
    author_avatar_url: plan.author_avatar_url || '',
    watermark: plan.watermark || false,
    goal: plan.goal || '',
    goal_mode: plan.goal_mode || false,
    has_content_image: plan.has_content_image ?? true,
    has_tail_image: plan.has_tail_image ?? false,
  }
}

export default function PlansPage() {
  const queryClient = useQueryClient()
  const [projectFilter, setProjectFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [page, setPage] = useState(1)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null)
  const { submit } = useSubmitLock()
  const { items: imageModelOptions, isLoading: imageModelsLoading } = useImageModels()

  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planSchema) as Resolver<PlanFormValues>,
    defaultValues: {
      project_id: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_model_key: '',
      style: '',
      writing_style: '',
      theme: '',
      author: '',
      author_style_intro: '',
      author_avatar_url: '',
      has_content_image: true,
      has_tail_image: false,
    },
  })

  // Auto-focus title field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('cron_expr'), 100)
    }
  }, [modalOpen, form])

  const watchedType = useWatch({ control: form.control, name: 'type' })
  const watchedAuthor = useWatch({ control: form.control, name: 'author' })
  const watchedAuthorIntro = useWatch({ control: form.control, name: 'author_style_intro' })
  const watchedAuthorAvatar = useWatch({ control: form.control, name: 'author_avatar_url' })
  const watchedTheme = useWatch({ control: form.control, name: 'theme' })

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

  const createMutation = useMutation({
    mutationFn: (data: CreatePlanRequest) => api.plans.create(data),
    onSuccess: () => {
      toast.success('计划创建成功')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: () => {
      toast.error('创建计划失败')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: CreatePlanRequest }) => api.plans.update(id, data),
    onSuccess: () => {
      toast.success('计划更新成功')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: () => {
      toast.error('更新计划失败')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.plans.delete(id),
    onSuccess: () => {
      toast.success('计划已删除')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      setDeleteTarget(null)
    },
  })

  const pauseMutation = useMutation({
    mutationFn: (id: string) => api.plans.pause(id),
    onSuccess: () => {
      toast.success('计划已暂停')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
  })

  const resumeMutation = useMutation({
    mutationFn: (id: string) => api.plans.resume(id),
    onSuccess: () => {
      toast.success('计划已恢复')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
  })

  function openCreate() {
    setEditingPlan(null)
    form.reset({
      project_id: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_model_key: '',
      style: '',
      writing_style: '',
      theme: '',
      author: '',
      author_style_intro: '',
      author_avatar_url: '',
      has_content_image: true,
      has_tail_image: false,
    })
    setSelectedTemplate(null)
    setModalOpen(true)
  }

  function openEdit(plan: Plan) {
    setEditingPlan(plan)
    form.reset(planToFormValues(plan))
    setSelectedTemplate(null)
    setModalOpen(true)

    // 回填计划已绑定的模板：fetch 后回填 selectedTemplate，style 已由
    // planToFormValues 从 plan.style 填入，无需再覆盖（shouldDirty:false 不触脏）。
    if (plan.template_id) {
      api.templates
        .get(plan.template_id)
        .then(setSelectedTemplate)
        .catch(() => {
          /* 模板已删/不存在：保持空选，不阻断编辑 */
        })
    }
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
      style: '',
      writing_style: '',
      theme: '',
      author: '',
      author_style_intro: '',
      author_avatar_url: '',
      has_content_image: true,
      has_tail_image: false,
    })
    setSelectedTemplate(null)
  }

  function handleTemplateSelect(template: Template) {
    // Clicking the already-active card is a no-op — preserves any edits the user
    // has made to the style textarea. Switching to a different template refills.
    if (selectedTemplate?.id === template.id) return
    setSelectedTemplate(template)
    // Template thumbnail is a UI preview only — it is not a generation reference image.
    // The three orthogonal style dimensions flow into the plan; spawned tasks inherit
    // them and the agent surfaces them via get_project_profile(task_id).
    form.setValue('style', template.style_prompt || '', { shouldDirty: true })
    form.setValue('writing_style', template.writing_style || '', { shouldDirty: true })
    form.setValue('theme', template.theme || '', { shouldDirty: true })
    // 公众号人设（作者署名 + 写作风格模仿 + 可选头像）随模板导入，仍可编辑。
    form.setValue('author', template.author_name || '', { shouldDirty: true })
    form.setValue('author_style_intro', template.author_style_intro || '', { shouldDirty: true })
    form.setValue('author_avatar_url', template.author_avatar_url || '', { shouldDirty: true })
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
      style: values.style || undefined,
      writing_style: values.writing_style || undefined,
      theme: values.theme || undefined,
      author: values.author || undefined,
      author_style_intro: values.author_style_intro || undefined,
      author_avatar_url: values.author_avatar_url || undefined,
      watermark: values.watermark || undefined,
      goal_mode: values.goal_mode || undefined,
      goal: values.goal_mode ? (values.goal?.trim() || undefined) : undefined,
      has_content_image: values.type === 'seednote' ? values.has_content_image : undefined,
      has_tail_image: values.type === 'seednote' ? values.has_tail_image : undefined,
      // template_id 透传给后端，由 CreateFromPlan 复制到派生任务，从而让 Agent 通过
      // get_project_profile(task_id) 拿到模板的内容脚手架。
      template_id: selectedTemplate?.id || undefined,
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
                className={`group rounded-lg border border-border bg-card p-4 border-l-4 ${borderColor} ${hoverBorderColor} transition-all duration-200 hover:shadow-md`}
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
                    <Button variant="ghost" size="xs" loading={pauseMutation.isPending} onClick={() => submit(async () => pauseMutation.mutateAsync(plan.id))}>
                      暂停
                    </Button>
                  )}
                  {plan.status === 'paused' && (
                    <Button variant="ghost" size="xs" loading={resumeMutation.isPending} onClick={() => submit(async () => resumeMutation.mutateAsync(plan.id))}>
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
                        if (id) form.setValue('type', platform as PlanType)
                      }}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="type" render={({ field }) => (
                <FormItem>
                  <FormLabel>内容类型</FormLabel>
                  <FormControl>
                    <Select value={field.value} onValueChange={(v) => field.onChange(v as PlanType)} disabled={!!form.watch('project_id')}>
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="选择类型" />
                      </SelectTrigger>
                      <SelectContent>
                        {contentTypeOptions.map((opt) => (
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

              <FormField control={form.control} name="prompt" render={({ field }) => (
                <FormItem>
                  <FormLabel>Prompt（可选）</FormLabel>
                  <FormControl>
                    <Input placeholder="留空则根据项目信息自动生成" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="image_model_key" render={({ field }) => (
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
              )} />

              <TemplatePicker
                type={watchedType as import('@/types').TemplateType}
                selected={selectedTemplate}
                onSelect={handleTemplateSelect}
              />

              <FormField control={form.control} name="style" render={({ field }) => (
                <FormItem>
                  <FormLabel>视觉风格</FormLabel>
                  <FormControl>
                    <Textarea
                      {...field}
                      placeholder="选择模板自动填充，或直接输入自定义风格"
                      maxLength={1024}
                      className="min-h-[72px] resize-y"
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              {/* 公众号 (article) 写作风格 + 排版：选模板后自动填入，可继续编辑（覆盖模板/项目）。
                  计划级 author_* 字段经后端解析链 plan > template > project 下发到 spawned task。 */}
              {watchedType === 'article' && (
                <>
                  <PersonaBlock
                    authorName={watchedAuthor ?? ''}
                    onAuthorName={(v) => form.setValue('author', v, { shouldDirty: true })}
                    authorStyleIntro={watchedAuthorIntro ?? ''}
                    onAuthorStyleIntro={(v) => form.setValue('author_style_intro', v, { shouldDirty: true })}
                    authorAvatarUrl={watchedAuthorAvatar ?? ''}
                    onAuthorAvatarUrl={(v) => form.setValue('author_avatar_url', v, { shouldDirty: true })}
                  />
                  <ThemePicker theme={watchedTheme ?? ''} onTheme={(v) => form.setValue('theme', v, { shouldDirty: true })} />
                  {!selectedTemplate && (
                    <p className="text-xs text-muted-foreground">未选模板时默认随项目设置，也可在此覆盖。</p>
                  )}
                </>
              )}

              <FormField control={form.control} name="watermark" render={({ field }) => (
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
              )} />

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
            <AlertDialogAction variant="destructive" disabled={deleteMutation.isPending} onClick={() => { if (deleteTarget) submit(async () => deleteMutation.mutateAsync(deleteTarget)) }}>
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
