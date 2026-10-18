import { useState, useEffect, useMemo, useRef, type ReactNode } from 'react'
import { useInfiniteQuery, useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Plus, ClipboardList, Download, Square, CheckSquare, Ban, RotateCcw, Trash2, BarChart3, CheckCircle2, CircleDashed, XCircle, Ban as BanIcon, LoaderCircle } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import type { AgentExecutionProfileID, Task, TaskStatus, Project, TaskType } from '@/types'
import { ProjectSelector } from '@/components/ProjectSelector'
import { AgentIconStack, agentDisplayName } from '@/components/agent-prompt/AgentIconStack'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { taskStatusLabel, contentTypeDisplayName, formatDateTimeCN } from '@/lib/labels'
import { parseCreationIntent, projectsReturnHref } from '@/lib/command-center'
import { taskActionSignal } from '@/lib/studio-ux'
import { taskStageSummary } from '@/lib/task-lifecycle'
import { taskContentAnalyticsHref } from '@/lib/content-analytics'
import { cn } from '@/lib/utils'
import { TaskFormDialog } from '@/components/tasks/TaskFormDialog'
import { ExecutionProfileSelector } from '@/components/tasks/ExecutionProfileSelector'
import { useAgentExecutionProfiles } from '@/hooks/useAgentExecutionProfiles'
import { cheapestAvailableExecutionProfileForTasks, taskCostTotalFor } from '@/lib/pricing'
import { queryKeys } from '@/lib/query-keys'

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '待执行', value: 'pending' },
  { label: '运行中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' },
  { label: '已取消', value: 'cancelled' },
]

const TASK_PAGE_SIZE = 20
type TaskCursor = { createdAt: string; id: string }

function normalizeTaskStatusFilter(value: string | null) {
  return value && statusTabs.some((tab) => tab.value === value) ? value : 'all'
}

function taskActivityTimestamp(task: { status: TaskStatus; lifecycle?: { updated_at?: string }; completed_at: string; started_at: string; created_at: string; last_heartbeat_at?: string }) {
  if (task.status === 'completed' && task.completed_at) return task.completed_at
  return task.lifecycle?.updated_at || task.last_heartbeat_at || task.completed_at || task.started_at || task.created_at
}

function taskAgentId(task: { agent_id?: string; type: TaskType; project_snapshot?: { agent_id?: string; channel?: string } }) {
  return task.agent_id || task.project_snapshot?.agent_id || task.project_snapshot?.channel || task.type
}

function TaskStatusMark({ status, className = 'size-4' }: { status: TaskStatus; className?: string }) {
  const Icon = status === 'completed' ? CheckCircle2 : status === 'failed' ? XCircle : status === 'cancelled' ? BanIcon : status === 'running' ? LoaderCircle : CircleDashed
  return <Icon aria-hidden="true" className={`${className} shrink-0 ${status === 'completed' ? 'text-emerald-600' : status === 'failed' ? 'text-destructive' : status === 'running' ? 'animate-spin text-primary motion-reduce:animate-none' : 'text-muted-foreground'}`} />
}

function taskStatusTextClass(status: TaskStatus) {
  if (status === 'completed') return 'text-emerald-700 dark:text-emerald-300'
  if (status === 'failed') return 'text-destructive'
  if (status === 'running') return 'text-primary'
  return 'text-muted-foreground'
}

export type TaskActionDefinition = {
  id: string
  label: string
  ariaLabel: string
  icon: ReactNode
  priority: number
  href?: string
  onSelect?: () => void
  disabled?: boolean
  loading?: boolean
  className?: string
}

