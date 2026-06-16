import { useState, useEffect, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Plus, Loader2, ClipboardList, Check, Download, Square, CheckSquare, Stamp } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import type { TaskType, TaskStatus, CreateTaskRequest, Channel, WorkflowStatus } from '@/types'
import type { Resolver } from 'react-hook-form'
import { ChannelSelector } from '@/components/ChannelSelector'
import { ReferenceImageUpload } from '@/components/channels/ReferenceImageUpload'
import { ImageModelSelector } from '@/components/ImageModelSelector'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import PageHeader from '@/components/layout/PageHeader'
import { SimplePagination } from '@/components/SimplePagination'
import EmptyState from '@/components/EmptyState'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN, statusBadgeVariant, platformDefaultRatio, platformRatioLabel } from '@/lib/labels'
import { platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { createTaskSchema, type CreateTaskFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useImageModels } from '@/hooks/useImageModels'

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '待执行', value: 'pending' },
  { label: '运行中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' },
  { label: '已取消', value: 'cancelled' },
]

function workflowReadinessLabel(workflow: WorkflowStatus | string | null | undefined) {
  if (!workflow) return ''
  const parsed: WorkflowStatus | null = typeof workflow === 'string' ? (() => {
    try {
      return JSON.parse(workflow) as WorkflowStatus
    } catch {
      return null
    }
  })() : workflow
  const readiness = parsed?.review?.readiness
  if (readiness === 'ready') return '可发布'
  if (readiness === 'ready_with_minor_edits') return '建议修改'
  if (readiness === 'needs_revision') return '需重做'
  return ''
}

