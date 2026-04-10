import { useState, useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { api, type TaskType, type CreateTaskRequest, type TaskStatus } from '@/lib/api'
import { ChannelSelector } from '@/components/ChannelSelector'
import { Button } from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/Input'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN } from '@/lib/labels'
import { createTaskSchema, type CreateTaskFormValues } from '@/lib/schemas'

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '待执行', value: 'pending' },
  { label: '运行中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' },
  { label: '已取消', value: 'cancelled' },
]

function statusBadgeVariant(status: TaskStatus) {
  switch (status) {
    case 'completed': return 'success'
    case 'failed': return 'danger'
    case 'running': return 'warning'
    case 'cancelled': return 'neutral'
    default: return 'neutral'
  }
}

export default function TasksPage() {
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()

  const initialStatus = searchParams.get('status') || 'all'
  const shouldCreate = searchParams.get('create') === 'true'

  const [statusFilter, setStatusFilter] = useState(initialStatus)
  const [channelFilter, setChannelFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(shouldCreate)

  const form = useForm<CreateTaskFormValues>({
    resolver: zodResolver(createTaskSchema),
    defaultValues: { type: 'rednote', topic: '', channel_id: '' },
  })

  // Clear create param on mount
  useEffect(() => {
    if (shouldCreate) {
      setSearchParams({}, { replace: true })
    }
  }, [shouldCreate, setSearchParams])

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
      closeModal()
      // Navigate to task detail
      window.location.href = `/tasks/${task.id}`
    },
    onError: () => {
      toast.error('创建任务失败，请重试')
    },
  })

  function openCreate() {
    form.reset({ type: 'rednote', topic: '', channel_id: '' })
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    form.reset({ type: 'rednote', topic: '', channel_id: '' })
  }

  async function onSubmit(values: CreateTaskFormValues) {
    createMutation.mutate({
      type: values.type,
      topic: values.topic.trim(),
      channel_id: values.channel_id || undefined,
    })
  }

  const runningCount = tasks.filter((t) => t.status === 'running').length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-foreground">任务</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            跟踪和管理你的内容任务。
            {runningCount > 0 && (
              <span className="ml-1 text-amber-400">({runningCount} 运行中)</span>
            )}
          </p>
        </div>
        <Button onClick={openCreate}>
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          新建任务
        </Button>
      </div>

      {/* Filters row */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        {/* Status filter tabs */}
        <div className="flex gap-1 overflow-x-auto rounded-lg border border bg-muted p-1">
          {statusTabs.map((tab) => (
            <button
              key={tab.value}
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
                  ? 'bg-accent text-accent-foreground'
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
          <svg className="h-8 w-8 animate-spin text-primary" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
      ) : tasks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border bg-card py-16">
          <svg className="mb-4 h-12 w-12 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
          </svg>
          <p className="text-sm text-muted-foreground">
            {statusFilter === 'all' ? '还没有任务' : `没有${taskStatusLabel[statusFilter]}的任务`}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {statusFilter === 'all'
              ? '创建任务开始生成内容。'
              : '尝试其他筛选条件或创建新任务。'}
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {tasks.map((task) => (
            <Link key={task.id} to={`/tasks/${task.id}`} className="block">
              <Card className="transition-colors hover:border-foreground/20">
                <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="truncate text-sm font-medium text-foreground">{task.topic}</h3>
                      <Badge variant="outline" className="shrink-0 text-[10px]">
                        {contentTypeLabel[task.type] || task.type}
                      </Badge>
                      {task.status === 'running' && task.progress > 0 && (
                        <Badge variant="warning" className="shrink-0 text-[10px]">
                          {task.progress}%
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
                          className="h-1.5 rounded-full bg-amber-500 transition-all"
                          style={{ width: `${task.progress}%` }}
                        />
                      </div>
                    )}
                  </div>
                  <Badge variant={statusBadgeVariant(task.status)}>
                    {taskStatusLabel[task.status] || task.status}
                  </Badge>
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
                        if (id) form.setValue('type', platform as TaskType)
                      }}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="topic" render={({ field }) => (
                <FormItem>
                  <FormLabel>主题</FormLabel>
                  <FormControl>
                    <Input placeholder="例如：2026夏季最佳护肤指南" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />
            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button type="submit" form="task-create-form" loading={createMutation.isPending}>
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
