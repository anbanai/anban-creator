import { useState, useEffect, useMemo, useRef } from 'react'
import { useInfiniteQuery, useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { AlertTriangle, Plus, ClipboardList, Download, Square, CheckSquare, Ban, RotateCcw, Trash2, BarChart3, CheckCircle2, CircleDashed, XCircle, Ban as BanIcon, LoaderCircle } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import type { AgentExecutionProfileID, TaskStatus, Project, TaskType } from '@/types'
import { ProjectSelector } from '@/components/ProjectSelector'
import { AgentIconStack, agentDisplayName } from '@/components/agent-prompt/AgentIconStack'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { taskStatusLabel, contentTypeDisplayName, formatDateTimeCN } from '@/lib/labels'
import { platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { parseCreationIntent, projectsReturnHref } from '@/lib/command-center'
import { taskActionSignal } from '@/lib/studio-ux'
import { taskStageSummary } from '@/lib/task-lifecycle'
import { taskContentAnalyticsHref } from '@/lib/content-analytics'
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

function TaskStatusMark({ status }: { status: TaskStatus }) {
  const Icon = status === 'completed' ? CheckCircle2 : status === 'failed' ? XCircle : status === 'cancelled' ? BanIcon : status === 'running' ? LoaderCircle : CircleDashed
  return <Icon aria-hidden="true" className={`size-4 shrink-0 ${status === 'completed' ? 'text-emerald-600' : status === 'failed' ? 'text-destructive' : status === 'running' ? 'animate-spin text-primary motion-reduce:animate-none' : 'text-muted-foreground'}`} />
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
    toast.error('请先创建一个项目，再开始新建任务。')
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

  // Bulk cancel / clone / delete — best-effort; the server returns a per-task
  // summary. Each operates only on the subset it can act on; on success we toast
  // the succeeded/skipped counts, invalidate the list, and clear the selection.
  const toastBulk = (verb: string, res: { succeeded: number; skipped: number }) =>
    toast.success(`已${verb} ${res.succeeded} 个任务${res.skipped ? `，跳过 ${res.skipped} 个` : ''}`)
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
      toast.error('请先创建一个项目，再开始新建任务。')
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
      toast.error('请选择已完成的任务')
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
      title: '批量取消任务？',
      desc: `已选 ${selectedTasks.length} 个，其中 ${selectedCancellable.length} 个可取消，其余将跳过。未消耗的部分将退还积分，此操作不可撤销。`,
    },
    clone: {
      title: '批量克隆任务？',
      desc: `已选 ${selectedTasks.length} 个，其中 ${selectedCloneable.length} 个可克隆，其余将跳过。克隆会按新任务重新计费，原任务保留。`,
    },
    delete: {
      title: '批量删除任务？',
      desc: `已选 ${selectedTasks.length} 个，其中 ${selectedDeletable.length} 个可删除，其余将跳过。将永久删除任务及其产出文件，不可恢复。`,
    },
  }

  const queueStats = useMemo(() => ({
    active: tasks.filter((t) => t.status === 'running' || t.status === 'pending').length,
    failed: tasks.filter((t) => t.status === 'failed').length,
  }), [tasks])
  const statusCounts = useMemo(() => statusTabs.reduce<Record<string, number>>((counts, tab) => {
    counts[tab.value] = tab.value === 'all'
      ? totalTasks
      : tasks.filter((task) => task.status === tab.value).length
    return counts
  }, {}), [tasks, totalTasks])
  const loadedRangeLabel = totalTasks === 0
    ? '当前筛选共 0 个'
    : `当前筛选共 ${totalTasks} 个 · 已加载 1–${Math.min(tasks.length, totalTasks)} / ${totalTasks} 个`
  const failedTaskParams = new URLSearchParams(searchParams)
  failedTaskParams.set('status', 'failed')

  return (
    <div className="space-y-6">
      <PageHeader
        title="任务"
        description={
          queueStats.active > 0
            ? <>已加载：{queueStats.active} 个任务等待或执行中</>
            : '查看进度、产物与发布状态。'
        }
      >
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建任务
        </Button>
      </PageHeader>

      {planFilter ? (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-muted/30 px-3 py-2 text-sm">
          <span>正在查看此计划生成的任务</span>
          <div className="flex items-center gap-3">
            <Link to="/plans" className="text-primary hover:underline">返回计划</Link>
            <Button variant="ghost" size="sm" onClick={() => {
              setSelectedTaskIds([])
              setSearchParams((current) => { const next = new URLSearchParams(current); next.delete('plan_id'); return next }, { replace: true })
            }}>查看全部任务</Button>
          </div>
        </div>
      ) : null}

      {!isLoading && !isError && (
        <section aria-label="任务状态概览" className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 border-b border-border/70 px-3 py-2.5">
            <h2 className="text-sm font-semibold text-foreground">任务状态</h2>
            <p className="text-xs text-muted-foreground">状态数量按当前已加载任务统计</p>
          </div>
          <div className="grid grid-cols-2 divide-x divide-y divide-border/70 sm:grid-cols-5 sm:divide-y-0">
            {statusTabs.filter((tab) => tab.value !== 'all').map((tab) => (
              <button
                key={tab.value}
                type="button"
                className="flex min-w-0 items-center justify-between gap-2 px-3 py-2.5 text-left transition-colors hover:bg-muted/35 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/60"
                aria-label={`筛选${tab.label}任务`}
                onClick={() => {
                  setStatusFilter(tab.value)
                  setSearchParams((current) => {
                    const next = new URLSearchParams(current)
                    next.set('status', tab.value)
                    return next
                  }, { replace: true })
                }}
              >
                <span className="truncate text-xs text-muted-foreground">
                  {tab.label}{' '}
                  <strong className="text-sm tabular-nums text-foreground">{statusCounts[tab.value] ?? 0}</strong>
                </span>
              </button>
            ))}
          </div>
        </section>
      )}

      {queueStats.failed > 0 && (
        <div role="alert" aria-label="失败任务提醒" className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2.5 text-sm">
          <div className="flex min-w-0 items-start gap-2.5">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
            <div className="min-w-0">
              <p className="font-medium text-foreground">需要处理 · {queueStats.failed} 个失败任务</p>
              <p className="mt-0.5 text-xs text-muted-foreground">进入详情查看失败原因，并继续执行或克隆任务。</p>
            </div>
          </div>
          <Link
            to={`/tasks?${failedTaskParams}`}
            aria-label={`查看失败任务，${queueStats.failed} 个失败任务`}
            className="shrink-0 text-sm font-medium text-destructive hover:underline"
          >
            查看失败任务
          </Link>
        </div>
      )}

      {/* Filters apply to the task queue; text search covers tasks loaded so far. */}
      <div className="flex flex-wrap items-center gap-3">
        {/* Status filter tabs */}
        <div className="flex max-w-full flex-wrap gap-1 rounded-lg bg-muted p-1" role="group" aria-label="任务状态">
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
              className={`whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                statusFilter === tab.value
                  ? 'bg-card text-foreground shadow-sm'
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
            placeholder="搜索已加载任务..."
          />
        </div>
      </div>

      {!isLoading && !isError && (
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <p role="status">{loadedRangeLabel}{searchFilter.trim() ? ` · 匹配 ${filteredTasks.length} 个` : ''}</p>
          {(statusFilter !== 'all' || projectFilter || searchFilter) && (
            <Button variant="ghost" size="sm" onClick={() => {
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
          title={planFilter && !searchFilter.trim() && !projectFilter && statusFilter === 'all' ? '此计划还没有生成任务' : searchFilter.trim() || projectFilter ? '未找到匹配的任务' : statusFilter === 'all' ? '还没有任务' : `没有${taskStatusLabel[statusFilter as TaskStatus]}的任务`}
          description={
            planFilter && !searchFilter.trim() && !projectFilter && statusFilter === 'all'
              ? '计划会按排期自动创建任务，可返回计划检查排期与启用状态。'
              : searchFilter.trim() || projectFilter
              ? '试试其他关键词、清空筛选，或继续向下滚动加载任务。搜索仅匹配已加载任务。'
              : statusFilter === 'all'
              ? projects.length === 0
                ? '先创建一个项目，再开始生成内容。'
                : '创建任务开始生成内容。'
              : '尝试其他筛选条件或创建新任务。'
          }
          action={
            !planFilter && !searchFilter.trim() && !projectFilter && statusFilter === 'all'
              ? projects.length === 0
                ? { label: '去创建项目', onClick: () => navigate('/projects') }
                : { label: '新建任务', onClick: openCreate }
              : undefined
          }
        />
      ) : (
        <>
        <div className="space-y-3">
          {selectedTaskIds.length > 0 && (
            <div className="sticky top-0 z-10 flex flex-col gap-2 rounded-lg border border-primary/30 bg-background/95 p-3 shadow-sm backdrop-blur sm:flex-row sm:items-center sm:justify-between">
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
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="ghost" size="sm" onClick={() => setSelectedTaskIds([])} disabled={bulkAnyPending}>
                  清空选择
                </Button>
                {/* 批量操作：每个按钮只对它能作用的子集生效（计数即实际提交数），
                    点击进入二次确认。cancel/clone=outline，delete=destructive 以示不可逆。 */}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setBulkAction('cancel')}
                  disabled={selectedCancellable.length === 0 || bulkAnyPending}
                >
                  <Ban className="h-4 w-4" />
                  取消{selectedCancellable.length > 0 ? ` (${selectedCancellable.length})` : ''}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setBulkAction('clone')}
                  disabled={selectedCloneable.length === 0 || bulkAnyPending}
                >
                  <RotateCcw className="h-4 w-4" />
                  克隆{selectedCloneable.length > 0 ? ` (${selectedCloneable.length})` : ''}
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setBulkAction('delete')}
                  disabled={selectedDeletable.length === 0 || bulkAnyPending}
                >
                  <Trash2 className="h-4 w-4" />
                  删除{selectedDeletable.length > 0 ? ` (${selectedDeletable.length})` : ''}
                </Button>
                <Button
                  size="sm"
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
          <div className="overflow-hidden rounded-lg border border-border bg-card">
            {filteredTasks.map((task) => {
            const project = projectMap[task.project_id]
            const borderColor = platformBorderColor[task.type] || ''
            const hoverBorderColor = platformHoverBorderColor[task.type] || ''
            const selected = selectedTaskIdSet.has(task.id)
            const actionSignal = taskActionSignal(task)
            const stage = taskStageSummary(task)
            const activityAt = taskActivityTimestamp(task)

            return (
              <div key={task.id} className="border-b border-border last:border-b-0">
                <div className={`border-l-2 p-3.5 sm:p-4 ${borderColor} ${hoverBorderColor} transition-colors hover:bg-muted/35`}>
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
                      className={`-ml-1 flex min-h-9 min-w-9 items-center justify-center rounded-md transition-colors ${
                        selected ? 'text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                      }`}
                    >
                      {selected ? <CheckSquare className="h-4 w-4" /> : <Square className="h-4 w-4" />}
                    </button>
                    <Link to={`/tasks/${task.id}`} className="flex min-w-0 flex-1 items-start gap-3 rounded-md">
                      <div className="flex shrink-0 items-center gap-2 pt-0.5" title={agentDisplayName(taskAgentId(task))}>
                        <AgentIconStack agentIds={[taskAgentId(task)]} compact />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-start justify-between gap-2">
                          <h3 className="line-clamp-2 text-sm font-medium leading-6 text-foreground sm:line-clamp-1">{task.title || task.prompt || contentTypeDisplayName(task.type) + ' 任务'}</h3>
                          <div className="flex w-7 shrink-0 items-center justify-end gap-1.5" title={taskStatusLabel[task.status] || task.status}>
                            <TaskStatusMark status={task.status} />
                            <span className="sr-only">{taskStatusLabel[task.status] || task.status}</span>
                          </div>
                        </div>
                        <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1.5 text-xs text-muted-foreground">
                          {project ? <span className="max-w-[min(16rem,42vw)] truncate text-muted-foreground">{project.name}</span> : <span>未设置项目</span>}
                          <span className={actionSignal.tone === 'risk' ? 'font-medium text-destructive' : 'font-medium text-foreground'}>{stage.title}</span>
                          {actionSignal.tone === 'risk' && <span>{actionSignal.hint}</span>}
                          <span aria-label={`最近活动：${formatDateTimeCN(activityAt)}`}>最近活动：{formatDateTimeCN(activityAt)}</span>
                          <span className="font-medium text-foreground">累计扣费：{(task.billing_total_credits ?? task.billing_price_credits).toLocaleString()} 积分</span>
                        </div>
                      </div>
                    </Link>
                    <div className="flex w-9 shrink-0 items-center justify-center">
                      {task.status === 'completed' && (task.type === 'wechat-article' || task.type === 'seednote') ? (
                        <Link
                          to={taskContentAnalyticsHref(task.project_id, task.id, task.channel || task.type)}
                          aria-label="查看内容分析"
                          title="查看内容分析"
                          className="inline-flex size-9 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
                        ><BarChart3 className="size-4" /></Link>
                      ) : <span aria-hidden="true" className="size-9" />}
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
            <span className="text-sm text-muted-foreground">正在加载更多任务...</span>
          ) : hasNextPage ? (
            <Button variant="ghost" size="sm" onClick={() => fetchNextPage()}>
              继续加载
            </Button>
          ) : (
            <span className="text-xs text-muted-foreground">已加载全部任务</span>
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
                  ? '暂时无法获取所选配置的任务价格'
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
              确认{bulkAction === 'delete' ? '删除' : bulkAction === 'cancel' ? '取消任务' : bulkAction === 'clone' ? '克隆' : ''}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
