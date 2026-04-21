import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Loader2, FileText } from 'lucide-react'
import { api, type Plan, type PlanType, type CreatePlanRequest } from '@/lib/api'
import { ChannelSelector } from '@/components/ChannelSelector'
import { Button } from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/Input'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import SchedulePicker from '@/components/SchedulePicker'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { planStatusLabel, contentTypeLabel, contentTypeOptions, formatDateTimeCN } from '@/lib/labels'
import { planSchema, type PlanFormValues } from '@/lib/schemas'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'

function planStatusBadge(status: string) {
  return status === 'active' ? 'success' : 'neutral'
}

function cronToHuman(cron: string): string {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return cron
  const [min, hourStr, , , weekday] = parts
  const time = `${hourStr.padStart(2, '0')}:${min.padStart(2, '0')}`
  if (weekday === '*') return `每天 ${time}`
  const dayNames = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
  const dayList = weekday.split(',').map((d) => dayNames[Number(d)] ?? d).join('、')
  return `每${dayList} ${time}`
}

function planToFormValues(plan: Plan): PlanFormValues {
  return {
    channel_id: plan.channel_id || '',
    type: plan.type,
    title: plan.title,
    description: plan.description || '',
    cron_expr: plan.cron_expr,
    topic_hint: plan.topic_hint || '',
  }
}

export default function PlansPage() {
  const queryClient = useQueryClient()
  const [channelFilter, setChannelFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planSchema),
    defaultValues: {
      channel_id: '',
      type: 'rednote',
      title: '',
      description: '',
      cron_expr: '0 9 * * 1,3,5',
      topic_hint: '',
    },
  })

  const { data, isLoading } = useQuery({
    queryKey: ['plans', channelFilter],
    queryFn: () => api.plans.list({
      limit: 100,
      channel_id: channelFilter || undefined,
    }),
  })

  const plans = data?.items ?? []

  const createMutation = useMutation({
    mutationFn: (data: CreatePlanRequest) => api.plans.create(data),
    onSuccess: () => {
      toast.success('计划创建成功')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      closeModal()
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
      closeModal()
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
      type: 'rednote',
      title: '',
      description: '',
      cron_expr: '0 9 * * 1,3,5',
      topic_hint: '',
    })
    setModalOpen(true)
  }

  function openEdit(plan: Plan) {
    setEditingPlan(plan)
    form.reset(planToFormValues(plan))
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingPlan(null)
  }

  async function onSubmit(values: PlanFormValues) {
    const payload: CreatePlanRequest = {
      type: values.type,
      title: values.title.trim(),
      description: values.description?.trim() || undefined,
      cron_expr: values.cron_expr.trim(),
      topic_hint: values.topic_hint?.trim() || undefined,
      channel_id: values.channel_id || undefined,
    }

    if (editingPlan) {
      updateMutation.mutate({ id: editingPlan.id, data: payload })
    } else {
      createMutation.mutate(payload)
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

      {/* Channel filter */}
      <div className="w-full sm:w-48">
        <ChannelSelector
          value={channelFilter}
          onChange={(id) => setChannelFilter(id)}
        />
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      ) : plans.length === 0 ? (
        <EmptyState
          icon={FileText}
          title="还没有计划"
          description="创建你的第一个内容计划，让 AI 定时帮你创作。"
          action={{ label: '新建计划', onClick: openCreate }}
        />
      ) : (
        <div className="space-y-3">
          {plans.map((plan) => (
            <Card key={plan.id}>
              <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="truncate text-sm font-medium text-foreground">{plan.title}</h3>
                    <Badge variant="outline" className="shrink-0 text-[10px]">
                      {contentTypeLabel[plan.type] || plan.type}
                    </Badge>
                    <Badge variant={planStatusBadge(plan.status)} className="shrink-0">
                      {planStatusLabel[plan.status] || plan.status}
                    </Badge>
                  </div>
                  {plan.description && (
                    <p className="mt-1 truncate text-xs text-muted-foreground">{plan.description}</p>
                  )}
                  <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span>{cronToHuman(plan.cron_expr)}</span>
                    {plan.topic_hint && <span>主题：{plan.topic_hint}</span>}
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">
                    下次执行：{formatDateTimeCN(plan.next_run_at)}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {plan.status === 'active' && (
                    <Button variant="ghost" size="sm" onClick={() => pauseMutation.mutate(plan.id)}>
                      暂停
                    </Button>
                  )}
                  {plan.status === 'paused' && (
                    <Button variant="ghost" size="sm" onClick={() => resumeMutation.mutate(plan.id)}>
                      恢复
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" onClick={() => openEdit(plan)}>
                    编辑
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-red-400 hover:text-red-300"
                    onClick={() => setDeleteTarget(plan.id)}
                  >
                    删除
                  </Button>
                </div>
              </div>
            </Card>
          ))}
        </div>
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
                  <FormLabel>频道</FormLabel>
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
                    <FormDescription>内容类型随所选频道自动确定</FormDescription>
                  )}
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="title" render={({ field }) => (
                <FormItem>
                  <FormLabel>标题</FormLabel>
                  <FormControl>
                    <Input placeholder="例如：每周小红书发布" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="description" render={({ field }) => (
                <FormItem>
                  <FormLabel>描述</FormLabel>
                  <FormControl>
                    <Textarea placeholder="可选的计划描述" {...field} />
                  </FormControl>
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

              <FormField control={form.control} name="topic_hint" render={({ field }) => (
                <FormItem>
                  <FormLabel>主题方向（可选）</FormLabel>
                  <FormControl>
                    <Input placeholder="例如：美妆技巧、科技评测" {...field} />
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
            <AlertDialogAction variant="destructive" onClick={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget) }}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
