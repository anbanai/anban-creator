import { useState, useEffect, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { AlertTriangle, Plus, Loader2, ClipboardList, Check, Download, Square, CheckSquare, Stamp, Target, Images, Package, Minus, Ban, RotateCcw, Trash2 } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { TaskStatus, CreateTaskRequest, Project } from '@/types'
import type { Resolver } from 'react-hook-form'
import { ProjectSelector } from '@/components/ProjectSelector'
import { ImageModelSelector } from '@/components/ImageModelSelector'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import PageHeader from '@/components/layout/PageHeader'
import { SimplePagination } from '@/components/SimplePagination'
import EmptyState from '@/components/EmptyState'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN, statusBadgeVariant, platformDefaultRatio, platformRatioLabel, ecommerceModuleCatalog, ecommerceTargetPlatformOptions, ecommerceLanguageOptions } from '@/lib/labels'
import {
  getLocalExecutorStatus,
  setExecutorEnabled,
  startLocalExecutor,
  type LocalExecutorStatus,
} from '@/lib/tauri'
import {
  canSubmitLocalTask,
  localExecutorCreateHint,
  shouldDefaultRunLocally,
} from '@/lib/local-executor-ux'
import { platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { MultiImageUpload } from '@/components/projects/MultiImageUpload'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { createTaskSchema, type CreateTaskFormValues } from '@/lib/schemas'
import { buildVideoInputForSubmit, initialVideoInput } from '@/lib/video-form'
import { buildMontageInputForSubmit, initialMontageInput } from '@/lib/montage-form'
import { isVideoCreator, isVideoEditor, isVideoPlatform } from '@/lib/video-platforms'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useImageModels } from '@/hooks/useImageModels'
import { VideoCreationPanel } from '@/components/video/VideoCreationPanel'
import { MontageCreationPanel } from '@/components/montage/MontageCreationPanel'
import { parseCreationIntent, projectsReturnHref } from '@/lib/command-center'
import { getProjectCreationDefaults, taskActionSignal, taskCreationCostPreview } from '@/lib/studio-ux'

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
  const [quantity, setQuantity] = useState(1)
  const [watermark, setWatermark] = useState(false)
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [goalMode, setGoalMode] = useState(false)
  const [hasContentImage, setHasContentImage] = useState(true)
  const [hasTailImage, setHasTailImage] = useState(false)
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable. Both default true → legacy "always generate both" (zero regression).
  const [articleWithCover, setArticleWithCover] = useState(true)
  const [articleWithContentImages, setArticleWithContentImages] = useState(true)
  const [goalText, setGoalText] = useState('')
  const [projectImageRatio, setProjectImageRatio] = useState('')
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([])
  // Desktop local-executor integration: when the Tauri shell reports a
  // provisioned local executor, default new tasks to run on the user's machine
  // (enables ffmpeg / local-shell). The user can flip this off to force cloud.
  // In the browser isLocalExecutorAvailable() is always false → no-op.
  const [localExecutorStatus, setLocalExecutorStatus] = useState<LocalExecutorStatus | null>(null)
  const [runLocally, setRunLocally] = useState(true)
  const localExecutorAvailable = localExecutorStatus?.available ?? false
  const localExecutorHint = localExecutorCreateHint(localExecutorStatus)
  useEffect(() => {
    let cancelled = false
    getLocalExecutorStatus().then((status) => {
      if (!cancelled) {
        setLocalExecutorStatus(status)
        setRunLocally(shouldDefaultRunLocally(status))
      }
    })
    return () => { cancelled = true }
  }, [])
  const { submit } = useSubmitLock()
  const { items: imageModelOptions, isLoading: imageModelsLoading } = useImageModels()

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

  const { data: creditsBalance } = useQuery({
    queryKey: ['credits', 'balance'],
    queryFn: () => api.credits.balance(),
  })

  const { data: pricing } = useQuery({
    queryKey: ['credits', 'pricing'],
    queryFn: () => api.credits.pricing(),
  })

  const form = useForm<CreateTaskFormValues>({
    resolver: zodResolver(createTaskSchema) as Resolver<CreateTaskFormValues>,
    defaultValues: { type: 'seednote', prompt: '', project_id: '', quantity: 1, image_ratio: '', image_model_key: '', input_attachments: [], product_photos: [], selected_modules: {}, target_platform: '', selling_points: '', language: '' },
  })

  const watchedType = useWatch({ control: form.control, name: 'type' })
  const isVideoCreatorTask = isVideoCreator(watchedType)
  const isVideoTask = isVideoPlatform(watchedType)
  const isMontageTask = watchedType === 'montage'
  const watchedSelectedModules = useWatch({ control: form.control, name: 'selected_modules' })
  const watchedProductPhotos = useWatch({ control: form.control, name: 'product_photos' })
  const watchedInputAttachments = useWatch({ control: form.control, name: 'input_attachments' })
  const watchedVideoEditorReferences = useWatch({ control: form.control, name: 'video_editor_input.references' })
  const watchedProjectId = useWatch({ control: form.control, name: 'project_id' })
  // 选定项目的配置预览。创建任务时这些值会冻结为 task.project_snapshot。
  const selectedProject = projectMap[watchedProjectId ?? ''] ?? undefined

  // Toggle a module on/off or adjust its quantity. Removing the key (vs storing 0)
  // keeps selected_modules clean and matches the server's "active module" semantics.
  const setModuleQty = (key: string, qty: number) => {
    const cur = form.getValues('selected_modules') ?? {}
    const next = { ...cur }
    if (qty >= 1) next[key] = qty
    else delete next[key]
    form.setValue('selected_modules', next, { shouldDirty: true })
  }

  // Auto-focus prompt field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('prompt'), 100)
    }
  }, [modalOpen, form])

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

  // Resolve create intent after project prerequisites are known.
  useEffect(() => {
    if (!shouldCreate || projectsLoading) return
    openCreate()
    setSearchParams({}, { replace: true })
  }, [shouldCreate, projectsLoading, setSearchParams])

  useEffect(() => {
    if (!modalOpen) return
    if (projects.length > 0) return

    setModalOpen(false)
    toast.error('请先创建一个项目，再开始新建任务。')
    navigate(projectsReturnHref({ type: createIntent.type ?? 'seednote', intent: createIntent.intent ?? 'new' }))
  }, [projects.length, modalOpen, navigate, createIntent.type, createIntent.intent])

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

  const createMutation = useMutation({
    mutationFn: (data: CreateTaskRequest) => api.tasks.create(data),
    onSuccess: (task) => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      toast.success('任务创建成功')
      resetModal()
      navigate(`/tasks/${task.id}`)
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '创建任务失败，请重试'))
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
    if (projects.length === 0) {
      toast.error('请先创建一个项目，再开始新建任务。')
      navigate(projectsReturnHref({ type: createIntent.type ?? 'seednote', intent: createIntent.intent ?? 'new' }))
      return
    }
    const selectedIntentProject = createIntent.projectId ? projectMap[createIntent.projectId] : undefined
    const defaults = getProjectCreationDefaults(selectedIntentProject)
    const defaultType = createIntent.type ?? defaults.type
    form.reset({
      type: defaultType,
      prompt: '',
      project_id: selectedIntentProject?.id ?? '',
      image_ratio: defaults.imageRatio as CreateTaskFormValues['image_ratio'],
      image_model_key: defaults.imageModelKey,
      input_attachments: [],
      product_photos: [],
      selected_modules: defaults.selectedModules,
      target_platform: defaults.targetPlatform,
      selling_points: '',
      language: '',
      video_creator_input: isVideoCreator(defaultType) ? initialVideoInput('') : undefined,
      video_editor_input: isVideoEditor(defaultType) ? initialVideoInput('') : undefined,
      montage_input: defaultType === 'montage' ? initialMontageInput('') : undefined,
    })
    setQuantity(1)
    setWatermark(false)
    setReferenceUploading(false)
    setProjectImageRatio(defaults.imageRatio)
    setHasContentImage(true)
    setHasTailImage(false)
    setArticleWithCover(true)
    setArticleWithContentImages(true)
    void getLocalExecutorStatus().then((status) => {
      setLocalExecutorStatus(status)
      setRunLocally(shouldDefaultRunLocally(status))
    })
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
    form.reset({ type: 'seednote', prompt: '', project_id: '', image_ratio: '', image_model_key: '', input_attachments: [], product_photos: [], selected_modules: {}, target_platform: '', selling_points: '', language: '', video_creator_input: undefined, video_editor_input: undefined, montage_input: undefined })
    setQuantity(1)
    setWatermark(false)
    setReferenceUploading(false)
    setGoalMode(false)
    setGoalText('')
    setProjectImageRatio('')
    setHasContentImage(true)
    setHasTailImage(false)
    setArticleWithCover(true)
    setArticleWithContentImages(true)
  }

  async function onSubmit(values: CreateTaskFormValues) {
    let statusForSubmit = localExecutorStatus
    const wantsLocalExecution = values.type !== 'montage' && runLocally
    if (wantsLocalExecution && statusForSubmit?.state === 'ready_stopped') {
      const ok = await startLocalExecutor()
      if (ok) {
        await setExecutorEnabled(true)
        statusForSubmit = { ...statusForSubmit, running: true, state: 'running_idle' }
        setLocalExecutorStatus(statusForSubmit)
        toast.success('本地执行器已启动')
      } else {
        toast.error('本地执行器启动失败，本次将改为云端执行')
      }
    }
    const runThisTaskLocally = canSubmitLocalTask(statusForSubmit, wantsLocalExecution)
    if (wantsLocalExecution && !runThisTaskLocally) {
      toast.message('本地执行器未运行，本次将改为云端执行')
    }
    await submit(async () => createMutation.mutateAsync({
      type: values.type as import('@/types').TaskType,
      prompt: values.prompt?.trim() || undefined,
      project_id: values.project_id,
      quantity: quantity > 1 ? quantity : undefined,
      image_ratio: values.image_ratio || undefined,
      image_model_key: values.image_model_key || undefined,
      watermark: watermark || undefined,
      goal_mode: values.type !== 'ecommerce' && goalMode ? true : undefined,
      goal: values.type !== 'ecommerce' && goalMode ? (goalText.trim() || undefined) : undefined,
      has_content_image: values.type === 'seednote' ? hasContentImage : undefined,
      has_tail_image: values.type === 'seednote' ? hasTailImage : undefined,
      input_attachments: values.type === 'seednote' ? values.input_attachments : undefined,
      // Article image toggles (公众号文章): both default true; non-article omits.
      article_with_cover: values.type === 'article' ? articleWithCover : undefined,
      article_with_content_images: values.type === 'article' ? articleWithContentImages : undefined,
      // E-commerce package (server forces quantity=1 and charges only the base task fee).
      // The schema guarantees ≥1 module + ≥1 photo for ecommerce; strip empties.
      product_photos: values.type === 'ecommerce' && values.product_photos?.length ? values.product_photos : undefined,
      selected_modules: values.type === 'ecommerce' && values.selected_modules && Object.values(values.selected_modules).some((q) => q >= 1) ? values.selected_modules : undefined,
      target_platform: values.type === 'ecommerce' ? (values.target_platform || undefined) : undefined,
      selling_points: values.type === 'ecommerce' ? (values.selling_points?.trim() || undefined) : undefined,
      language: values.type === 'ecommerce' ? (values.language || undefined) : undefined,
      video_creator_input: isVideoCreator(values.type) ? buildVideoInputForSubmit(values.prompt, values.video_creator_input) : undefined,
      video_editor_input: isVideoEditor(values.type) ? buildVideoInputForSubmit(values.prompt, values.video_editor_input) : undefined,
      montage_input: values.type === 'montage' ? buildMontageInputForSubmit(values.prompt, values.montage_input) : undefined,
      // Route to the desktop local executor only when it is running and able to claim now.
      execution_target: values.type !== 'montage' && runThisTaskLocally ? 'local' : undefined,
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

  const costPreview = taskCreationCostPreview({
    pricing,
    type: watchedType,
    quantity,
    goalMode,
    balance: creditsBalance?.balance ?? 0,
  })

  const videoEditorHasSourceMedia = (watchedVideoEditorReferences ?? []).some((ref) => ref.type === 'video_url' && (ref.url || ref.task_file_id))
  const creationBlocker = costPreview.insufficient
    ? { message: '积分不足，补充积分后再创建。', href: '/credits', actionLabel: '查看积分' }
    : watchedType !== 'ecommerce' && goalMode && !goalText.trim()
      ? { message: '强目标模式需要填写目标条件。', href: '', actionLabel: '' }
      : watchedType === 'ecommerce' && (!watchedProductPhotos || watchedProductPhotos.length === 0)
        ? { message: '电商出图需要先上传产品图。', href: '', actionLabel: '' }
        : isVideoEditor(watchedType) && !videoEditorHasSourceMedia
          ? { message: '视频剪辑后期需要先上传至少一个源视频素材。', href: '', actionLabel: '' }
        : null

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

      {/* Create Task Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-5xl">
          <DialogHeader className="border-b border-border px-4 py-3">
            <DialogTitle>新建任务</DialogTitle>
            <DialogDescription>任务创建路径：类型 → 项目 → 目标/提示词 → 图片/高级 → 基础任务费预估</DialogDescription>
            <p className="pt-2 text-[11px] text-muted-foreground">
              类型 / 项目 / 目标/提示词 / 图片/高级 / 基础任务费预估
            </p>
          </DialogHeader>
          <Form {...form}>
            <form id="task-create-form" onSubmit={form.handleSubmit(onSubmit)} className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-4">
              <div className="rounded-lg border border-border bg-muted/30 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-xs font-medium uppercase text-muted-foreground">01 类型</p>
                    <p className="mt-1 text-sm font-medium text-foreground">选择项目后自动匹配任务类型</p>
                  </div>
                  <Badge variant="secondary" className="shrink-0">
                    {contentTypeLabel[watchedType] || watchedType}
                  </Badge>
                </div>
              </div>

              <div>
                <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">02 项目</p>
              <FormField control={form.control} name="project_id" render={({ field }) => (
                <FormItem>
                  <FormLabel>项目</FormLabel>
                  <FormControl>
                    <ProjectSelector
                      value={field.value || ''}
                      onChange={(id) => {
                        field.onChange(id)
                        if (id) {
                          const ch = projects.find((c) => c.id === id)
                          const defaults = getProjectCreationDefaults(ch)
                          setProjectImageRatio(defaults.imageRatio)
                          form.setValue('type', defaults.type)
                          form.setValue('image_ratio', defaults.imageRatio as CreateTaskFormValues['image_ratio'], { shouldDirty: false })
                          form.setValue('selected_modules', defaults.selectedModules, { shouldDirty: false })
                          form.setValue('target_platform', defaults.targetPlatform, { shouldDirty: false })
                          form.setValue('image_model_key', defaults.imageModelKey, { shouldDirty: false })
                          form.setValue('video_creator_input', isVideoCreator(defaults.type) ? initialVideoInput(form.getValues('prompt') || '') : undefined, { shouldDirty: false })
                          form.setValue('video_editor_input', isVideoEditor(defaults.type) ? initialVideoInput(form.getValues('prompt') || '') : undefined, { shouldDirty: false })
                          form.setValue('montage_input', defaults.type === 'montage' ? initialMontageInput(form.getValues('prompt') || '') : undefined, { shouldDirty: false })
                        } else {
                          setProjectImageRatio('')
                          form.setValue('video_creator_input', undefined, { shouldDirty: false })
                          form.setValue('video_editor_input', undefined, { shouldDirty: false })
                          form.setValue('montage_input', undefined, { shouldDirty: false })
                        }
                      }}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />
              </div>

              {selectedProject && watchedType !== 'viral_analysis' && (
                <div className="rounded-lg border border-dashed border-border bg-muted/30 p-3 space-y-1">
                  <p className="text-xs font-medium text-foreground/80">将使用项目「{selectedProject.name}」的快照</p>
                  <p className="text-xs text-muted-foreground">
                    {isVideoTask || isMontageTask ? (
                      <>项目定位 {selectedProject.instructions || selectedProject.positioning || '—'}</>
                    ) : (
                      <>视觉风格 {selectedProject.visual_style || '—'}</>
                    )}
                    {watchedType === 'article' && (
                      <> · 署名 {selectedProject.author || '—'} · 写作风格 {selectedProject.writer || '—'} · 排版 {selectedProject.theme || '默认'}</>
                    )}
                  </p>
                  <p className="text-[11px] text-muted-foreground/80">创建后项目再修改，不会影响这个任务。</p>
                </div>
              )}

              <div className="pt-1">
                <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">03 目标/提示词</p>
              {!isVideoTask && !isMontageTask && <FormField control={form.control} name="prompt" render={({ field }) => (
                <FormItem>
                  <FormLabel>创作要求（可选）</FormLabel>
                  <FormControl>
                    <Textarea placeholder="描述你的创作要求，留空则根据项目信息自动生成" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />}
              </div>

              <div className="pt-1">
                <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">04 图片/高级</p>

              {/* Quantity selector (ecommerce creates one guided package task at qty=1) */}
              {watchedType !== 'ecommerce' && !isVideoTask && !isMontageTask && (
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
              )}

              {/* Image ratio selector (ecommerce uses per-module ratios from platform specs) */}
              {watchedType !== 'ecommerce' && !isVideoTask && !isMontageTask && (
              <FormField control={form.control} name="image_ratio" render={({ field }) => {
                const defaultRatio = projectImageRatio || platformDefaultRatio[watchedType] || '3:4'
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
              )}

              {/* Image model selector */}
              {!isVideoTask && !isMontageTask && <FormField control={form.control} name="image_model_key" render={({ field }) => (
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
              )} />}

              {isVideoTask && (
                <VideoCreationPanel
                  form={form}
                  fieldRoot={isVideoCreatorTask ? 'video_creator_input' : 'video_editor_input'}
                  selectedProject={selectedProject}
                  title={isVideoCreatorTask ? 'AI 视频生成' : '视频剪辑后期'}
                  promptField={(
                    <FormField control={form.control} name="prompt" render={({ field }) => (
                      <FormItem>
                        <FormControl>
                          <Textarea
                            placeholder={isVideoCreatorTask
                              ? '描述你想要的视频内容、卖点、镜头风格或禁忌，留空则根据项目自动生成'
                              : '描述剪辑目标、脚本、字幕、节奏、包装、CapCut 草稿或交付要求'}
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                  )}
                />
              )}

              {isMontageTask && (
                <MontageCreationPanel
                  form={form}
                  fieldRoot="montage_input"
                />
              )}

              {/* Watermark toggle */}
              {!isVideoTask && !isMontageTask && <button
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
              </button>}

              {watchedType === 'seednote' && (
                <section aria-label="Seednote 参考素材" className="space-y-3 rounded-lg border border-border p-3">
                  <div>
                    <h3 className="text-sm font-medium text-foreground">参考素材</h3>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      上传产品图、场景图或风格参考，AI 会自动判断如何使用。
                    </p>
                  </div>
                  <ReferenceMaterialInput
                    value={watchedInputAttachments ?? []}
                    onChange={(value) => form.setValue(
                      'input_attachments',
                      value,
                      { shouldDirty: true, shouldValidate: true },
                    )}
                    allowedTypes={['image']}
                    maxCount={16}
                    instructionEnabled
                    instructionMaxLength={1000}
                    hint="AI 会先理解创作需求，再逐张分析图片并自动决定每页是否使用。"
                    onUploadingChange={setReferenceUploading}
                  />
                </section>
              )}

              {/* Image composition (seednote only) */}
              {watchedType === 'seednote' && (
                <div className="rounded-lg border border-border p-3">
                  <div className="flex items-start gap-3">
                    <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-foreground">图片构成</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        封面始终生成；勾选要额外生成的图。
                      </p>
                    </div>
                  </div>
                  <div className="mt-3 divide-y divide-border">
                    <div className="flex items-center justify-between py-2">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium text-foreground">封面图</span>
                        <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">必选</span>
                      </div>
                      <Switch checked disabled />
                    </div>
                    <div className="flex items-center justify-between py-2">
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-foreground">内容图</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">承载 2-4 个信息点（image_01.png）</p>
                      </div>
                      <Switch checked={hasContentImage} onCheckedChange={setHasContentImage} />
                    </div>
                    <div className="flex items-center justify-between py-2">
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-foreground">尾图</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">行动召唤 / 关注引导（tail.png）</p>
                      </div>
                      <Switch checked={hasTailImage} onCheckedChange={setHasTailImage} />
                    </div>
                  </div>
                  <p className="mt-2 text-xs text-muted-foreground">
                    当前将生成 {1 + (hasContentImage ? 1 : 0) + (hasTailImage ? 1 : 0)} 张图片
                  </p>
                </div>
              )}

              {/* Image composition (公众号 article): cover + content images each
                  independently toggleable — unlike seednote, the article cover is
                  NOT mandatory (both default on → legacy behavior). */}
              {watchedType === 'article' && (
                <div className="rounded-lg border border-border p-3">
                  <div className="flex items-start gap-3">
                    <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-foreground">图片构成</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        独立选择是否生成封面与正文配图。两者都关 = 纯文字文章。
                      </p>
                    </div>
                  </div>
                  <div className="mt-3 divide-y divide-border">
                    <div className="flex items-center justify-between py-2">
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-foreground">封面图</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">公众号头图（900×383，作为发布草稿封面）</p>
                      </div>
                      <Switch checked={articleWithCover} onCheckedChange={setArticleWithCover} />
                    </div>
                    <div className="flex items-center justify-between py-2">
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-foreground">正文配图</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">按排版节奏插入的章节插图</p>
                      </div>
                      <Switch checked={articleWithContentImages} onCheckedChange={setArticleWithContentImages} />
                    </div>
                  </div>
                  <p className="mt-2 text-xs text-muted-foreground">
                    {articleWithCover && articleWithContentImages
                      ? '将生成封面 + 正文配图（默认）'
                      : articleWithCover
                        ? '仅生成封面图，不生成正文配图'
                        : articleWithContentImages
                          ? '仅生成正文配图；发布草稿不设封面'
                          : '纯文字文章，不生成任何图片；发布草稿不设封面'}
                  </p>
                </div>
              )}

              {/* E-commerce package: product photos + selectable modules + platform/compliance.
                  Creation billing is the ecommerce base task fee; module qty only guides later MCP usage. */}
              {watchedType === 'ecommerce' && (
                <div className="space-y-4 rounded-lg border border-border p-3">
                  <div className="flex items-start gap-3">
                    <Package className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-foreground">电商素材包</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        上传产品图，选择交付模块。创建只扣基础任务费，后续图片生成和理解按实际用量结算。
                      </p>
                    </div>
                  </div>

                  <FormField control={form.control} name="product_photos" render={({ field }) => (
                    <FormItem>
                      <FormLabel>产品图（必填）</FormLabel>
                      <FormControl>
                        <MultiImageUpload
                          value={field.value ?? []}
                          onChange={(urls) => field.onChange(urls)}
                          purpose="reference"
                          max={16}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <div className="space-y-2">
                    <FormLabel>交付模块（至少选一项）</FormLabel>
                    <div className="divide-y divide-border">
                      {ecommerceModuleCatalog.map((mod) => {
                        const qty = watchedSelectedModules?.[mod.key] ?? 0
                        const enabled = qty >= 1
                        const price = pricing?.ecommerce_module_prices?.[mod.key]
                        return (
                          <div key={mod.key} className="flex items-center justify-between gap-3 py-2">
                            <div className="min-w-0 flex-1">
                              <div className="flex flex-wrap items-center gap-2">
                                <p className="text-sm font-medium text-foreground">{mod.label}</p>
                                <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{mod.ratio}</span>
                                {price != null && (
                                  <span className="text-[10px] text-muted-foreground">{price} 积分/{mod.qtyLabel}</span>
                                )}
                              </div>
                              <p className="mt-0.5 text-xs text-muted-foreground">{mod.hint}</p>
                            </div>
                            <div className="flex items-center gap-2">
                              {enabled && (
                                <div className="flex items-center gap-1">
                                  <Button type="button" variant="outline" size="sm" className="h-7 w-7 p-0" onClick={() => setModuleQty(mod.key, Math.max(mod.minQty, qty - mod.qtyStep))} aria-label="减少">
                                    <Minus className="h-3 w-3" />
                                  </Button>
                                  <span className="w-8 text-center text-sm tabular-nums">{qty}{mod.qtyLabel}</span>
                                  <Button type="button" variant="outline" size="sm" className="h-7 w-7 p-0" onClick={() => setModuleQty(mod.key, Math.min(mod.maxQty, qty + mod.qtyStep))} aria-label="增加">
                                    <Plus className="h-3 w-3" />
                                  </Button>
                                </div>
                              )}
                              <Switch checked={enabled} onCheckedChange={(on) => setModuleQty(mod.key, on ? mod.defaultQty : 0)} aria-label={`启用 ${mod.label}`} />
                            </div>
                          </div>
                        )
                      })}
                    </div>
                    {(!watchedSelectedModules || Object.values(watchedSelectedModules).every((q) => !q || q < 1)) && (
                      <p className="text-xs text-destructive">请至少选择一个交付模块</p>
                    )}
                  </div>

                  <FormField control={form.control} name="target_platform" render={({ field }) => (
                    <FormItem>
                      <FormLabel>目标平台</FormLabel>
                      <Select value={field.value || undefined} onValueChange={field.onChange}>
                        <FormControl>
                          <SelectTrigger className="w-full"><SelectValue placeholder="选择投放平台（影响尺寸与合规规范）" /></SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {ecommerceTargetPlatformOptions.map((opt) => (
                            <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="selling_points" render={({ field }) => (
                    <FormItem>
                      <FormLabel>核心卖点（可选）</FormLabel>
                      <FormControl>
                        <Textarea placeholder="列出产品核心卖点（材质 / 功能 / 使用场景 / 价格优势等）。留空则由 AI 从产品图分析提炼" className="min-h-[72px] resize-y" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="language" render={({ field }) => (
                    <FormItem>
                      <FormLabel>语言</FormLabel>
                      <Select value={field.value || undefined} onValueChange={field.onChange}>
                        <FormControl>
                          <SelectTrigger className="w-full"><SelectValue placeholder="中文" /></SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {ecommerceLanguageOptions.map((opt) => (
                            <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )} />
                </div>
              )}


              {/* Goal mode toggle (ecommerce keeps a guided package flow without goal retries) */}
              {watchedType !== 'ecommerce' && !isMontageTask && (
              <div className={`rounded-lg border p-3 transition-colors ${
                goalMode ? 'border-primary bg-primary/5' : 'border-border'
              }`}>
                <button
                  type="button"
                  onClick={() => setGoalMode(!goalMode)}
                  className="flex w-full items-start gap-3 text-left"
                >
                  <Target className={`mt-0.5 h-5 w-5 shrink-0 ${goalMode ? 'text-primary' : 'text-muted-foreground'}`} />
                  <div className="min-w-0 flex-1">
                    <p className={`text-sm font-medium ${goalMode ? 'text-foreground' : 'text-muted-foreground'}`}>
                      强目标模式
                    </p>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      开启后扣费 ×3，最多尝试 3 次。任务执行后由 AI 评估产出是否满足「目标条件」，未达成自动重试。
                    </p>
                  </div>
                  <Switch checked={goalMode} onCheckedChange={setGoalMode} />
                </button>
                {goalMode && (
                  <div className="mt-3 space-y-2">
                    <Textarea
                      value={goalText}
                      onChange={(e) => setGoalText(e.target.value)}
                      placeholder="例：文章字数 ≥ 1500 字；必须包含 3 个真实案例；开头必须设置钩子；种草笔记必须包含具体使用感受…"
                      className="min-h-[80px] resize-y text-sm"
                      maxLength={4000}
                    />
                    <div className="flex flex-wrap gap-1.5">
                      {[
                        '文章字数 ≥ 1500 字',
                        '必须包含 3 个真实案例',
                        '开头必须设置钩子，吸引读者继续阅读',
                        '必须包含数据或引用来源',
                      ].map((example) => (
                        <button
                          key={example}
                          type="button"
                          onClick={() => setGoalText(example)}
                          className="rounded-full border border-border bg-background px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground"
                        >
                          {example}
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
              )}
              </div>

              {/* Cost display */}
              <div className="pt-1">
                <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">05 基础任务费预估</p>
                <div className="space-y-1 rounded-md border border-border bg-muted/50 p-3 text-sm">
                  <p className="text-muted-foreground">
                    基础任务费：{costPreview.baseCost} × {costPreview.billableQuantity}
                    {costPreview.multiplier > 1 && ` × ${costPreview.multiplier}`} = <span className="font-medium text-foreground">{costPreview.totalCost}</span> 积分
                    {costPreview.multiplier > 1 && <span className="ml-1 text-xs text-amber-600">（含目标重试）</span>}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {runLocally && !isMontageTask ? '本机运行使用你的 Claude Code 环境。' : '云端 Claude Code 运行成本由平台承担，不额外预留或补扣。'}
                  </p>
                  {watchedType === 'ecommerce' ? (
                    <p className="text-xs text-muted-foreground">所选交付模块会影响后续图片生成和理解操作用量，最终以交易明细汇总为准。</p>
                  ) : (
                    <p className="text-xs text-muted-foreground">模型、图片、视频等 MCP 操作费用按实际用量另计。</p>
                  )}
                  <p className="text-muted-foreground">
                    余额：{(creditsBalance?.balance ?? 0).toLocaleString()} →{' '}
                    <span className={`font-medium ${costPreview.remaining < 0 ? 'text-red-500' : 'text-foreground'}`}>
                      {costPreview.remaining.toLocaleString()}
                    </span>
                  </p>
                  {creationBlocker && (
                    <p className="text-sm font-medium text-red-500">
                      {creationBlocker.href ? <Link to={creationBlocker.href}>{creationBlocker.message}</Link> : creationBlocker.message}
                    </p>
                  )}
                </div>
              </div>
            </form>
          </Form>
          <DialogFooter className="mx-0 mb-0 border-t border-border bg-popover px-4 py-3 sm:flex-row sm:items-center sm:justify-end">
            {localExecutorAvailable && !isMontageTask && (
              <div className="mr-auto flex min-w-0 items-center gap-2 text-xs">
                <label className="flex cursor-pointer items-center gap-2 text-muted-foreground" title="在本机运行：使用桌面端内置的 Claude Code + ffmpeg，可剪辑本地视频、执行本地命令。关闭则改为云端执行。">
                  <Switch checked={runLocally} onCheckedChange={setRunLocally} />
                  <span className="whitespace-nowrap">在本机运行</span>
                </label>
                <span className="max-w-[260px] truncate text-muted-foreground/75" title={localExecutorHint}>
                  {localExecutorHint}
                </span>
              </div>
            )}
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button
              type="submit"
              form="task-create-form"
              loading={createMutation.isPending}
              disabled={Boolean(creationBlocker) || (watchedType === 'seednote' && referenceUploading)}
            >
              {runLocally && !isMontageTask && localExecutorStatus?.state === 'ready_stopped'
                ? '启动并创建'
                : quantity > 1 ? `创建 ${quantity} 个任务` : '创建'}
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
