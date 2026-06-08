import { useState, useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, FileText } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import type { Channel, Plan, PlanType, CreatePlanRequest } from '@/types'
import type { Resolver } from 'react-hook-form'
import { ChannelSelector } from '@/components/ChannelSelector'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import SchedulePicker from '@/components/SchedulePicker'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { planStatusLabel, contentTypeLabel, contentTypeOptions, formatDateTimeCN, cronToHuman, getBadgeVariant } from '@/lib/labels'
import { platformBadgeVariant, platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { planSchema, type PlanFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import PageHeader from '@/components/layout/PageHeader'
import { SimplePagination } from '@/components/SimplePagination'
import EmptyState from '@/components/EmptyState'

function planToFormValues(plan: Plan): PlanFormValues {
  return {
    channel_id: plan.channel_id || '',
    type: plan.type,
    cron_expr: plan.cron_expr,
    prompt: plan.prompt || '',
    skip_reference_image: plan.skip_reference_image || false,
  }
}

export default function PlansPage() {
  const queryClient = useQueryClient()
  const [channelFilter, setChannelFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [page, setPage] = useState(1)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const { submit } = useSubmitLock()

  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planSchema) as Resolver<PlanFormValues>,
    defaultValues: {
      channel_id: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
    },
  })

  // Auto-focus title field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('cron_expr'), 100)
    }
  }, [modalOpen, form])

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

  useEffect(() => { setPage(1) }, [channelFilter, searchFilter])

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['plans', channelFilter, page],
    queryFn: () => api.plans.list({
      limit: 50,
      offset: (page - 1) * 50,
      channel_id: channelFilter || undefined,
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

  // Fetch channels for name/avatar display
  const { data: allChannels } = useQuery({
    queryKey: ['channels-for-plans'],
    queryFn: () => api.channels.list(),
    staleTime: 60_000,
  })

  const channelMap = useMemo(() => {
    const map: Record<string, Channel> = {}
    if (allChannels) {
      for (const ch of allChannels) {
        map[ch.id] = ch
      }
    }
    return map
  }, [allChannels])

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
      channel_id: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
    })
    setModalOpen(true)
  }

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
      channel_id: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
    })
  }

  async function onSubmit(values: PlanFormValues) {
    const payload: CreatePlanRequest = {
      type: values.type,
      cron_expr: values.cron_expr.trim(),
      prompt: values.prompt?.trim() || undefined,
      channel_id: values.channel_id || undefined,
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

      {/* Channel filter + Search */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="w-full sm:w-48">
          <ChannelSelector
            value={channelFilter}
            onChange={(id) => setChannelFilter(id)}
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
            const channel = channelMap[plan.channel_id]
            const borderColor = platformBorderColor[plan.type] || ''
            const hoverBorderColor = platformHoverBorderColor[plan.type] || ''
            const platformBadge = platformBadgeVariant[plan.type] || ('neutral' as const)

            return (
              <div
                key={plan.id}
                className={`group rounded-lg border border-border bg-card p-4 border-l-4 ${borderColor} ${hoverBorderColor} transition-all duration-200 hover:shadow-md`}
              >
                <div className="flex items-start gap-3">
                  <PlatformAvatar avatarUrl={channel?.avatar_url} name={channel?.name} platform={plan.type} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-start justify-between gap-2">
                      <span className="truncate text-sm font-medium text-foreground">
                        {plan.prompt || contentTypeLabel[plan.type] + '计划'}
                      </span>
                      <Badge variant={getBadgeVariant(plan.status, 'plan')} className="shrink-0">
                        {planStatusLabel[plan.status] || plan.status}
                      </Badge>
                    </div>
                    {channel?.name && (
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">{channel.name}</p>
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
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingPlan ? '编辑计划' : '新建计划'}</DialogTitle>
          </DialogHeader>
          <Form {...form}>
            <form id="plan-form" onSubmit={form.handleSubmit(onSubmit)} className="max-h-[60vh] space-y-4 overflow-y-auto">
              <FormField control={form.control} name="channel_id" render={({ field }) => (
                <FormItem>
                  <FormLabel>账号</FormLabel>
                  <FormControl>
                    <ChannelSelector
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
                    <Select value={field.value} onValueChange={(v) => field.onChange(v as PlanType)} disabled={!!form.watch('channel_id')}>
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
                  {form.watch('channel_id') && (
                    <FormDescription>内容类型随所选账号自动确定</FormDescription>
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
                    <Input placeholder="留空则根据账号信息自动生成" {...field} />
                  </FormControl>
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
            <AlertDialogAction variant="destructive" loading={deleteMutation.isPending} onClick={() => { if (deleteTarget) submit(async () => deleteMutation.mutateAsync(deleteTarget)) }}>
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