export default function TasksPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const initialStatus = searchParams.get('status') || 'all'
  const shouldCreate = searchParams.get('create') === 'true'

  const [statusFilter, setStatusFilter] = useState(initialStatus)
  const [channelFilter, setChannelFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [page, setPage] = useState(1)
  const [modalOpen, setModalOpen] = useState(false)
  const [quantity, setQuantity] = useState(1)
  const [watermark, setWatermark] = useState(false)
  const [channelImageRatio, setChannelImageRatio] = useState('')
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([])
  const { submit } = useSubmitLock()
  const { items: imageModelOptions, isLoading: imageModelsLoading } = useImageModels()

  useEffect(() => { setPage(1) }, [statusFilter, channelFilter, searchFilter])

  const { data: channels = [], isLoading: channelsLoading } = useQuery({
    queryKey: ['channels', 'active'],
    queryFn: () => api.channels.list({ status: 'active' }),
  })

  const channelMap = useMemo(() => {
    const map: Record<string, Channel> = {}
    for (const ch of channels) {
      map[ch.id] = ch
    }
    return map
  }, [channels])

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
    defaultValues: { type: 'seednote', prompt: '', channel_id: '', quantity: 1, image_ratio: '', image_model_key: '' },
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
    toast.error('请先创建一个账号，再开始新建任务。')
    navigate('/channels')
  }, [channels.length, modalOpen, navigate])

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['tasks', statusFilter, channelFilter, page],
    queryFn: () =>
      api.tasks.list({
        limit: 50,
        offset: (page - 1) * 50,
        status: statusFilter === 'all' ? undefined : statusFilter,
        channel_id: channelFilter || undefined,
      }),
    refetchInterval: statusFilter === 'all' || statusFilter === 'running' ? 10000 : undefined,
  })

  const tasks = data?.items ?? []
  const totalTasks = data?.total ?? 0
  const totalPages = Math.ceil(totalTasks / 50)
  const filteredTasks = useMemo(() => {
    if (!searchFilter.trim()) return tasks
    const q = searchFilter.toLowerCase()
    return tasks.filter((t) => (t.title || '').toLowerCase().includes(q) || (t.prompt || '').toLowerCase().includes(q))
  }, [tasks, searchFilter])
  const selectedTaskIdSet = useMemo(() => new Set(selectedTaskIds), [selectedTaskIds])
  const selectedTasks = useMemo(
    () => filteredTasks.filter((task) => selectedTaskIdSet.has(task.id)),
    [filteredTasks, selectedTaskIdSet],
  )
  const selectedCompletedTasks = selectedTasks.filter((task) => task.status === 'completed')
  const completedTasksOnPage = filteredTasks.filter((task) => task.status === 'completed')
  const allCompletedSelected = completedTasksOnPage.length > 0 && completedTasksOnPage.every((task) => selectedTaskIdSet.has(task.id))

  useEffect(() => {
    const visibleTaskIds = new Set(filteredTasks.map((task) => task.id))
    setSelectedTaskIds((prev) => {
      const next = prev.filter((id) => visibleTaskIds.has(id))
      return next.length === prev.length ? prev : next
    })
  }, [tasks])

  const createMutation = useMutation({
    mutationFn: (data: CreateTaskRequest) => api.tasks.create(data),
    onSuccess: (task) => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      toast.success('任务创建成功')
      resetModal()
      navigate(`/tasks/${task.id}`)
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

  const bulkDownloadMutation = useMutation({
    mutationFn: (taskIds: string[]) => api.tasks.downloadBulkZipBlob(taskIds),
    onSuccess: (blob) => {
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `tasks_export_${new Date().toISOString().slice(0, 10)}.zip`
      a.click()
      URL.revokeObjectURL(url)
      toast.success('批量下载已开始')
    },
    onError: () => toast.error('批量下载失败，请稍后重试'),
  })

  function openCreate() {
    if (channels.length === 0) {
      toast.error('请先创建一个账号，再开始新建任务。')
      navigate('/channels')
      return
    }
    const defaultType = (searchParams.get('type') || 'seednote') as TaskType
    form.reset({ type: defaultType, prompt: '', channel_id: '', image_ratio: '', image_model_key: '', reference_image_url: '' })
    setQuantity(1)
    setWatermark(false)
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
    form.reset({ type: 'seednote', prompt: '', channel_id: '', image_ratio: '', image_model_key: '', reference_image_url: '' })
    setQuantity(1)
    setWatermark(false)
    setChannelImageRatio('')
  }

  async function onSubmit(values: CreateTaskFormValues) {
    await submit(async () => createMutation.mutateAsync({
      type: values.type as import('@/types').TaskType,
      prompt: values.prompt?.trim() || undefined,
      channel_id: values.channel_id,
      quantity: quantity > 1 ? quantity : undefined,
      image_ratio: values.image_ratio || undefined,
      image_model_key: values.image_model_key || undefined,
      reference_image_url: values.reference_image_url || undefined,
      watermark: watermark || undefined,
    }))
  }

  function toggleTaskSelection(taskId: string) {
    setSelectedTaskIds((prev) =>
      prev.includes(taskId) ? prev.filter((id) => id !== taskId) : [...prev, taskId],
    )
  }

  function toggleSelectCompletedOnPage() {
    const completedIds = completedTasksOnPage.map((task) => task.id)
    if (allCompletedSelected) {
      setSelectedTaskIds((prev) => prev.filter((id) => !completedIds.includes(id)))
      return
    }
    setSelectedTaskIds((prev) => Array.from(new Set([...prev, ...completedIds])))
  }

  function handleBulkDownload() {
    const taskIds = selectedCompletedTasks.map((task) => task.id)
    if (taskIds.length === 0) {
      toast.error('请选择已完成的任务')
      return
    }
    bulkDownloadMutation.mutate(taskIds)
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

        {/* Search */}
        <div className="w-full sm:w-48 sm:ml-auto">
          <SearchInput
            value={searchFilter}
            onChange={setSearchFilter}
            placeholder="搜索任务..."
          />
        </div>
      </div>

      {isError ? (
        <QueryErrorState onRetry={() => refetch()} />
      ) : isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-4 border-l-4 border-l-muted">
              <div className="flex items-start gap-3">
                <Skeleton className="h-4 w-4 mt-1" />
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="min-w-0 flex-1 space-y-2">
                  <Skeleton className="h-4 w-3/5" />
                  <Skeleton className="h-3 w-1/3" />
                  <div className="flex gap-2">
                    <Skeleton className="h-5 w-12 rounded-full" />
                    <Skeleton className="h-3 w-24" />
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : filteredTasks.length === 0 ? (
        <EmptyState
          icon={ClipboardList}
          title={statusFilter === 'all' ? '还没有任务' : `没有${taskStatusLabel[statusFilter as TaskStatus]}的任务`}
          description={
            statusFilter === 'all'
              ? channels.length === 0
                ? '先创建一个账号，再开始生成内容。'
                : '创建任务开始生成内容。'
              : '尝试其他筛选条件或创建新任务。'
          }
          action={
            statusFilter === 'all'
              ? channels.length === 0
                ? { label: '去创建账号', onClick: () => navigate('/channels') }
                : { label: '新建任务', onClick: openCreate }
              : undefined
          }
        />
      ) : (
        <>
        <div className="space-y-2">
          <div className="sticky top-0 z-10 flex flex-col gap-2 rounded-lg border border-border bg-background/95 p-3 shadow-sm backdrop-blur sm:flex-row sm:items-center sm:justify-between">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <Button variant="secondary" size="sm" onClick={toggleSelectCompletedOnPage} disabled={completedTasksOnPage.length === 0}>
                {allCompletedSelected ? <CheckSquare className="h-4 w-4" /> : <Square className="h-4 w-4" />}
                {allCompletedSelected ? '取消全选已完成' : '全选已完成'}
              </Button>
              <span className="text-muted-foreground">
                已选 {selectedTaskIds.length} 个，{selectedCompletedTasks.length} 个可下载
              </span>
              {selectedTaskIds.length > selectedCompletedTasks.length && (
                <span className="text-xs text-muted-foreground">仅打包已完成任务</span>
              )}
            </div>
            <div className="flex items-center gap-2">
              {selectedTaskIds.length > 0 && (
                <Button variant="ghost" size="sm" onClick={() => setSelectedTaskIds([])}>
                  清空选择
                </Button>
              )}
              <Button
                size="sm"
                onClick={handleBulkDownload}
                loading={bulkDownloadMutation.isPending}
                disabled={selectedCompletedTasks.length === 0}
              >
                <Download className="h-4 w-4" />
                下载选中文件
              </Button>
            </div>
          </div>
          {filteredTasks.map((task) => {
            const channel = channelMap[task.channel_id]
            const borderColor = platformBorderColor[task.type] || ''
            const hoverBorderColor = platformHoverBorderColor[task.type] || ''
            const selected = selectedTaskIdSet.has(task.id)

            return (
              <Link key={task.id} to={`/tasks/${task.id}`} className="block">
                <div className={`rounded-lg border border-border bg-card p-4 border-l-4 ${borderColor} ${hoverBorderColor} transition-all duration-200 hover:shadow-sm active:scale-[0.99]`}>
                  <div className="flex items-start gap-3">
                    <button
                      type="button"
                      aria-label={selected ? '取消选择任务' : '选择任务'}
                      aria-pressed={selected}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        toggleTaskSelection(task.id)
                      }}
                      className={`mt-1 rounded-md p-1 transition-colors ${
                        selected ? 'text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                      }`}
                    >
                      {selected ? <CheckSquare className="h-4 w-4" /> : <Square className="h-4 w-4" />}
                    </button>
                    <PlatformAvatar avatarUrl={channel?.avatar_url} name={channel?.name} platform={task.type} />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-start justify-between gap-2">
                        <h3 className="truncate text-sm font-medium text-foreground">{task.title || task.prompt || (contentTypeLabel[task.type] || task.type) + ' 任务'}</h3>
                        <div className="flex shrink-0 items-center gap-1.5">
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
                          {task.status === 'completed' && workflowReadinessLabel(task.workflow_status) && (
                            <Badge variant="outline">
                              {workflowReadinessLabel(task.workflow_status)}
                            </Badge>
                          )}
                        </div>
                      </div>
                      {channel?.name && (
                        <p className="mt-0.5 truncate text-xs text-muted-foreground">{channel.name}</p>
                      )}
                      <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <Badge variant="outline" className="text-[10px]">
                          {contentTypeLabel[task.type] || task.type}
                        </Badge>
                        {task.status === 'running' && (task.progress ?? 0) > 0 && (
                          <Badge variant="warning" className="text-[10px]">
                            {task.progress ?? 0}%
                          </Badge>
                        )}
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
                  </div>
                </div>
              </Link>
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
                  <FormLabel>账号</FormLabel>
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
                    <Textarea placeholder="描述你的创作要求，留空则根据账号信息自动生成" {...field} />
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

              {/* Image model selector */}
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

              {watchedType !== 'viral_analysis' && (
                <FormField control={form.control} name="reference_image_url" render={({ field }) => (
                  <FormItem>
                    <FormLabel>参考图片（可选）</FormLabel>
                    <FormControl>
                      <ReferenceImageUpload
                        value={field.value || ''}
                        onChange={field.onChange}
                        purpose="reference"
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )} />
              )}

              {/* Watermark toggle */}
              <button
                type="button"
                onClick={() => setWatermark(!watermark)}
                className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${
                  watermark
                    ? 'border-primary bg-primary/5'
                    : 'border-border hover:border-foreground/20'
                }`}
              >
                <Stamp className={`mt-0.5 h-5 w-5 shrink-0 ${watermark ? 'text-primary' : 'text-muted-foreground'}`} />
                <div className="min-w-0">
                  <p className={`text-sm font-medium ${watermark ? 'text-foreground' : 'text-muted-foreground'}`}>
                    水印
                  </p>
                  <p className="mt-0.5 text-xs text-muted-foreground">开启后生成的图片将带有水印（仅火山引擎支持）</p>
                </div>
              </button>

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
