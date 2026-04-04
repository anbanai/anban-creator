import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type TaskType, type CreateTaskRequest, type TaskStatus } from '@/lib/api'
import { ChannelSelector } from '@/components/ChannelSelector'
import Button from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import Modal from '@/components/ui/Modal'
import { Input } from '@/components/ui/Input'
import Select from '@/components/ui/Select'
import { taskStatusLabel, contentTypeLabel, contentTypeOptions, formatDateTimeCN } from '@/lib/labels'

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
  const [form, setForm] = useState<{ type: TaskType; topic: string; channel_id: string; channel_platform: string }>({
    type: 'rednote',
    topic: '',
    channel_id: '',
    channel_platform: '',
  })
  const [formError, setFormError] = useState('')

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
      setFormError('创建任务失败，请重试。')
    },
  })

  function openCreate() {
    setForm({ type: 'rednote', topic: '', channel_id: '', channel_platform: '' })
    setFormError('')
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setForm({ type: 'rednote', topic: '', channel_id: '', channel_platform: '' })
    setFormError('')
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setFormError('')
    if (!form.topic.trim()) {
      setFormError('主题不能为空。')
      return
    }
    createMutation.mutate({
      type: form.type,
      topic: form.topic.trim(),
      channel_id: form.channel_id || undefined,
    })
  }

  const runningCount = tasks.filter((t) => t.status === 'running').length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">任务</h1>
          <p className="mt-1 text-sm text-gray-400">
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
        <div className="flex gap-1 overflow-x-auto rounded-lg border border-gray-700 bg-gray-800 p-1">
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
                  ? 'bg-gray-700 text-white'
                  : 'text-gray-400 hover:bg-gray-700/50 hover:text-gray-200'
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
          <svg className="h-8 w-8 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
      ) : tasks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-gray-700 bg-gray-800 py-16">
          <svg className="mb-4 h-12 w-12 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
          </svg>
          <p className="text-sm text-gray-400">
            {statusFilter === 'all' ? '还没有任务' : `没有${taskStatusLabel[statusFilter]}的任务`}
          </p>
          <p className="mt-1 text-xs text-gray-500">
            {statusFilter === 'all'
              ? '创建任务开始生成内容。'
              : '尝试其他筛选条件或创建新任务。'}
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {tasks.map((task) => (
            <Link key={task.id} to={`/tasks/${task.id}`} className="block">
              <Card className="transition-colors hover:border-gray-600">
                <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="truncate text-sm font-medium text-gray-100">{task.topic}</h3>
                      <Badge variant="outline" className="shrink-0 text-[10px]">
                        {contentTypeLabel[task.type] || task.type}
                      </Badge>
                      {task.status === 'running' && task.progress > 0 && (
                        <Badge variant="warning" className="shrink-0 text-[10px]">
                          {task.progress}%
                        </Badge>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500">
                      <span>创建：{formatDateTimeCN(task.created_at)}</span>
                      {task.completed_at && (
                        <span>完成：{formatDateTimeCN(task.completed_at)}</span>
                      )}
                    </div>
                    {task.status === 'running' && (
                      <div className="mt-2 h-1.5 w-full rounded-full bg-gray-700">
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

      {/* Create Task Modal */}
      <Modal
        open={modalOpen}
        onClose={closeModal}
        title="新建任务"
        footer={
          <>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button onClick={handleSubmit} loading={createMutation.isPending}>
              创建
            </Button>
          </>
        }
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1.5 block text-sm font-medium text-gray-300">频道</label>
            <ChannelSelector
              value={form.channel_id}
              onChange={(id, platform) => {
                setForm({
                  ...form,
                  channel_id: id,
                  channel_platform: id ? platform : '',
                  // Auto-set type from channel platform
                  type: id ? (platform as TaskType) || form.type : form.type,
                })
              }}
            />
            <p className="mt-1 text-xs text-gray-500">选择频道以自动填充内容类型和配置。</p>
          </div>

          <Select
            label="内容类型"
            options={contentTypeOptions}
            value={form.type}
            onChange={(e) => setForm({ ...form, type: e.target.value as TaskType })}
          />

          <Input
            label="主题"
            placeholder="例如：2026夏季最佳护肤指南"
            value={form.topic}
            onChange={(e) => setForm({ ...form, topic: e.target.value })}
            required
          />

          {formError && (
            <div className="rounded-lg bg-red-900/50 px-3 py-2 text-sm text-red-300">
              {formError}
            </div>
          )}
        </form>
      </Modal>
    </div>
  )
}