function TaskActionControl({ action }: { action: TaskActionDefinition }) {
  const inactive = Boolean(action.disabled || action.loading)
  const className = cn(
    'size-11',
    action.className,
    'border-0 bg-transparent shadow-none hover:bg-transparent dark:hover:bg-transparent aria-expanded:bg-transparent focus-visible:bg-transparent',
  )
  const content = action.loading
    ? <LoaderCircle aria-hidden="true" className="size-4 animate-spin motion-reduce:animate-none" />
    : action.icon

  return (
    <Tooltip>
      <TooltipTrigger
        render={action.href
          ? <Link
              to={action.href}
              aria-label={action.ariaLabel}
              aria-disabled={inactive || undefined}
              aria-busy={action.loading || undefined}
              tabIndex={inactive ? -1 : undefined}
              onClick={inactive ? (event) => event.preventDefault() : undefined}
              className={`inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 aria-disabled:pointer-events-none aria-disabled:opacity-50 ${className}`}
            />
          : <Button type="button" variant="ghost" size="icon-md" aria-label={action.ariaLabel} aria-busy={action.loading || undefined} disabled={inactive} onClick={action.onSelect} className={className} />}
      >
        {content}
      </TooltipTrigger>
      <TooltipContent>{action.label}</TooltipContent>
    </Tooltip>
  )
}

export function TaskActionToolbar({
  actions,
  status,
}: {
  actions: TaskActionDefinition[]
  status?: TaskStatus
}) {
  if (actions.length === 0) return null

  const orderedActions = [...actions].sort((a, b) => b.priority - a.priority)

  return (
    <div data-testid="task-action-rail" data-status={status} role="group" aria-label="作品操作" className="flex max-w-full basis-full shrink-0 flex-wrap items-center justify-end gap-1 sm:basis-auto sm:border-l sm:border-border/70 sm:pl-3">
      {orderedActions.map((action) => <TaskActionControl key={action.id} action={action} />)}
    </div>
  )
}

function triggerBlobDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

