import { useState, useEffect, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { AlertTriangle, Plus, Loader2, ClipboardList, Check, Download, Square, CheckSquare, Ban, RotateCcw, Trash2 } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import type { TaskStatus, Project, TaskType } from '@/types'
import { ProjectSelector } from '@/components/ProjectSelector'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import PageHeader from '@/components/layout/PageHeader'
import { SimplePagination } from '@/components/SimplePagination'
import EmptyState from '@/components/EmptyState'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN, statusBadgeVariant } from '@/lib/labels'
import { platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { parseCreationIntent, projectsReturnHref } from '@/lib/command-center'
import { taskActionSignal } from '@/lib/studio-ux'
import { TaskFormDialog } from '@/components/tasks/TaskFormDialog'

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '待执行', value: 'pending' },
  { label: '运行中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' },
  { label: '已取消', value: 'cancelled' },
]

function normalizeTaskStatusFilter(value: string | null) {
  return value && statusTabs.some((tab) => tab.value === value) ? value : 'all'
}

export default function TasksPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const initialStatus = normalizeTaskStatusFilter(searchParams.get('status'))
  const createIntent = parseCreationIntent(searchParams)
  const shouldCreate = createIntent.shouldCreate

  const [statusFilter, setStatusFilter] = useState(initialStatus)
  const [projectFilter, setProjectFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [page, setPage] = useState(1)
  const [modalOpen, setModalOpen] = useState(false)
  const [createDialogIntent, setCreateDialogIntent] = useState<{ projectId?: string; type?: TaskType }>({})
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([])
  const { submit } = useSubmitLock()

  useEffect(() => {
    const nextStatus = normalizeTaskStatusFilter(searchParams.get('status'))
    setStatusFilter((current) => (current === nextStatus ? current : nextStatus))
  }, [searchParams])

  useEffect(() => { setPage(1) }, [statusFilter, projectFilter, searchFilter])

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

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['tasks', statusFilter, projectFilter, page],
    queryFn: () =>
      api.tasks.list({
        limit: 50,
        offset: (page - 1) * 50,
        status: statusFilter === 'all' ? undefined : statusFilter,
        project_id: projectFilter || undefined,
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
  const selectedCancellable = selectedTasks.filter((task) => task.status === 'pending' || task.status === 'running')
  const selectedCloneable = selectedTasks.filter((task) => task.status === 'failed' || task.status === 'cancelled')
  const selectedDeletable = selectedTasks.filter((task) => task.status !== 'running')
  const completedTasksOnPage = filteredTasks.filter((task) => task.status === 'completed')
  const allCompletedSelected = completedTasksOnPage.length > 0 && completedTasksOnPage.every((task) => selectedTaskIdSet.has(task.id))

  useEffect(() => {
    const visibleTaskIds = new Set(filteredTasks.map((task) => task.id))
    setSelectedTaskIds((prev) => {
      const next = prev.filter((id) => visibleTaskIds.has(id))
      return next.length === prev.length ? prev : next
    })
  }, [tasks])

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

  // Bulk cancel / clone / delete — best-effort; the server returns a per-task
  // summary. Each operates only on the subset it can act on; on success we toast
  // the succeeded/skipped counts, invalidate the list, and clear the selection.
  const [bulkAction, setBulkAction] = useState<'cancel' | 'clone' | 'delete' | null>(null)
  const toastBulk = (verb: string, res: { succeeded: number; skipped: number }) =>
    toast.success(`已${verb} ${res.succeeded} 个任务${res.skipped ? `，跳过 ${res.skipped} 个` : ''}`)
  const onBulkDone = (res: { succeeded: number; skipped: number }) => {
    setSelectedTaskIds([])
    setBulkAction(null)
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
    mutationFn: (taskIds: string[]) => api.tasks.bulkClone(taskIds),
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
    else if (bulkAction === 'clone') bulkCloneMutation.mutate(selectedCloneable.map((t) => t.id))
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
    approval: tasks.filter((t) => t.publish_approval_state === 'pending').length,
  }), [tasks])

  return (
    <div className="space-y-6">
      <PageHeader
        title="任务"
        description={
          queueStats.active > 0 || queueStats.failed > 0 || queueStats.approval > 0
            ? <>{queueStats.active > 0 ? `${queueStats.active} 个执行中` : '暂无执行中任务'}{queueStats.failed > 0 ? ` · ${queueStats.failed} 个失败待处理` : ''}{queueStats.approval > 0 ? ` · ${queueStats.approval} 个待发布` : ''}</>
            : '查看进度、产物与发布状态。'
        }
      >
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建任务
        </Button>
      </PageHeader>

      {(queueStats.failed > 0 || queueStats.approval > 0) && (
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-y border-border py-3 text-sm">
          <span className="font-medium text-foreground">需要处理</span>
          {queueStats.failed > 0 && (
            <Link to="/tasks?status=failed" className="inline-flex items-center gap-1.5 text-destructive hover:underline">
              <AlertTriangle className="h-4 w-4" />
              {queueStats.failed} 个失败任务
            </Link>
          )}
          {queueStats.approval > 0 && (
            <Link to="/tasks?status=completed" className="text-primary hover:underline">
              {queueStats.approval} 个待发布确认
            </Link>
          )}
        </div>
      )}

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
              ? projects.length === 0
                ? '先创建一个项目，再开始生成内容。'
                : '创建任务开始生成内容。'
              : '尝试其他筛选条件或创建新任务。'
          }
          action={
            statusFilter === 'all'
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

            return (
              <Link key={task.id} to={`/tasks/${task.id}`} className="block border-b border-border last:border-b-0">
                <div className={`border-l-2 p-4 ${borderColor} ${hoverBorderColor} transition-colors hover:bg-muted/35`}>
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
                    <span className="hidden sm:block">
                      <PlatformAvatar avatarUrl={project?.avatar_url} name={project?.name} platform={task.type} />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-start justify-between gap-2">
                        <h3 className="truncate text-sm font-medium text-foreground">{task.title || task.prompt || (contentTypeLabel[task.type] || task.type) + ' 任务'}</h3>
                        <div className="flex shrink-0 items-center gap-1.5">
                          <Badge variant={statusBadgeVariant(task.status)}>
                            {taskStatusLabel[task.status] || task.status}
                          </Badge>
                        </div>
                      </div>
                      {project?.name && (
                        <p className="mt-0.5 truncate text-xs text-muted-foreground">{project.name}</p>
                      )}
                      <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <Badge variant="outline" className="text-[10px]">
                          {contentTypeLabel[task.type] || task.type}
                        </Badge>
                        <span className={actionSignal.tone === 'risk' ? 'text-destructive' : 'text-muted-foreground'}>{actionSignal.label}</span>
                        {actionSignal.tone !== 'risk' && <span>{actionSignal.hint}</span>}
                        {(task.execution_target === 'local' || task.execution_target === 'local_claimed') && (
                          <span>本地{task.execution_target === 'local' ? '待认领' : '运行中'}</span>
                        )}
                        {task.publish_approval_state === 'rejected' && (
                          <span>已驳回发布</span>
                        )}
                        <span>创建：{formatDateTimeCN(task.created_at)}</span>
                        {task.completed_at && (
                          <span>完成：{formatDateTimeCN(task.completed_at)}</span>
                        )}
                      </div>
                      {task.status === 'completed' && (
                        <button
                          type="button"
                          disabled={togglePublished.isPending}
                          onClick={(e) => {
                            e.preventDefault()
                            e.stopPropagation()
                            void submit(async () => togglePublished.mutateAsync({ id: task.id, published: !task.published })).catch(() => {})
                          }}
                          className={`mt-2 inline-flex items-center gap-1 rounded-md px-2 py-1 text-[10px] font-medium transition-colors ${
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
        onOpenChange={(open) => { if (!open) setBulkAction(null) }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{bulkAction ? bulkActionCopy[bulkAction].title : ''}</AlertDialogTitle>
            <AlertDialogDescription>{bulkAction ? bulkActionCopy[bulkAction].desc : ''}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkAnyPending}>再想想</AlertDialogCancel>
            <AlertDialogAction
              variant={bulkAction === 'delete' || bulkAction === 'cancel' ? 'destructive' : 'default'}
              disabled={bulkAnyPending}
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
