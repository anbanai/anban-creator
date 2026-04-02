import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type TaskType, type CreateTaskRequest, type TaskStatus } from '@/lib/api'
import Button from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import Modal from '@/components/ui/Modal'
import { Input } from '@/components/ui/Input'
import Select from '@/components/ui/Select'

const taskTypeOptions = [
  { value: 'rednote', label: 'RedNote (Xiaohongshu)' },
  { value: 'article', label: 'Article (WeChat)' },
  { value: 'xls', label: 'XLS (Xiaolvshu)' },
]

const statusTabs: { label: string; value: string }[] = [
  { label: 'All', value: 'all' },
  { label: 'Pending', value: 'pending' },
  { label: 'Running', value: 'running' },
  { label: 'Completed', value: 'completed' },
  { label: 'Failed', value: 'failed' },
  { label: 'Cancelled', value: 'cancelled' },
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

function formatDateTime(dateStr: string): string {
  if (!dateStr) return '--'
  return new Date(dateStr).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export default function TasksPage() {
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()

  const initialStatus = searchParams.get('status') || 'all'
  const shouldCreate = searchParams.get('create') === 'true'

  const [statusFilter, setStatusFilter] = useState(initialStatus)
  const [modalOpen, setModalOpen] = useState(shouldCreate)
  const [form, setForm] = useState<{ type: TaskType; topic: string }>({
    type: 'rednote',
    topic: '',
  })
  const [formError, setFormError] = useState('')

  // Clear create param on mount
  useEffect(() => {
    if (shouldCreate) {
      setSearchParams({}, { replace: true })
    }
  }, [shouldCreate, setSearchParams])

  const { data, isLoading } = useQuery({
    queryKey: ['tasks', statusFilter],
    queryFn: () =>
      api.tasks.list({
        limit: 50,
        status: statusFilter === 'all' ? undefined : statusFilter,
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
      setFormError('Failed to create task. Please try again.')
    },
  })

  function openCreate() {
    setForm({ type: 'rednote', topic: '' })
    setFormError('')
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setForm({ type: 'rednote', topic: '' })
    setFormError('')
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setFormError('')
    if (!form.topic.trim()) {
      setFormError('Topic is required.')
      return
    }
    createMutation.mutate({ type: form.type, topic: form.topic.trim() })
  }

  const runningCount = tasks.filter((t) => t.status === 'running').length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">Tasks</h1>
          <p className="mt-1 text-sm text-gray-400">
            Track and manage your content tasks.
            {runningCount > 0 && (
              <span className="ml-1 text-amber-400">({runningCount} running)</span>
            )}
          </p>
        </div>
        <Button onClick={openCreate}>
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          New Task
        </Button>
      </div>

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
            {statusFilter === 'all' ? 'No tasks yet' : `No ${statusFilter} tasks`}
          </p>
          <p className="mt-1 text-xs text-gray-500">
            {statusFilter === 'all'
              ? 'Create a task to start producing content.'
              : 'Try a different filter or create a new task.'}
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
                        {task.type}
                      </Badge>
                      {task.status === 'running' && task.progress > 0 && (
                        <Badge variant="warning" className="shrink-0 text-[10px]">
                          {task.progress}%
                        </Badge>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500">
                      <span>Created: {formatDateTime(task.created_at)}</span>
                      {task.completed_at && (
                        <span>Completed: {formatDateTime(task.completed_at)}</span>
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
                    {task.status}
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
        title="New Task"
        footer={
          <>
            <Button variant="secondary" onClick={closeModal}>Cancel</Button>
            <Button onClick={handleSubmit} loading={createMutation.isPending}>
              Create
            </Button>
          </>
        }
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <Select
            label="Content Type"
            options={taskTypeOptions}
            value={form.type}
            onChange={(e) => setForm({ ...form, type: e.target.value as TaskType })}
          />

          <Input
            label="Topic"
            placeholder="e.g. Best skincare routine for summer 2026"
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