export default function TasksPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const planFilter = searchParams.get('plan_id') || ''
  const initialStatus = normalizeTaskStatusFilter(searchParams.get('status'))
  const createIntent = parseCreationIntent(searchParams)
  const shouldCreate = createIntent.shouldCreate

  const [statusFilter, setStatusFilter] = useState(initialStatus)
  const [projectFilter, setProjectFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [createDialogIntent, setCreateDialogIntent] = useState<{ projectId?: string; type?: TaskType }>({})
  const [retrySourceTask, setRetrySourceTask] = useState<Task | null>(null)
  const [downloadingTaskIds, setDownloadingTaskIds] = useState<Set<string>>(() => new Set())
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([])
  const [bulkAction, setBulkAction] = useState<'cancel' | 'clone' | 'delete' | null>(null)
  const [bulkExecutionProfile, setBulkExecutionProfile] = useState<AgentExecutionProfileID | ''>('')

  useEffect(() => {
    const nextStatus = normalizeTaskStatusFilter(searchParams.get('status'))
    setStatusFilter((current) => (current === nextStatus ? current : nextStatus))
  }, [searchParams])

  const { data: projects = [], isLoading: projectsLoading } = useQuery({
    queryKey: ['projects', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
  })

  const projectMap = useMemo(() => {
    const map: Record<string, Project> = {}
    for (const ch of projects) {
      map[ch.id] = ch
    }
    return map
  }, [projects])

  // Resolve create intent after project prerequisites are known.
  useEffect(() => {
    if (!shouldCreate || projectsLoading) return
    openCreate()
    setSearchParams({}, { replace: true })
  }, [shouldCreate, projectsLoading, setSearchParams])

  useEffect(() => {
    if (!modalOpen) return
    if (projectsLoading) return
    if (projects.length > 0) return

    setModalOpen(false)
    toast.error('请先创建一个创作项目，再开始新建作品。')
    navigate(projectsReturnHref({ type: createIntent.type ?? 'seednote', intent: createIntent.intent ?? 'new' }))
  }, [projects.length, projectsLoading, modalOpen, navigate, createIntent.type, createIntent.intent])

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: ['tasks', { status: statusFilter, project_id: projectFilter, plan_id: planFilter }],
    initialPageParam: null as TaskCursor | null,
    queryFn: ({ pageParam }) => pageParam
      ? api.tasks.listBefore({
        limit: TASK_PAGE_SIZE,
        before_created_at: pageParam.createdAt,
        before_id: pageParam.id,
        status: statusFilter === 'all' ? undefined : statusFilter,
        project_id: projectFilter || undefined,
        plan_id: planFilter || undefined,
      })
      : api.tasks.list({
        limit: TASK_PAGE_SIZE,
        status: statusFilter === 'all' ? undefined : statusFilter,
        project_id: projectFilter || undefined,
        plan_id: planFilter || undefined,
      }),
    getNextPageParam: (lastPage) => {
      if (lastPage.items.length === 0) return undefined
      const lastTask = lastPage.items[lastPage.items.length - 1]
      return lastTask && lastPage.items.length === TASK_PAGE_SIZE
        ? { createdAt: lastTask.created_at, id: lastTask.id }
        : undefined
    },
    refetchInterval: (query) => {
      const loadedPages = query.state.data?.pages.length ?? 0
      return loadedPages <= 1 && (statusFilter === 'all' || statusFilter === 'running') ? 10000 : false
    },
  })

  const tasks = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data])
  const totalTasks = data?.pages[0]?.total ?? 0
  const loadMoreRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const element = loadMoreRef.current
    if (!element || !hasNextPage || isFetchingNextPage || searchFilter.trim() || typeof IntersectionObserver === 'undefined') return

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) fetchNextPage()
      },
      { rootMargin: '600px 0px' },
    )
    observer.observe(element)
    return () => observer.disconnect()
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, searchFilter])

  const filteredTasks = useMemo(() => {
    if (!searchFilter.trim()) return tasks
    const q = searchFilter.trim().toLowerCase()
    return tasks.filter((t) => (t.title || '').toLowerCase().includes(q) || (t.prompt || '').toLowerCase().includes(q))
  }, [tasks, searchFilter])
  const selectedTaskIdSet = useMemo(() => new Set(selectedTaskIds), [selectedTaskIds])
  const selectedTasks = useMemo(
    () => filteredTasks.filter((task) => selectedTaskIdSet.has(task.id)),
    [filteredTasks, selectedTaskIdSet],
  )
  const selectedCompletedTasks = selectedTasks.filter((task) => task.status === 'completed')
  const selectedCancellable = selectedTasks.filter((task) => task.status === 'pending' || task.status === 'running')
  const selectedCloneable = selectedTasks.filter((task) => task.status === 'failed' || task.status === 'cancelled')
  const selectedDeletable = selectedTasks.filter((task) => task.status !== 'running')
  const completedTasksOnPage = filteredTasks.filter((task) => task.status === 'completed')
  const allCompletedSelected = completedTasksOnPage.length > 0 && completedTasksOnPage.every((task) => selectedTaskIdSet.has(task.id))
  const profilesQuery = useAgentExecutionProfiles()
  const billingCatalogQuery = useQuery({
    queryKey: queryKeys.billing.catalog,
    queryFn: () => api.billing.catalog(),
  })
  const bulkCloneTaskTypes = selectedCloneable.map((task) => task.type)
  const defaultBulkExecutionProfile = cheapestAvailableExecutionProfileForTasks(
    profilesQuery.data,
    billingCatalogQuery.data,
    bulkCloneTaskTypes,
  )
  const selectedBulkProfile = profilesQuery.data?.find((profile) => profile.id === bulkExecutionProfile)
  const bulkCloneTotal = bulkExecutionProfile && selectedBulkProfile?.available
    ? taskCostTotalFor(billingCatalogQuery.data, bulkCloneTaskTypes, bulkExecutionProfile)
    : undefined

  useEffect(() => {
    if (bulkAction !== 'clone' || bulkExecutionProfile || !defaultBulkExecutionProfile) return
    setBulkExecutionProfile(defaultBulkExecutionProfile)
  }, [bulkAction, bulkExecutionProfile, defaultBulkExecutionProfile])

  useEffect(() => {
    const visibleTaskIds = new Set(filteredTasks.map((task) => task.id))
    setSelectedTaskIds((prev) => {
      const next = prev.filter((id) => visibleTaskIds.has(id))
      return next.length === prev.length ? prev : next
    })
  }, [filteredTasks])

  const bulkDownloadMutation = useMutation({
    mutationFn: (taskIds: string[]) => api.tasks.downloadBulkZipBlob(taskIds),
    onSuccess: (blob) => {
      triggerBlobDownload(blob, `tasks_export_${new Date().toISOString().slice(0, 10)}.zip`)
      toast.success('批量下载已开始')
    },
    onError: () => toast.error('批量下载失败，请稍后重试'),
  })

  const downloadTaskMutation = useMutation({
    mutationFn: (task: Task) => api.tasks.downloadZipBlob(task.id),
    onMutate: (task) => {
      setDownloadingTaskIds((current) => new Set(current).add(task.id))
    },
    onSuccess: (blob, task) => {
      triggerBlobDownload(blob, `${(task.title || task.id).replace(/[^\w\-\u4e00-\u9fff]+/g, '_')}.zip`)
      toast.success('作品下载已开始')
    },
    onError: () => toast.error('作品下载失败，请稍后重试'),
    onSettled: (_data, _error, task) => {
      if (!task) return
      setDownloadingTaskIds((current) => {
        const next = new Set(current)
        next.delete(task.id)
        return next
      })
    },
  })

  // Bulk cancel / clone / delete — best-effort; the server returns a per-task
  // summary. Each operates only on the subset it can act on; on success we toast
  // the succeeded/skipped counts, invalidate the list, and clear the selection.
  const toastBulk = (verb: string, res: { succeeded: number; skipped: number }) =>
    toast.success(`已${verb} ${res.succeeded} 个作品${res.skipped ? `，跳过 ${res.skipped} 个` : ''}`)
  const onBulkDone = (res: { succeeded: number; skipped: number }) => {
    setSelectedTaskIds([])
    setBulkAction(null)
    setBulkExecutionProfile('')
    return res
  }
  const bulkCancelMutation = useMutation({
    mutationFn: (taskIds: string[]) => api.tasks.bulkCancel(taskIds),
    onSuccess: (res) => {
      toastBulk('取消', res)
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      onBulkDone(res)
    },
    onError: () => toast.error('批量取消失败，请稍后重试'),
  })
  const bulkCloneMutation = useMutation({
    mutationFn: ({ taskIds, executionProfile }: { taskIds: string[]; executionProfile: AgentExecutionProfileID }) =>
      api.tasks.bulkClone(taskIds, executionProfile),
    onSuccess: (res) => {
      toastBulk('克隆', res)
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      onBulkDone(res)
    },
    onError: () => toast.error('批量克隆失败，请稍后重试'),
  })
  const bulkDeleteMutation = useMutation({
    mutationFn: (taskIds: string[]) => api.tasks.bulkDelete(taskIds),
    onSuccess: (res) => {
      toastBulk('删除', res)
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      onBulkDone(res)
    },
    onError: () => toast.error('批量删除失败，请稍后重试'),
  })
  const bulkAnyPending =
    bulkDownloadMutation.isPending ||
    bulkCancelMutation.isPending ||
    bulkCloneMutation.isPending ||
    bulkDeleteMutation.isPending

  function openCreate() {
    if (projectsLoading) {
      setCreateDialogIntent({ projectId: createIntent.projectId, type: createIntent.type })
      setModalOpen(true)
      return
    }
    if (projects.length === 0) {
      toast.error('请先创建一个创作项目，再开始新建作品。')
      navigate(projectsReturnHref({ type: createIntent.type ?? 'seednote', intent: createIntent.intent ?? 'new' }))
      return
    }
    setCreateDialogIntent({ projectId: createIntent.projectId, type: createIntent.type })
    setModalOpen(true)
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
      toast.error('请选择已完成的作品')
      return
    }
    bulkDownloadMutation.mutate(taskIds)
  }

  // Run the bulk action currently awaiting confirmation. Each sends only the
  // subset it can act on (matching the button counts the user saw).
  function confirmBulkAction() {
    if (bulkAction === 'cancel') bulkCancelMutation.mutate(selectedCancellable.map((t) => t.id))
    else if (bulkAction === 'clone' && bulkExecutionProfile && bulkCloneTotal !== undefined) {
      bulkCloneMutation.mutate({
        taskIds: selectedCloneable.map((task) => task.id),
        executionProfile: bulkExecutionProfile,
      })
    }
    else if (bulkAction === 'delete') bulkDeleteMutation.mutate(selectedDeletable.map((t) => t.id))
  }

  const bulkActionCopy: Record<string, { title: string; desc: string }> = {
    cancel: {
      title: '批量取消作品？',
      desc: `已选 ${selectedTasks.length} 个，其中 ${selectedCancellable.length} 个可取消，其余将跳过。未消耗的部分将退还积分，此操作不可撤销。`,
    },
    clone: {
      title: '批量克隆作品？',
      desc: `已选 ${selectedTasks.length} 个，其中 ${selectedCloneable.length} 个可克隆，其余将跳过。克隆会按新作品重新计费，原作品保留。`,
    },
    delete: {
      title: '批量删除作品？',
      desc: `已选 ${selectedTasks.length} 个，其中 ${selectedDeletable.length} 个可删除，其余将跳过。将永久删除作品及其产出文件，不可恢复。`,
    },
  }

  const activeTaskCount = useMemo(
    () => tasks.filter((t) => t.status === 'running' || t.status === 'pending').length,
    [tasks],
  )
  const loadedRangeLabel = totalTasks === 0
    ? '当前筛选共 0 个'
    : `当前筛选共 ${totalTasks} 个 · 已加载 1–${Math.min(tasks.length, totalTasks)} / ${totalTasks} 个`

  return (
    <div className="space-y-6">
      <PageHeader
        title="作品"
        description={
          activeTaskCount > 0
            ? <>已加载：{activeTaskCount} 个作品等待或执行中</>
            : '查看进度、产物与发布状态。'
        }
      >
        <Button className="min-h-11" onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建作品
        </Button>
      </PageHeader>

      {planFilter ? (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-muted/30 px-3 py-2 text-sm">
          <span>正在查看此排期生成的作品</span>
          <div className="flex items-center gap-3">
            <Link to="/plans" className="inline-flex min-h-11 items-center text-primary hover:underline">返回排期</Link>
            <Button variant="ghost" size="sm" className="min-h-11" onClick={() => {
              setSelectedTaskIds([])
              setSearchParams((current) => { const next = new URLSearchParams(current); next.delete('plan_id'); return next }, { replace: true })
            }}>查看全部作品</Button>
          </div>
        </div>
      ) : null}

      {/* Filters apply to the task queue; text search covers tasks loaded so far. */}
      <div className="flex flex-wrap items-center gap-3 border-b border-border pb-3">
        {/* Status filter tabs */}
        <div className="flex max-w-full flex-wrap gap-1 rounded-lg bg-muted/70 p-1" role="group" aria-label="作品状态">
          {statusTabs.map((tab) => (
            <button
              key={tab.value}
              type="button"
              aria-pressed={statusFilter === tab.value}
              onClick={() => {
                setStatusFilter(tab.value)
                setSearchParams((current) => {
                  const next = new URLSearchParams(current)
                  if (tab.value === 'all') next.delete('status')
                  else next.set('status', tab.value)
                  return next
                }, { replace: true })
              }}
                className={`min-h-10 whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                statusFilter === tab.value
                  ? 'bg-card text-foreground shadow-sm ring-1 ring-border/70'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Project filter */}
        <div className="w-full sm:w-48">
          <ProjectSelector
            value={projectFilter}
            onChange={(id) => setProjectFilter(id)}
          />
        </div>

        {/* Search */}
        <div className="w-full sm:w-48 sm:ml-auto">
          <SearchInput
            value={searchFilter}
            onChange={setSearchFilter}
            placeholder="搜索已加载作品..."
          />
        </div>
      </div>

      {!isLoading && !isError && (
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <p role="status">{loadedRangeLabel}{searchFilter.trim() ? ` · 匹配 ${filteredTasks.length} 个` : ''}</p>
          {(statusFilter !== 'all' || projectFilter || searchFilter) && (
            <Button variant="ghost" size="sm" className="min-h-11" onClick={() => {
              setStatusFilter('all'); setProjectFilter(''); setSearchFilter('')
              setSearchParams((current) => { const next = new URLSearchParams(current); next.delete('status'); return next }, { replace: true })
            }}>清空筛选</Button>
          )}
        </div>
      )}

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
          title={planFilter && !searchFilter.trim() && !projectFilter && statusFilter === 'all' ? '此排期还没有生成作品' : searchFilter.trim() || projectFilter ? '未找到匹配的作品' : statusFilter === 'all' ? '还没有作品' : `没有${taskStatusLabel[statusFilter as TaskStatus]}的作品`}
          description={
            planFilter && !searchFilter.trim() && !projectFilter && statusFilter === 'all'
              ? '系统会按排期自动创建作品，可返回排期检查时间与启用状态。'
              : searchFilter.trim() || projectFilter
              ? '试试其他关键词、清空筛选，或继续向下滚动加载作品。搜索仅匹配已加载作品。'
              : statusFilter === 'all'
              ? projects.length === 0
                ? '先创建一个创作项目，再开始生成内容。'
                : '创建作品开始生成内容。'
              : '尝试其他筛选条件或创建新作品。'
          }
          action={
            !planFilter && !searchFilter.trim() && !projectFilter && statusFilter === 'all'
              ? projects.length === 0
                ? { label: '去创建创作项目', onClick: () => navigate('/projects') }
                : { label: '新建作品', onClick: openCreate }
              : undefined
          }
        />
      ) : (
        <>
          <div className="space-y-2">
          {selectedTaskIds.length > 0 && (
            <div className="sticky top-0 z-10 flex flex-col gap-2 rounded-lg border border-primary/30 bg-background/95 p-3 shadow-sm backdrop-blur sm:flex-row sm:items-center sm:justify-between">
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <Button variant="secondary" size="sm" className="min-h-11" onClick={toggleSelectCompletedOnPage} disabled={completedTasksOnPage.length === 0}>
                  {allCompletedSelected ? <CheckSquare className="h-4 w-4" /> : <Square className="h-4 w-4" />}
                  {allCompletedSelected ? '取消全选已完成' : '全选已完成'}
                </Button>
                <span className="text-muted-foreground">
                  已选 {selectedTaskIds.length} 个，{selectedCompletedTasks.length} 个可下载
                </span>
                {selectedTaskIds.length > selectedCompletedTasks.length && (
                  <span className="text-xs text-muted-foreground">仅打包已完成作品</span>
                )}
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="ghost" size="sm" className="min-h-11" onClick={() => setSelectedTaskIds([])} disabled={bulkAnyPending}>
                  清空选择
                </Button>
                {/* 批量操作：每个按钮只对它能作用的子集生效（计数即实际提交数），
                    点击进入二次确认。cancel/clone=outline，delete=destructive 以示不可逆。 */}
                <Button
                  variant="outline"
                  size="sm"
                  className="min-h-11"
                  onClick={() => setBulkAction('cancel')}
                  disabled={selectedCancellable.length === 0 || bulkAnyPending}
                >
                  <Ban className="h-4 w-4" />
                  取消{selectedCancellable.length > 0 ? ` (${selectedCancellable.length})` : ''}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  className="min-h-11"
                  onClick={() => setBulkAction('clone')}
                  disabled={selectedCloneable.length === 0 || bulkAnyPending}
                >
                  <RotateCcw className="h-4 w-4" />
                  克隆{selectedCloneable.length > 0 ? ` (${selectedCloneable.length})` : ''}
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  className="min-h-11"
                  onClick={() => setBulkAction('delete')}
                  disabled={selectedDeletable.length === 0 || bulkAnyPending}
                >
                  <Trash2 className="h-4 w-4" />
                  删除{selectedDeletable.length > 0 ? ` (${selectedDeletable.length})` : ''}
                </Button>
                <Button
                  size="sm"
                  className="min-h-11"
                  onClick={handleBulkDownload}
                  loading={bulkDownloadMutation.isPending}
                  disabled={selectedCompletedTasks.length === 0 || bulkAnyPending}
                >
                  <Download className="h-4 w-4" />
                  下载选中文件
                </Button>
              </div>
            </div>
          )}
          <div className="overflow-hidden border-y border-border bg-card">
            {filteredTasks.map((task) => {
            const project = projectMap[task.project_id]
            const selected = selectedTaskIdSet.has(task.id)
            const actionSignal = taskActionSignal(task)
            const stage = taskStageSummary(task)
            const activityAt = taskActivityTimestamp(task)

            return (
              <div key={task.id} className="border-b border-border/80 last:border-b-0">
                <div className={`px-2 py-3 transition-colors hover:bg-muted/30 sm:px-3 ${selected ? 'bg-primary/[0.045]' : ''}`}>
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-2.5 sm:gap-x-3">
                    <button
                      type="button"
                      aria-label={selected ? '取消选择作品' : '选择作品'}
                      aria-pressed={selected}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        toggleTaskSelection(task.id)
                      }}
                      className={`-ml-1 flex min-h-11 min-w-11 items-center justify-center rounded-md transition-colors ${
                        selected ? 'text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                      }`}
                    >
                      {selected ? <CheckSquare className="h-4 w-4" /> : <Square className="h-4 w-4" />}
                    </button>
                    <div data-testid="task-type-icon" className="flex size-9 shrink-0 self-center items-center justify-center" title={agentDisplayName(taskAgentId(task))}>
                      <AgentIconStack agentIds={[taskAgentId(task)]} className="scale-110" />
                    </div>
                    <Link to={`/tasks/${task.id}`} className="flex min-w-0 flex-1 items-center rounded-md">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-start justify-between gap-2">
                          <h3 className="line-clamp-2 text-sm font-medium leading-6 text-foreground sm:line-clamp-1">{task.title || task.prompt || contentTypeDisplayName(task.type)}</h3>
                        </div>
                        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                          {project ? <span className="max-w-[min(16rem,42vw)] truncate text-muted-foreground">{project.name}</span> : <span>未设置创作项目</span>}
                          <span aria-hidden="true" className="text-border">·</span>
                          <span className={actionSignal.tone === 'risk' ? 'font-medium text-destructive' : 'font-medium text-foreground'}>{stage.title}</span>
                          {actionSignal.tone === 'risk' && <span>{actionSignal.hint}</span>}
                          <span className="hidden text-border sm:inline" aria-hidden="true">·</span>
                          <span aria-label={`最近活动：${formatDateTimeCN(activityAt)}`}>{formatDateTimeCN(activityAt)}</span>
                          <span className="hidden text-border sm:inline" aria-hidden="true">·</span>
                          <span className="font-medium text-muted-foreground">扣费：{(task.billing_total_credits ?? task.billing_price_credits).toLocaleString()} 积分</span>
                        </div>
                      </div>
                    </Link>
                    <TaskActionToolbar
                      status={task.status}
                      actions={[
                        ...(task.status === 'failed' ? [{
                          id: 'retry',
                          label: '重试',
                          ariaLabel: `重试作品：${task.title || '未命名作品'}`,
                          priority: 100,
                          icon: <RotateCcw className="size-4" />,
                          className: 'font-semibold text-primary hover:text-primary',
                          onSelect: () => setRetrySourceTask(task),
                        }] : []),
                        ...(task.status === 'completed' && (task.type === 'wechat-article' || task.type === 'seednote') ? [{
                          id: 'analytics',
                          label: '查看内容分析',
                          ariaLabel: `查看内容分析：${task.title || '未命名作品'}`,
                          priority: 90,
                          icon: <BarChart3 className="size-4" />,
                          href: taskContentAnalyticsHref(task.project_id, task.id, task.channel || task.type),
                        }] : []),
                        ...(task.status === 'completed' ? [{
                          id: 'download',
                          label: '下载作品文件',
                          ariaLabel: `下载作品：${task.title || '未命名作品'}`,
                          priority: 80,
                          icon: <Download className="size-4" />,
                          loading: downloadingTaskIds.has(task.id),
                          onSelect: () => downloadTaskMutation.mutate(task),
                        }] : []),
                      ]}
                    />
                    <div
                      data-testid="task-status-mark"
                      data-status={task.status}
                      role="img"
                      aria-label={`任务状态：${taskStatusLabel[task.status] || task.status}`}
                      className={`ml-auto flex min-h-11 w-20 shrink-0 items-center gap-1.5 whitespace-nowrap text-xs font-semibold ${taskStatusTextClass(task.status)}`}
                      title={taskStatusLabel[task.status] || task.status}
                    >
                      <TaskStatusMark status={task.status} className="size-4" />
                      <span>{taskStatusLabel[task.status] || task.status}</span>
                    </div>
                  </div>
                </div>
              </div>
            )
            })}
          </div>
        </div>

        </>
      )}

      {!isLoading && !isError && (filteredTasks.length > 0 || hasNextPage || isFetchingNextPage) && (
        <div ref={loadMoreRef} className="flex min-h-12 items-center justify-center pt-3" aria-live="polite">
          {isFetchingNextPage ? (
            <span className="text-sm text-muted-foreground">正在加载更多作品...</span>
          ) : hasNextPage ? (
            <Button variant="ghost" size="sm" className="min-h-11" onClick={() => fetchNextPage()}>
              继续加载
            </Button>
          ) : (
            <span className="text-xs text-muted-foreground">已加载全部作品</span>
          )}
        </div>
      )}

      <TaskFormDialog
        open={modalOpen}
        mode="create"
        initialProjectId={createDialogIntent.projectId}
        initialType={createDialogIntent.type}
        onOpenChange={setModalOpen}
        onCreated={(task) => navigate(`/tasks/${task.id}`)}
      />

      {retrySourceTask ? (
        <TaskFormDialog
          open
          mode="clone"
          sourceTask={retrySourceTask}
          onOpenChange={(open) => {
            if (!open) setRetrySourceTask(null)
          }}
          onCreated={(task) => navigate(`/tasks/${task.id}`)}
        />
      ) : null}

      {/* 批量操作二次确认：文案/按钮随 bulkAction 变化。取消与删除不可逆 → destructive。 */}
      <AlertDialog
        open={bulkAction !== null}
        onOpenChange={(open) => {
          if (!open) {
            setBulkAction(null)
            setBulkExecutionProfile('')
          }
        }}
      >
        <AlertDialogContent className={bulkAction === 'clone' ? 'sm:max-w-2xl' : undefined}>
          <AlertDialogHeader>
            <AlertDialogTitle>{bulkAction ? bulkActionCopy[bulkAction].title : ''}</AlertDialogTitle>
            <AlertDialogDescription>{bulkAction ? bulkActionCopy[bulkAction].desc : ''}</AlertDialogDescription>
          </AlertDialogHeader>
          {bulkAction === 'clone' ? (
            <section className="flex flex-col gap-3" aria-labelledby="bulk-clone-profile-title">
              <h3 id="bulk-clone-profile-title" className="text-sm font-medium">执行配置</h3>
              <ExecutionProfileSelector
                profiles={profilesQuery.data ?? []}
                value={bulkExecutionProfile}
                onChange={setBulkExecutionProfile}
                loading={profilesQuery.isLoading || billingCatalogQuery.isLoading}
                disabled={bulkAnyPending}
              />
              <p className="text-sm text-muted-foreground" aria-live="polite">
                {bulkCloneTotal === undefined
                  ? '暂时无法获取所选配置的创作价格'
                  : `预计总计 ${bulkCloneTotal.toLocaleString()} 积分`}
              </p>
            </section>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkAnyPending}>再想想</AlertDialogCancel>
            <AlertDialogAction
              variant={bulkAction === 'delete' || bulkAction === 'cancel' ? 'destructive' : 'default'}
              disabled={bulkAnyPending || (bulkAction === 'clone' && (!bulkExecutionProfile || bulkCloneTotal === undefined))}
              onClick={confirmBulkAction}
            >
              确认{bulkAction === 'delete' ? '删除' : bulkAction === 'cancel' ? '取消作品' : bulkAction === 'clone' ? '克隆' : ''}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
