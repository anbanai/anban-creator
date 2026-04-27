import { useState, useEffect } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Plus, Loader2, ClipboardList, Check, Film } from 'lucide-react'
import { api } from '@/lib/api'
import type { TaskType, TaskStatus, CreateTaskRequest } from '@/types'
import type { Resolver } from 'react-hook-form'
import { ChannelSelector } from '@/components/ChannelSelector'
import { Button } from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN, statusBadgeVariant, platformDefaultRatio, platformRatioLabel } from '@/lib/labels'
import { renderPlatformIcon } from '@/lib/PlatformIcon'
import { createTaskSchema, type CreateTaskFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '待执行', value: 'pending' },
  { label: '运行中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' },
  { label: '已取消', value: 'cancelled' },
]

export default function TasksPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const initialStatus = searchParams.get('status') || 'all'
  const shouldCreate = searchParams.get('create') === 'true'

  const [statusFilter, setStatusFilter] = useState(initialStatus)
  const [channelFilter, setChannelFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [quantity, setQuantity] = useState(1)
  const [generateVideo, setGenerateVideo] = useState(false)
  const [channelImageRatio, setChannelImageRatio] = useState('')
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const { submit } = useSubmitLock()

  const { data: channels = [], isLoading: channelsLoading } = useQuery({
    queryKey: ['channels', 'active'],
    queryFn: () => api.channels.list({ status: 'active' }),
  })

  const { data: creditsBalance } = useQuery({
    queryKey: ['credits', 'balance'],
    queryFn: () => api.credits.balance(),
  })

  const { data: pricing } = useQuery({
    queryKey: ['credits', 'pricing'],
    queryFn: () => api.credits.pricing(),
  })

  const taskCostFor = (type: string) => pricing?.task_costs[type] ?? 3200

  const form = useForm<CreateTaskFormValues>({
    resolver: zodResolver(createTaskSchema) as Resolver<CreateTaskFormValues>,
    defaultValues: { type: 'rednote', prompt: '', channel_id: '', quantity: 1, image_ratio: '' },
  })

  const watchedType = useWatch({ control: form.control, name: 'type' })

  // Auto-focus prompt field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('prompt'), 100)
    }
  }, [modalOpen, form])

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

  // Resolve create intent after channel prerequisites are known.
  useEffect(() => {
    if (!shouldCreate || channelsLoading) return
    openCreate()
    setSearchParams({}, { replace: true })
  }, [shouldCreate, channelsLoading, setSearchParams])

  useEffect(() => {
    if (!modalOpen) return
    if (channels.length > 0) return

    setModalOpen(false)
    toast.error('请先创建一个频道，再开始新建任务。')
    navigate('/channels')
  }, [channels.length, modalOpen, navigate])

  const { data, isLoading } = useQuery({
    queryKey: ['tasks', statusFilter, channelFilter],
    queryFn: () =>
      api.tasks.list({
        limit: 50,
        status: statusFilter === 'all' ? undefined : statusFilter,
        channel_id: channelFilter || undefined,
      }),
    refetchInterval: statusFilter === 'all' || statusFilter === 'running' ? 10000 : undefined,
  })

  const tasks = data?.items ?? []

  const createMutation = useMutation({
    mutationFn: (data: CreateTaskRequest) => api.tasks.create(data),
    onSuccess: (task) => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      toast.success('任务创建成功')
      setTimeout(() => {
        resetModal()
        navigate(`/tasks/${task.id}`)
      }, 800)
    },
    onError: () => {
      toast.error('创建任务失败，请重试')
    },
  })

  const togglePublished = useMutation({
    mutationFn: ({ id, published }: { id: string; published: boolean }) =>
      api.tasks.markPublished(id, published),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
    onError: () => toast.error('更新发布状态失败'),
  })

  function openCreate() {
    if (channels.length === 0) {
      toast.error('请先创建一个频道，再开始新建任务。')
      navigate('/channels')
      return
    }
    form.reset({ type: 'rednote', prompt: '', channel_id: '', image_ratio: '' })
    setQuantity(1)
    setGenerateVideo(false)
    setChannelImageRatio('')
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
    form.reset({ type: 'rednote', prompt: '', channel_id: '', image_ratio: '' })
    setQuantity(1)
    setGenerateVideo(false)
    setChannelImageRatio('')
  }

  async function onSubmit(values: CreateTaskFormValues) {
    await submit(async () => createMutation.mutateAsync({
      type: values.type,
      prompt: values.prompt?.trim() || undefined,
      channel_id: values.channel_id,
      quantity: quantity > 1 ? quantity : undefined,
      image_ratio: values.image_ratio || undefined,
      generate_video: generateVideo || undefined,
    }))
  }

  const runningCount = tasks.filter((t) => t.status === 'running').length

  return (
    <div className="space-y-6">
      <PageHeader
        title="任务"
        description={
          runningCount > 0
            ? <>跟踪和管理你的内容任务。<span className="ml-1 text-primary">({runningCount} 运行中)</span></>
            : '跟踪和管理你的内容任务。'
        }
      >
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建任务
        </Button>
      </PageHeader>

      {/* Filters row */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        {/* Status filter tabs */}
        <div className="flex gap-1 overflow-x-auto rounded-lg border border-border bg-muted p-1" role="tablist">
          {statusTabs.map((tab) => (
            <button
              key={tab.value}
              role="tab"
              aria-selected={statusFilter === tab.value}
              onClick={() => {
                setStatusFilter(tab.value)
                if (tab.value !== 'all') {
                  setSearchParams({ status: tab.value })
                } else {
                  setSearchParams({}, { replace: true })
                }
              }}
              className={`whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                statusFilter === tab.value
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Channel filter */}
        <div className="w-full sm:w-48">
          <ChannelSelector
            value={channelFilter}
            onChange={(id) => setChannelFilter(id)}
          />
        </div>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      ) : tasks.length === 0 ? (
        <EmptyState
          icon={ClipboardList}
          title={statusFilter === 'all' ? '还没有任务' : `没有${taskStatusLabel[statusFilter as TaskStatus]}的任务`}
          description={
            statusFilter === 'all'
              ? channels.length === 0
                ? '先创建一个频道，再开始生成内容。'
                : '创建任务开始生成内容。'
              : '尝试其他筛选条件或创建新任务。'
          }
          action={
            statusFilter === 'all'
              ? channels.length === 0
                ? { label: '去创建频道', onClick: () => navigate('/channels') }
                : { label: '新建任务', onClick: openCreate }
              : undefined
          }
        />
      ) : (
        <div className="space-y-2">
          {tasks.map((task) => (
            <Link key={task.id} to={`/tasks/${task.id}`} className="block">
              <Card className="transition-all duration-200 hover:border-foreground/20 hover:shadow-sm active:scale-[0.99]">
                <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="truncate text-sm font-medium text-foreground">{task.title || task.prompt || (contentTypeLabel[task.type] || task.type) + ' 任务'}</h3>
                      <Badge variant="outline" className="shrink-0 text-[10px]">
                        {renderPlatformIcon(task.type)}
                        {contentTypeLabel[task.type] || task.type}
                      </Badge>
                      {task.status === 'running' && (task.progress ?? 0) > 0 && (
                        <Badge variant="warning" className="shrink-0 text-[10px]">
                          {task.progress ?? 0}%
                        </Badge>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      <span>创建：{formatDateTimeCN(task.created_at)}</span>
                      {task.completed_at && (
                        <span>完成：{formatDateTimeCN(task.completed_at)}</span>
                      )}
                    </div>
                    {task.status === 'running' && (
                      <div className="mt-2 h-1.5 w-full rounded-full bg-muted">
                        <div
                          className="h-1.5 rounded-full bg-primary transition-all animate-pulse"
                          style={{ width: `${task.progress ?? 0}%` }}
                        />
                      </div>
                    )}
                  </div>
                  <Badge variant={statusBadgeVariant(task.status)}>
                    {taskStatusLabel[task.status] || task.status}
                  </Badge>
                  {task.status === 'completed' && (
                    <button
                      type="button"
                      disabled={togglePublished.isPending}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        submit(async () => togglePublished.mutateAsync({ id: task.id, published: !task.published }))
                      }}
                      className={`flex items-center gap-1 rounded-md px-2 py-0.5 text-[10px] font-medium transition-colors ${
                        task.published
                          ? 'bg-emerald-500/15 text-emerald-400 hover:bg-emerald-500/25'
                          : 'bg-muted/50 text-muted-foreground hover:bg-muted'
                      }`}
                    >
                      {togglePublished.isPending ? (
                        <Loader2 className="h-3 w-3 animate-spin" />
                      ) : task.published ? (
                        <Check className="h-3 w-3" />
                      ) : null}
                      {task.published ? '已发布' : '标记发布'}
                    </button>
                  )}
                </div>
              </Card>
            </Link>
          ))}
        </div>
      )}

      {/* Create Task Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建任务</DialogTitle>
          </DialogHeader>
          <Form {...form}>
            <form id="task-create-form" onSubmit={form.handleSubmit(onSubmit)} className="max-h-[60vh] space-y-4 overflow-y-auto">
              <FormField control={form.control} name="channel_id" render={({ field }) => (
                <FormItem>
                  <FormLabel>频道</FormLabel>
                  <FormControl>
                    <ChannelSelector
                      value={field.value || ''}
                      onChange={(id, platform) => {
                        field.onChange(id)
                        if (id) {
                          form.setValue('type', platform as TaskType)
                          form.setValue('image_ratio', '')
                          const ch = channels.find((c) => c.id === id)
                          setChannelImageRatio(ch?.image_ratio || platformDefaultRatio[platform] || '3:4')
                        } else {
                          setChannelImageRatio('')
                        }
                      }}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="prompt" render={({ field }) => (
                <FormItem>
                  <FormLabel>创作要求（可选）</FormLabel>
                  <FormControl>
                    <Textarea placeholder="描述你的创作要求，留空则根据频道信息自动生成" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              {/* Quantity selector */}
              <div className="space-y-2">
                <FormLabel>数量</FormLabel>
                <div className="flex gap-2">
                  {[1, 2, 3, 4, 5].map((n) => (
                    <Button
                      key={n}
                      type="button"
                      variant={quantity === n ? 'default' : 'outline'}
                      size="sm"
                      onClick={() => setQuantity(n)}
                    >
                      {n}
                    </Button>
                  ))}
                </div>
              </div>

              {/* Image ratio selector */}
              <FormField control={form.control} name="image_ratio" render={({ field }) => {
                const defaultRatio = channelImageRatio || platformDefaultRatio[watchedType] || '3:4'
                const defaultLabel = platformRatioLabel[watchedType] || `${defaultRatio}（默认）`
                const ratioOptions = [
                  { value: '3:4', label: '3:4 竖版' },
                  { value: '1:1', label: '1:1 方形' },
                  { value: '4:3', label: '4:3 横版' },
                  { value: '16:9', label: '16:9 宽屏' },
                ].map((opt) => opt.value === defaultRatio
                  ? { ...opt, label: `${opt.label}（默认）` }
                  : opt,
                )
                return (
                <FormItem>
                  <FormLabel>封面比例</FormLabel>
                  <Select value={field.value || undefined} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder={defaultLabel} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {ratioOptions.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
                )
              }} />

              {/* Generate video toggle — only for image-heavy types */}
              {(watchedType === 'rednote' || watchedType === 'xls') && (
                <button
                  type="button"
                  onClick={() => setGenerateVideo(!generateVideo)}
                  className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${
                    generateVideo
                      ? 'border-primary bg-primary/5'
                      : 'border-border hover:border-foreground/20'
                  }`}
                >
                  <Film className={`mt-0.5 h-5 w-5 shrink-0 ${generateVideo ? 'text-primary' : 'text-muted-foreground'}`} />
                  <div className="min-w-0">
                    <p className={`text-sm font-medium ${generateVideo ? 'text-foreground' : 'text-muted-foreground'}`}>
                      生成视频
                    </p>
                    <p className="mt-0.5 text-xs text-muted-foreground">开启后将自动合成视频</p>
                  </div>
                </button>
              )}

              {/* Cost display */}
              {(() => {
                const cost = taskCostFor(watchedType)
                const totalCost = cost * quantity
                const balance = creditsBalance?.balance ?? 0
                const remaining = balance - totalCost
                return (
                  <div className="space-y-1 rounded-md border border-border bg-muted/50 p-3 text-sm">
                    <p className="text-muted-foreground">
                      预估消耗：{cost} x {quantity} = <span className="font-medium text-foreground">{totalCost}</span> 积分
                    </p>
                    <p className="text-muted-foreground">
                      余额：{balance.toLocaleString()} →{' '}
                      <span className={`font-medium ${remaining < 0 ? 'text-red-500' : 'text-foreground'}`}>
                        {remaining.toLocaleString()}
                      </span>
                    </p>
                    {remaining < 0 && (
                      <p className="text-sm font-medium text-red-500">积分不足</p>
                    )}
                  </div>
                )
              })()}
            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button
              type="submit"
              form="task-create-form"
              loading={createMutation.isPending}
              disabled={(() => {
                const cost = taskCostFor(watchedType)
                const totalCost = cost * quantity
                const balance = creditsBalance?.balance ?? 0
                return balance - totalCost < 0
              })()}
            >
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
